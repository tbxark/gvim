package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// ignoreFS is an in-memory authority.FileSystem for the remote ignore/watch tests:
// files, directories and .gitignore contents are data, a local decoy tree must never be
// read, and Watch can succeed or be refused.
type ignoreFS struct {
	mu       sync.Mutex
	files    map[string]string
	dirs     map[string][]hostos.DirEntry
	reads    []string
	watchErr error
	watch    *fakeWatch
	watchN   int
}

func newIgnoreFS() *ignoreFS {
	return &ignoreFS{files: map[string]string{}, dirs: map[string][]hostos.DirEntry{}}
}

func (f *ignoreFS) file(p, data string) *ignoreFS {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[p] = data
	return f
}

func (f *ignoreFS) dir(p string, names ...string) *ignoreFS {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range names {
		kind := "file"
		if strings.HasSuffix(name, "/") {
			kind, name = "dir", strings.TrimSuffix(name, "/")
		}
		f.dirs[p] = append(f.dirs[p], hostos.DirEntry{Name: name, Kind: kind})
	}
	return f
}

func (f *ignoreFS) ReadFile(_ context.Context, p string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, p)
	data, ok := f.files[p]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	if max > 0 && int64(len(data)) > max {
		return nil, errors.New("remote read: file exceeds the read cap")
	}
	return []byte(data), nil
}

func (f *ignoreFS) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not used")
}

func (f *ignoreFS) WriteFile(context.Context, string, []byte, hostos.WriteOptions) error {
	return errors.New("not used")
}

func (f *ignoreFS) Stat(context.Context, string) (hostos.FileStat, error) {
	return hostos.FileStat{}, errors.New("not used")
}

func (f *ignoreFS) ReadDir(_ context.Context, p string, _ bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, p)
	entries, ok := f.dirs[p]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return append([]hostos.DirEntry(nil), entries...), nil
}

func (f *ignoreFS) Watch(_ context.Context, _ string, _ hostos.WatchOptions) (hostos.Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchN++
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	if f.watch == nil {
		f.watch = &fakeWatch{stop: make(chan struct{})}
	}
	return f.watch, nil
}

func (f *ignoreFS) readList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...)
}

// fakeWatch is an authority directory watch that only stops.
type fakeWatch struct {
	stop chan struct{}
	once sync.Once
}

func (w *fakeWatch) Next(ctx context.Context) (hostos.WatchEvent, bool) {
	select {
	case <-ctx.Done():
		return hostos.WatchEvent{}, false
	case <-w.stop:
		return hostos.WatchEvent{}, false
	}
}
func (w *fakeWatch) Stop() { w.once.Do(func() { close(w.stop) }) }

// addWatcher records the directories added to the watch bridge.
type addWatcher struct {
	mu   sync.Mutex
	dirs []string
	err  error
}

func (w *addWatcher) Add(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	w.dirs = append(w.dirs, dir)
	return nil
}
func (w *addWatcher) added() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.dirs...)
}

func ignoreSlot(fs *ignoreFS) *authoritytest.Fake {
	slot := authoritytest.New()
	slot.FileSystem = fs
	return slot
}

// ignoreFixture is the remote workspace of the ignore tests: a root .gitignore with a
// glob and an anchored directory, a negation, and a nested .gitignore.
func ignoreFixture() *ignoreFS {
	fs := newIgnoreFS()
	fs.file("/remote/w/.gitignore", "*.log\n!keep.log\n/build/\n")
	fs.dir("/remote/w", "a.go", "b.log", "keep.log", "build/", "sub/")
	fs.file("/remote/w/sub/.gitignore", "*.tmp\n")
	fs.dir("/remote/w/sub", "c.tmp", "d.go")
	return fs
}

// TestRemoteIgnoreFiltersExplorerListing checks root/nested/negation/anchored rules mark
// the remote explorer entries (the tree hides them unless show-ignored is on) and a
// local decoy tree is never read.
func TestRemoteIgnoreFiltersExplorerListing(t *testing.T) {
	local := t.TempDir()
	for _, name := range []string{"a.go", "b.log", "keep.log"} {
		if err := os.WriteFile(filepath.Join(local, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fs := ignoreFixture()
	loader := newAuthorityDirLoader(ignoreSlot(fs), nil, "/remote/w")
	got, err := loader.ReadDirs(context.Background(), []string{"/remote/w", "/remote/w/sub"})
	if err != nil {
		t.Fatalf("ReadDirs: %v", err)
	}
	flags := map[string]bool{}
	for dir, entries := range got {
		for _, e := range entries {
			flags[dir+"/"+e.Name] = e.Ignored
		}
	}
	for p, want := range map[string]bool{
		"/remote/w/a.go":      false,
		"/remote/w/b.log":     true,
		"/remote/w/keep.log":  false, // negated back in
		"/remote/w/build":     true,  // anchored root directory
		"/remote/w/sub":       false,
		"/remote/w/sub/c.tmp": true, // nested .gitignore
		"/remote/w/sub/d.go":  false,
	} {
		if flags[p] != want {
			t.Fatalf("%s ignored = %v, want %v (all %v)", p, flags[p], want, flags)
		}
	}
	// The .gitignore reads are the remote paths only; the local decoy was never read.
	for _, r := range fs.readList() {
		if !strings.HasPrefix(r, "/remote/w") {
			t.Fatalf("read outside the remote root: %q", r)
		}
	}
}

// TestRemoteIgnoreFiltersFinder checks the remote finder's directory reads drop ignored
// entries (root and nested), so the palette does not list them.
func TestRemoteIgnoreFiltersFinder(t *testing.T) {
	fs := ignoreFixture()
	files := func() authority.FileSystem { return fs }
	r := remoteFinderFS{files: files, ignore: newIgnoreBatch(files, "/remote/w")}
	root, err := r.ReadDir(context.Background(), "/remote/w", true)
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	if names := remoteEntryNames(root); strings.Join(names, ",") != "a.go,keep.log,sub" {
		t.Fatalf("root entries = %v, want ignored entries dropped", names)
	}
	sub, err := r.ReadDir(context.Background(), "/remote/w/sub", true)
	if err != nil {
		t.Fatalf("ReadDir sub: %v", err)
	}
	if names := remoteEntryNames(sub); strings.Join(names, ",") != "d.go" {
		t.Fatalf("sub entries = %v, want the nested ignore applied", names)
	}
}

func remoteEntryNames(entries []finder.RemoteEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

// TestRemoteIgnoreDegradesWithNotice checks an oversized .gitignore is not applied and
// records one notice instead of failing the listing.
func TestRemoteIgnoreDegradesWithNotice(t *testing.T) {
	fs := ignoreFixture()
	fs.file("/remote/w/.gitignore", strings.Repeat("# pad\n", remoteIgnoreFileMaxBytes/6+10))
	loader := newAuthorityDirLoader(ignoreSlot(fs), nil, "/remote/w")
	loader.setWatcher(&addWatcher{})
	if _, err := loader.ReadDirs(context.Background(), []string{"/remote/w"}); err != nil {
		t.Fatalf("ReadDirs: %v", err)
	}
	notice := loader.TakeNotice()
	if !strings.Contains(notice, "ignore filtering may be incomplete") {
		t.Fatalf("notice = %q, want the ignore degradation notice", notice)
	}
	if got := loader.TakeNotice(); got != "" {
		t.Fatalf("the notice repeated: %q", got)
	}
}

// TestRemoteIgnoreDepthBound checks rules beyond the depth bound are not applied and a
// notice is recorded.
func TestRemoteIgnoreDepthBound(t *testing.T) {
	fs := newIgnoreFS()
	parts := make([]string, remoteIgnoreMaxDepth+1)
	for i := range parts {
		parts[i] = "d"
	}
	deep := "/remote/w/" + strings.Join(parts, "/")
	batch := newIgnoreBatch(func() authority.FileSystem { return fs }, "/remote/w")
	_ = batch.rulesFor(context.Background(), deep)
	if notice := batch.Notice(); !strings.Contains(notice, "below 64 directory levels") {
		t.Fatalf("notice = %q, want the depth notice", notice)
	}
}

// TestRemoteWatchUnsupportedNotice checks a backend that refuses Watch gets one notice and
// no directory adds, while the listing still works (manual refresh).
func TestRemoteWatchUnsupportedNotice(t *testing.T) {
	fs := ignoreFixture()
	fs.watchErr = errors.New("watch unsupported")
	loader := newAuthorityDirLoader(ignoreSlot(fs), nil, "/remote/w")
	watcher := &addWatcher{}
	loader.setWatcher(watcher)
	if _, err := loader.ReadDirs(context.Background(), []string{"/remote/w"}); err != nil {
		t.Fatalf("ReadDirs with an unsupported watch: %v", err)
	}
	if notice := loader.TakeNotice(); !strings.Contains(notice, "not supported by this backend") {
		t.Fatalf("notice = %q, want the unsupported-watch notice", notice)
	}
	if len(watcher.added()) != 0 {
		t.Fatalf("watch dirs = %v, want none", watcher.added())
	}
}

// TestRemoteWatchAddsLoadedDirectories checks a supported watch gets each listed
// directory (the existing remoteWatch bridge then refreshes the tree off the Update loop).
func TestRemoteWatchAddsLoadedDirectories(t *testing.T) {
	fs := ignoreFixture()
	loader := newAuthorityDirLoader(ignoreSlot(fs), nil, "/remote/w")
	watcher := &addWatcher{}
	loader.setWatcher(watcher)
	if _, err := loader.ReadDirs(context.Background(), []string{"/remote/w", "/remote/w/sub"}); err != nil {
		t.Fatalf("ReadDirs: %v", err)
	}
	added := strings.Join(watcher.added(), ",")
	if !strings.Contains(added, "/remote/w") || !strings.Contains(added, "/remote/w/sub") {
		t.Fatalf("watch dirs = %v, want both listed directories", watcher.added())
	}
}

// TestRemoteIgnoreDisconnectedTouchesNothing checks a disconnected authority lists nothing,
// adds no watches and keeps the explicit disconnected error.
func TestRemoteIgnoreDisconnectedTouchesNothing(t *testing.T) {
	fs := ignoreFixture()
	slot := ignoreSlot(fs)
	slot.Disconnect("carrier down")
	loader := newAuthorityDirLoader(slot, nil, "/remote/w")
	watcher := &addWatcher{}
	loader.setWatcher(watcher)
	if _, err := loader.ReadDirs(context.Background(), []string{"/remote/w"}); !errors.Is(err, tui.ErrRemoteExplorerDisconnected) {
		t.Fatalf("error = %v, want the disconnected error", err)
	}
	if len(watcher.added()) != 0 || len(fs.readList()) != 0 {
		t.Fatalf("a disconnected load touched the remote: reads %v adds %v", fs.readList(), watcher.added())
	}
}

var _ = time.Second
