package app

import (
	"context"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

// statDirFS answers Stat for a fixed set of directories, so the watcher can arm nested
// custom-detector parents.
type statDirFS struct {
	*fakeWatchFS
	dirs map[string]bool
}

func (f *statDirFS) Stat(_ context.Context, p string) (hostos.FileStat, error) {
	if f.dirs[p] {
		return hostos.FileStat{Kind: "dir"}, nil
	}
	return hostos.FileStat{}, fs.ErrNotExist
}

func customDetectorProvider(t *testing.T, detect *atomic.Int32) *env.Remote {
	t.Helper()
	detectors := []config.EnvDetector{
		{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"},
		{Name: "custom", Markers: []string{"config/env.toml"}, Require: []string{"config/bin/python"}, Kind: config.EnvKindShell, Snippet: "true"},
	}
	run := func(context.Context, string) (string, error) {
		detect.Add(1)
		return "", nil
	}
	return env.NewRemote("/remote/proj", detectors, func() bool { return true }, run)
}

func waitWatchCount(t *testing.T, fs *fakeWatchFS, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fs.watchCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("watcher count = %d, want %d", fs.watchCount(), want)
}

func TestRemoteEnvWatchCoversCustomDetectorPaths(t *testing.T) {
	var detect atomic.Int32
	provider := customDetectorProvider(t, &detect)
	raw := &fakeWatchFS{}
	fs := &statDirFS{fakeWatchFS: raw, dirs: map[string]bool{
		"/remote/proj/config":     true,
		"/remote/proj/config/bin": true,
	}}
	slot := watchSlot(raw)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	defer watch.Close()
	waitWatchCount(t, raw, 3)
	raw.mu.Lock()
	dirs := append([]string(nil), raw.dirs...)
	raw.mu.Unlock()
	joined := strings.Join(dirs, ",")
	for _, want := range []string{"/remote/proj", "/remote/proj/config", "/remote/proj/config/bin"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("watched dirs = %v, want %s", dirs, want)
		}
	}

	// A root-level custom marker invalidates detection.
	initial := detect.Load()
	raw.watchers[0].push(hostos.WatchEvent{Path: "/remote/proj/.nvmrc", Kind: "write"})
	waitDetectIncrease(t, &detect, initial)

	// A nested custom marker event on the watched parent does too.
	initial = detect.Load()
	fs.nestedEvent(raw.watchers[1], hostos.WatchEvent{Path: "/remote/proj/config/env.toml", Kind: "write"})
	waitDetectIncrease(t, &detect, initial)

	// A require path event from the deeper parent does too.
	initial = detect.Load()
	fs.nestedEvent(raw.watchers[2], hostos.WatchEvent{Path: "/remote/proj/config/bin/python", Kind: "write"})
	waitDetectIncrease(t, &detect, initial)
}

func (f *statDirFS) nestedEvent(w *fakeWatchWatcher, ev hostos.WatchEvent) { w.push(ev) }

func waitDetectIncrease(t *testing.T, detect *atomic.Int32, initial int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if detect.Load() > initial {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("custom detector change did not invalidate detection: %d → %d", initial, detect.Load())
}

func TestRemoteEnvWatchBoundsCustomDetectorDirs(t *testing.T) {
	var detect atomic.Int32
	var detectors []config.EnvDetector
	dirs := map[string]bool{}
	for i := 0; i < env.MaxDetectorWatchDirs+4; i++ {
		dir := strings.Repeat("d", i+1)
		detectors = append(detectors, config.EnvDetector{Name: dir, Markers: []string{dir + "/marker"}, Kind: config.EnvKindShell})
		dirs["/remote/proj/"+dir] = true
	}
	run := func(context.Context, string) (string, error) {
		detect.Add(1)
		return "", nil
	}
	provider := env.NewRemote("/remote/proj", detectors, func() bool { return true }, run)
	raw := &fakeWatchFS{}
	fs := &statDirFS{fakeWatchFS: raw, dirs: dirs}
	slot := watchSlot(raw)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	defer watch.Close()
	waitWatchCount(t, raw, 1+env.MaxDetectorWatchDirs)
	if notice := provider.WatchNotice(); !strings.Contains(notice, "not watched") || !strings.Contains(notice, "watch bound") {
		t.Fatalf("bound notice = %q", notice)
	}
}
