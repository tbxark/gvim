package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// fakeWatchWatcher is a controllable hostos.Watch for the remote-watch adapter tests. It
// records the context the adapter gave it and reports events pushed by the test.
type fakeWatchWatcher struct {
	ctx    context.Context
	events chan hostos.WatchEvent
	stop   chan struct{}
	once   sync.Once
}

func newFakeWatchWatcher(ctx context.Context) *fakeWatchWatcher {
	return &fakeWatchWatcher{ctx: ctx, events: make(chan hostos.WatchEvent, 16), stop: make(chan struct{})}
}

func (w *fakeWatchWatcher) Next(ctx context.Context) (hostos.WatchEvent, bool) {
	select {
	case ev := <-w.events:
		return ev, true
	case <-w.ctx.Done():
		return hostos.WatchEvent{}, false
	case <-ctx.Done():
		return hostos.WatchEvent{}, false
	case <-w.stop:
		return hostos.WatchEvent{}, false
	}
}

func (w *fakeWatchWatcher) Stop() { w.once.Do(func() { close(w.stop) }) }

func (w *fakeWatchWatcher) push(ev hostos.WatchEvent) { w.events <- ev }

func (w *fakeWatchWatcher) stopped(t *testing.T) {
	t.Helper()
	select {
	case <-w.stop:
	case <-time.After(2 * time.Second):
		t.Fatal("the remote watch was not stopped")
	}
}

// fakeWatchFS is an authority.FileSystem whose Watch creates controllable watchers.
type fakeWatchFS struct {
	mu       sync.Mutex
	dirs     []string
	watchers []*fakeWatchWatcher
}

func (f *fakeWatchFS) Watch(ctx context.Context, dir string, _ hostos.WatchOptions) (hostos.Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := newFakeWatchWatcher(ctx)
	f.dirs = append(f.dirs, dir)
	f.watchers = append(f.watchers, w)
	return w, nil
}

func (f *fakeWatchFS) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not needed by the watch tests")
}

func (f *fakeWatchFS) ReadFile(context.Context, string, int64) ([]byte, error) {
	return nil, errors.New("not needed by the watch tests")
}

func (f *fakeWatchFS) WriteFile(context.Context, string, []byte, hostos.WriteOptions) error {
	return errors.New("not needed by the watch tests")
}

func (f *fakeWatchFS) Stat(context.Context, string) (hostos.FileStat, error) {
	return hostos.FileStat{}, errors.New("not needed by the watch tests")
}

func (f *fakeWatchFS) ReadDir(context.Context, string, bool) ([]hostos.DirEntry, error) {
	return nil, errors.New("not needed by the watch tests")
}

func (f *fakeWatchFS) waitWatch(t *testing.T, n int) *fakeWatchWatcher {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.watchers) >= n {
			w := f.watchers[n-1]
			f.mu.Unlock()
			return w
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("remote watch %d was not started", n)
	return nil
}

func (f *fakeWatchFS) watchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.watchers)
}

// watchSlot returns a connected fake authority slot over fs.
func watchSlot(fs *fakeWatchFS) *authoritytest.Fake {
	slot := authoritytest.New()
	slot.FileSystem = fs
	return slot
}

// nextChanged reads one change from the adapter with a timeout.
func nextChanged(t *testing.T, ch <-chan filewatch.Changed) filewatch.Changed {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("the watch channel closed")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("no remote change was forwarded")
	}
	return filewatch.Changed{}
}

// expectNoChanged asserts no change arrives within d.
func expectNoChanged(t *testing.T, ch <-chan filewatch.Changed, d time.Duration) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("an unexpected change arrived: %+v", ev)
		}
	case <-time.After(d):
	}
}

// TestRemoteWatchForwardsEvents checks a remote directory gets one authority watch and its
// events reach the root's Changed channel with the changed file and its directory.
func TestRemoteWatchForwardsEvents(t *testing.T) {
	fs := &fakeWatchFS{}
	slot := watchSlot(fs)
	w := newRemoteWatch(context.Background(), slot)
	defer func() { _ = w.Close() }()
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	watch := fs.waitWatch(t, 1)
	fs.mu.Lock()
	if fs.dirs[0] != "/remote/proj" {
		t.Fatalf("watched %q, want the remote directory", fs.dirs[0])
	}
	fs.mu.Unlock()
	watch.push(hostos.WatchEvent{Path: "/remote/proj/a.go", Kind: "create"})
	ev := nextChanged(t, w.Events())
	if len(ev.Files) != 1 || ev.Files[0] != "/remote/proj/a.go" {
		t.Fatalf("changed files = %v", ev.Files)
	}
	if len(ev.Dirs) != 1 || ev.Dirs[0] != "/remote/proj" {
		t.Fatalf("changed dirs = %v", ev.Dirs)
	}
}

// TestRemoteWatchCoalescesBursts checks a burst becomes one (or few) coalesced Changed
// values that still carry every changed file.
func TestRemoteWatchCoalescesBursts(t *testing.T) {
	fs := &fakeWatchFS{}
	w := newRemoteWatch(context.Background(), watchSlot(fs))
	defer func() { _ = w.Close() }()
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	watch := fs.waitWatch(t, 1)
	watch.push(hostos.WatchEvent{Path: "/remote/proj/a.go", Kind: "create"})
	watch.push(hostos.WatchEvent{Path: "/remote/proj/b.go", Kind: "write"})
	deadline := time.Now().Add(3 * time.Second)
	files := map[string]bool{}
	for len(files) < 2 && time.Now().Before(deadline) {
		for _, f := range nextChanged(t, w.Events()).Files {
			files[f] = true
		}
	}
	if !files["/remote/proj/a.go"] || !files["/remote/proj/b.go"] {
		t.Fatalf("coalesced files = %v, want both", files)
	}
}

// TestRemoteWatchCloseCancels checks Close stops the authority watch, closes the events
// channel and is idempotent.
func TestRemoteWatchCloseCancels(t *testing.T) {
	fs := &fakeWatchFS{}
	w := newRemoteWatch(context.Background(), watchSlot(fs))
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	watch := fs.waitWatch(t, 1)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	watch.stopped(t)
	if _, ok := <-w.Events(); ok {
		t.Fatal("the events channel stayed open after Close")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRemoteWatchReconnectRestartsAndDropsStale checks Reconnected cancels the old watch,
// starts a new one for the desired directory, and an event from the pre-reconnect watch
// never reaches the root (generation drop).
func TestRemoteWatchReconnectRestartsAndDropsStale(t *testing.T) {
	fs := &fakeWatchFS{}
	w := newRemoteWatch(context.Background(), watchSlot(fs))
	defer func() { _ = w.Close() }()
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	old := fs.waitWatch(t, 1)
	old.push(hostos.WatchEvent{Path: "/remote/proj/stale.go", Kind: "write"})
	w.Reconnected()
	old.stopped(t)
	fresh := fs.waitWatch(t, 2)
	// The stale event must not be applied: a flush from the old generation is dropped.
	expectNoChanged(t, w.Events(), 500*time.Millisecond)
	fresh.push(hostos.WatchEvent{Path: "/remote/proj/live.go", Kind: "write"})
	ev := nextChanged(t, w.Events())
	if len(ev.Files) != 1 || ev.Files[0] != "/remote/proj/live.go" {
		t.Fatalf("changed after reconnect = %v, want only the new event", ev.Files)
	}
}

// TestRemoteWatchWaitsForReconnect checks nothing is watched on a disconnected authority,
// and the desired directory starts being watched after Reconnected with a live slot.
func TestRemoteWatchWaitsForReconnect(t *testing.T) {
	fs := &fakeWatchFS{}
	slot := watchSlot(fs)
	slot.Disconnect("carrier down")
	w := newRemoteWatch(context.Background(), slot)
	defer func() { _ = w.Close() }()
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if got := fs.watchCount(); got != 0 {
		t.Fatalf("%d watches started on a disconnected authority", got)
	}
	if err := slot.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Reconnected()
	fresh := fs.waitWatch(t, 1)
	fresh.push(hostos.WatchEvent{Path: "/remote/proj/a.go", Kind: "create"})
	if ev := nextChanged(t, w.Events()); len(ev.Files) != 1 {
		t.Fatalf("change after reconnect = %+v", ev)
	}
}

// TestRemoteWatchAddIsIdempotent checks a directory added twice gets one watch.
func TestRemoteWatchAddIsIdempotent(t *testing.T) {
	fs := &fakeWatchFS{}
	w := newRemoteWatch(context.Background(), watchSlot(fs))
	defer func() { _ = w.Close() }()
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	fs.waitWatch(t, 1)
	time.Sleep(250 * time.Millisecond)
	if got := fs.watchCount(); got != 1 {
		t.Fatalf("watch count = %d, want one", got)
	}
}

// TestRemoteWatchNilSlot checks the adapter degrades safely without an authority (a test
// or a miswired launch): Add does nothing and Close is safe.
func TestRemoteWatchNilSlot(t *testing.T) {
	w := newRemoteWatch(context.Background(), nil)
	if err := w.Add("/remote/proj"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestSetupRemoteWatchWiresTheRootPorts checks the app wiring used by buildRuntime: a
// remote launch gets the bridge as the root's watch channel and WatchDirs port, and a local
// launch (nil spec) leaves the root ports untouched.
func TestSetupRemoteWatchWiresTheRootPorts(t *testing.T) {
	fs := &fakeWatchFS{}
	deps := tui.Deps{}
	w := setupRemoteWatch(context.Background(), &deps, watchSlot(fs), &authority.RemoteSpec{Path: "/remote/proj"})
	if w == nil {
		t.Fatal("a remote launch got no remote watch")
	}
	if deps.Root.Watch == nil {
		t.Fatal("the remote watch channel was not wired into RootPorts.Watch")
	}
	if deps.Root.WatchDirs != w {
		t.Fatal("the remote bridge was not wired into RootPorts.WatchDirs")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	local := tui.Deps{}
	if got := setupRemoteWatch(context.Background(), &local, watchSlot(fs), nil); got != nil {
		t.Fatalf("a local launch got a remote watch: %v", got)
	}
	if local.Root.Watch != nil || local.Root.WatchDirs != nil {
		t.Fatal("a local launch's root watch ports were touched")
	}
}

var _ authority.FileSystem = (*fakeWatchFS)(nil)
