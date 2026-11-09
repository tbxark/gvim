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
	if notice := provider.WatchNotice(); !strings.Contains(notice, "not watched") || !strings.Contains(notice, "watch bound") {
		t.Fatalf("bound notice = %q", notice)
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
