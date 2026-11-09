package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/internal/filewatch"
)

// fakeEnvFileWatch is the injectable local file watcher for the env-detector tests: it
// records Add/Remove and lets the test push filewatch.Changed values.
type fakeEnvFileWatch struct {
	mu      sync.Mutex
	events  chan filewatch.Changed
	added   []string
	removed []string
	closed  bool
	failDir string
}

func newFakeEnvFileWatch() *fakeEnvFileWatch {
	return &fakeEnvFileWatch{events: make(chan filewatch.Changed, 16)}
}

func (f *fakeEnvFileWatch) Add(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDir != "" && dir == f.failDir {
		return errors.New("watch add failed")
	}
	f.added = append(f.added, dir)
	return nil
}

func (f *fakeEnvFileWatch) Remove(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, dir)
	return nil
}

func (f *fakeEnvFileWatch) Events() <-chan filewatch.Changed { return f.events }

func (f *fakeEnvFileWatch) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeEnvFileWatch) push(ev filewatch.Changed) { f.events <- ev }

func (f *fakeEnvFileWatch) addedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.added)
}

func waitLocalDetected(t *testing.T, provider *env.Provider, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		det, ok := provider.Detected()
		if name == "" {
			if !ok {
				return
			}
		} else if ok && det.Name == name {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	det, ok := provider.Detected()
	t.Fatalf("detection = %+v, %v; want %q", det, ok, name)
}

func TestLocalEnvWatchRefreshesDetection(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}}, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	if lw == nil {
		t.Fatal("no local watcher")
	}
	defer lw.Close()
	marker := filepath.Join(root, ".nvmrc")
	if err := os.WriteFile(marker, []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake.push(filewatch.Changed{Files: []string{marker}})
	waitLocalDetected(t, provider, "node")

	// An unrelated file does not refresh; marker removal does.
	fake.push(filewatch.Changed{Files: []string{filepath.Join(root, "main.go")}})
	time.Sleep(50 * time.Millisecond)
	if det, ok := provider.Detected(); !ok || det.Name != "node" {
		t.Fatalf("unrelated change refreshed detection: %+v, %v", det, ok)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	fake.push(filewatch.Changed{Files: []string{marker}})
	waitLocalDetected(t, provider, "")
	if notice := provider.WatchNotice(); notice != "" {
		t.Fatalf("healthy local watch notice = %q", notice)
	}
}

func TestLocalEnvWatchReplansRuntimeDetectors(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell}}, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	defer lw.Close()
	if err := os.WriteFile(filepath.Join(root, ".envrc"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	lw.Replan([]config.EnvDetector{{Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell, Snippet: "true"}})
	if got := provider.Detectors(); len(got) != 1 || got[0].Name != "direnv" {
		t.Fatalf("provider detectors = %+v", got)
	}
	fake.push(filewatch.Changed{Files: []string{filepath.Join(root, ".envrc")}})
	waitLocalDetected(t, provider, "direnv")
}

func TestLocalEnvWatchDegradesWithoutFsnotify(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	lw := startLocalEnvWatchWith(context.Background(), provider, root, nil, errors.New("fsnotify unavailable"))
	if lw != nil {
		t.Fatal("a watcher was started without fsnotify")
	}
	notice := provider.WatchNotice()
	if !strings.Contains(notice, "file watching is unavailable") || !strings.Contains(notice, "Env: Reload") {
		t.Fatalf("degradation notice = %q", notice)
	}
}

func TestLocalEnvWatchBoundsDetectorDirs(t *testing.T) {
	root := t.TempDir()
	var detectors []config.EnvDetector
	for i := 0; i < env.MaxDetectorWatchDirs+4; i++ {
		dir := strings.Repeat("d", i+1)
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		detectors = append(detectors, config.EnvDetector{Name: dir, Markers: []string{dir + "/marker"}, Kind: config.EnvKindShell})
	}
	provider := env.New(root, detectors, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	defer lw.Close()
	if got, want := fake.addedCount(), 1+env.MaxDetectorWatchDirs; got > want {
		t.Fatalf("watched %d dirs, want at most %d", got, want)
	}
	notice := provider.WatchNotice()
	if !strings.HasPrefix(notice, "Local environment:") || !strings.Contains(notice, "not watched") || !strings.Contains(notice, "watch bound") {
		t.Fatalf("local bound notice = %q", notice)
	}
	if strings.Contains(notice, "Remote environment:") {
		t.Fatalf("local bound notice leaked the remote wording: %q", notice)
	}
}

func TestEnvWatchLimitNoticesUseTheirScope(t *testing.T) {
	plan := &envWatchPlan{omitted: 3, omittedDirs: 2}
	local := localEnvWatchLimitNotice(plan)
	if !strings.HasPrefix(local, "Local environment:") || !strings.Contains(local, "3 detector marker path(s) in 2 director(ies)") || !strings.Contains(local, "Env: Reload") {
		t.Fatalf("local limit notice = %q", local)
	}
	if strings.Contains(local, "Remote environment:") {
		t.Fatalf("local limit notice has remote wording: %q", local)
	}
	remote := remoteEnvWatchLimitNotice(plan)
	if !strings.HasPrefix(remote, "Remote environment:") || !strings.Contains(remote, "3 detector marker path(s) in 2 director(ies)") || !strings.Contains(remote, "Env: Reload") {
		t.Fatalf("remote limit notice = %q", remote)
	}
	if strings.Contains(remote, "Local environment:") {
		t.Fatalf("remote limit notice has local wording: %q", remote)
	}
}

func TestLocalEnvWatchCloseIsCancellable(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	if lw == nil {
		t.Fatal("no local watcher")
	}
	lw.Close()
	fake.mu.Lock()
	closed := fake.closed
	fake.mu.Unlock()
	if !closed {
		t.Fatal("Close did not close the file watcher")
	}
}

func waitFakeWatchClosed(t *testing.T, fake *fakeEnvFileWatch) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		closed := fake.closed
		fake.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the file watcher was not closed (resource leak)")
}

func TestLocalEnvWatchParentCancellationReclaimsWatcher(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	if lw == nil {
		t.Fatal("no local watcher")
	}
	cancel()
	// No Close call: session-context cancellation alone must release fsnotify.
	waitFakeWatchClosed(t, fake)
}

func TestLocalEnvWatchRootAddFailureReclaimsAndNotices(t *testing.T) {
	root := t.TempDir()
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	provider := env.New(root, testLocalDetector("node", ".nvmrc"), func() bool { return true })
	fake := newFakeEnvFileWatch()
	fake.failDir = rootAbs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil); lw != nil {
		lw.Close()
		t.Fatal("a watcher started although the workspace root could not be watched")
	}
	waitFakeWatchClosed(t, fake)
	notice := provider.WatchNotice()
	if !strings.Contains(notice, "Local environment:") || !strings.Contains(notice, "workspace root cannot be watched") || !strings.Contains(notice, "Env: Reload") {
		t.Fatalf("root-Add-failure notice = %q", notice)
	}
	if strings.Contains(notice, "Remote environment:") {
		t.Fatalf("root-Add-failure notice leaked remote wording: %q", notice)
	}
}

func TestLocalEnvWatchPartialAddFailureRecoversAndClearsNotice(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, ".venv")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	provider := env.New(root, testLocalDetector("node", ".nvmrc"), func() bool { return true })
	fake := newFakeEnvFileWatch()
	fake.failDir = nested
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	if lw == nil {
		t.Fatal("no local watcher")
	}
	defer lw.Close()
	notice := provider.WatchNotice()
	if !strings.Contains(notice, "Local environment:") || !strings.Contains(notice, "cannot be watched") {
		t.Fatalf("partial-Add-failure notice = %q", notice)
	}
	// A later arm retries the failed directory after the transient error clears.
	fake.mu.Lock()
	fake.failDir = ""
	fake.mu.Unlock()
	fake.push(filewatch.Changed{Dirs: []string{root}})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if provider.WatchNotice() == "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("partial-Add-failure notice did not clear after retry: %q", provider.WatchNotice())
}

func TestLocalEnvWatchConstructorClosesWatcherOnEarlyFailure(t *testing.T) {
	root := t.TempDir()
	// Nil provider: ownership of the watcher still transferred and must be released.
	fake := newFakeEnvFileWatch()
	if lw := startLocalEnvWatchWith(context.Background(), nil, root, fake, nil); lw != nil {
		t.Fatal("a watcher started with a nil provider")
	}
	waitFakeWatchClosed(t, fake)

	// Watcher start error with a non-nil watcher: close it too.
	provider := env.New(root, nil, func() bool { return true })
	fake = newFakeEnvFileWatch()
	if lw := startLocalEnvWatchWith(context.Background(), provider, root, fake, errors.New("fsnotify start failed")); lw != nil {
		t.Fatal("a watcher started after an fsnotify start error")
	}
	waitFakeWatchClosed(t, fake)
	if notice := provider.WatchNotice(); !strings.Contains(notice, "file watching is unavailable") {
		t.Fatalf("start-error notice = %q", notice)
	}
}

func testLocalDetector(name, marker string) []config.EnvDetector {
	return []config.EnvDetector{{Name: name, Markers: []string{marker}, Kind: config.EnvKindShell, Snippet: "true"}}
}

func TestLocalEnvWatchRetryAfterRootAddFailure(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace-does-not-exist-yet")
	provider := env.New(root, testLocalDetector("node", ".nvmrc"), func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := startLocalEnvWatch(ctx, provider, root)
	if first != nil {
		first.Close()
		t.Skip("fsnotify accepted the nonexistent root on this platform")
	}
	if notice := provider.WatchNotice(); !strings.Contains(notice, "workspace root cannot be watched") {
		t.Fatalf("initial root-Add notice = %q", notice)
	}
	if !provider.WatchRetryNeeded() {
		t.Fatal("root-Add failure did not mark the watcher retry needed")
	}
	// The root becomes watchable; an Env: Reload-style retry must recover the watcher.
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".nvmrc"), []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	retry := &localEnvWatchRetry{
		ctx:   ctx,
		start: func() *localEnvWatch { return startLocalEnvWatch(ctx, provider, root) },
	}
	retry.Retry()
	if retry.current == nil {
		t.Fatal("retry did not start a watcher after the root became watchable")
	}
	defer retry.current.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		det, ok := provider.Detected()
		if ok && det.Name == "node" && provider.WatchNotice() == "" && !provider.WatchRetryNeeded() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	det, ok := provider.Detected()
	t.Fatalf("retry detection/watch notice = %+v, %v, %q", det, ok, provider.WatchNotice())
}

func TestLocalEnvWatchRetrySkipsLiveWatcher(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	if lw == nil {
		t.Fatal("no watcher")
	}
	defer lw.Close()
	called := false
	retry := &localEnvWatchRetry{ctx: ctx, current: lw, start: func() *localEnvWatch {
		called = true
		return nil
	}}
	retry.Retry()
	if called {
		t.Fatal("Retry started a second watcher while the first was live")
	}
	if retry.current != lw {
		t.Fatal("Retry replaced a live watcher")
	}
}

func TestLocalEnvWatchRetryReplacesDeadWatcher(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstFake := newFakeEnvFileWatch()
	first := startLocalEnvWatchWith(ctx, provider, root, firstFake, nil)
	if first == nil {
		t.Fatal("no first watcher")
	}
	first.Close()
	if !first.Dead() {
		t.Fatal("closed watcher was not marked dead")
	}
	secondFake := newFakeEnvFileWatch()
	second := startLocalEnvWatchWith(ctx, provider, root, secondFake, nil)
	if second == nil {
		t.Fatal("no second watcher")
	}
	defer second.Close()
	installed := false
	retry := &localEnvWatchRetry{
		ctx:     ctx,
		current: first,
		start:   func() *localEnvWatch { return second },
		install: func(*localEnvWatch) { installed = true },
	}
	retry.Retry()
	if retry.current != second || !installed {
		t.Fatal("Retry did not replace/install the dead watcher")
	}
}

func TestLocalEnvWatchRetryHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	retry := &localEnvWatchRetry{ctx: ctx, start: func() *localEnvWatch {
		called = true
		return nil
	}}
	retry.Retry()
	if called {
		t.Fatal("Retry started a watcher after the session context was cancelled")
	}
}

func TestLocalEnvWatchRetrySetInstallAppliesRecoveredWatcher(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := newFakeEnvFileWatch()
	var lw *localEnvWatch
	retry := &localEnvWatchRetry{
		ctx: ctx,
		start: func() *localEnvWatch {
			lw = startLocalEnvWatchWith(ctx, provider, root, fake, nil)
			return lw
		},
	}
	// Recover before the settings service exists, then install the callback afterwards:
	// SetInstall must apply it to the already-recovered watcher.
	retry.Retry()
	if retry.Current() == nil {
		t.Fatal("retry did not recover a watcher")
	}
	defer retry.Current().Close()
	installed := make(chan *localEnvWatch, 1)
	retry.SetInstall(func(got *localEnvWatch) { installed <- got })
	select {
	case got := <-installed:
		if got != retry.Current() {
			t.Fatalf("installed %p, want current %p", got, retry.Current())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SetInstall did not apply to the already-recovered watcher")
	}
}

func TestLocalEnvWatchRetrySetInstallConcurrent(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 4)
	retry := &localEnvWatchRetry{
		ctx: ctx,
		start: func() *localEnvWatch {
			fake := newFakeEnvFileWatch()
			lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
			started <- struct{}{}
			return lw
		},
	}
	installed := make(chan struct{}, 4)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		retry.SetInstall(func(*localEnvWatch) { installed <- struct{}{} })
	}()
	go func() {
		defer wg.Done()
		retry.Retry()
	}()
	wg.Wait()
	if retry.Current() != nil {
		retry.Current().Close()
	}
	// Both the retry and the late install may produce an install callback; at least one
	// path must have synchronized without a race/panic (the real oracle is -race).
	for {
		select {
		case <-installed:
			return
		default:
			if retry.Current() == nil {
				t.Fatal("Retry did not recover a watcher")
			}
			// The install callback may have run before Retry completed; Current is enough.
			return
		}
	}
}
