package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

func watcherForDir(fs *fakeWatchFS, dir string) *fakeWatchWatcher {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, d := range fs.dirs {
		if d == dir && i < len(fs.watchers) {
			return fs.watchers[i]
		}
	}
	return nil
}

func TestRemoteEnvWatchReplansOnDetectorChange(t *testing.T) {
	var detect atomic.Int32
	run := func(context.Context, string) (string, error) {
		detect.Add(1)
		return "", nil
	}
	provider := env.NewRemote("/remote/proj", []config.EnvDetector{{
		Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell, Snippet: "true",
	}}, func() bool { return true }, run)
	provider.Warm(context.Background())
	raw := &fakeWatchFS{}
	fs := &statDirFS{fakeWatchFS: raw, dirs: map[string]bool{"/remote/proj/config": true}}
	slot := watchSlot(raw)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	if watch == nil {
		t.Fatal("no watcher")
	}
	defer watch.Close()
	waitWatchCount(t, raw, 1)
	initialDetect := detect.Load()

	watch.Replan([]config.EnvDetector{
		{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"},
		{Name: "custom", Markers: []string{"config/env.toml"}, Require: []string{"config/bin/python"}, Kind: config.EnvKindShell, Snippet: "true"},
	})
	// The provider now exposes the new detectors; the plan re-arms with the existing
	// config parent directory and the replan re-warms detection once.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := provider.Detectors(); len(got) == 2 && got[0].Name == "node" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := provider.Detectors(); len(got) != 2 || got[0].Name != "node" {
		t.Fatalf("provider detectors after replan = %+v", got)
	}
	waitDetectIncrease(t, &detect, initialDetect)
	waitWatchCount(t, raw, 3) // initial root, re-armed root, and the new config dir
	if w := watcherForDir(raw, "/remote/proj/config"); w == nil {
		t.Fatalf("replan did not watch the config parent: dirs=%v", raw.dirs)
	}
	// A change to the new custom marker invalidates detection through the new plan.
	initialDetect = detect.Load()
	w := watcherForDir(raw, "/remote/proj/config")
	w.push(hostos.WatchEvent{Path: "/remote/proj/config/env.toml", Kind: "write"})
	waitDetectIncrease(t, &detect, initialDetect)
}

func TestRemoteEnvWatchReplanRespectsBounds(t *testing.T) {
	var detect atomic.Int32
	run := func(context.Context, string) (string, error) {
		detect.Add(1)
		return "", nil
	}
	provider := env.NewRemote("/remote/proj", []config.EnvDetector{{
		Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell,
	}}, func() bool { return true }, run)
	raw := &fakeWatchFS{}
	files := map[string]bool{}
	var detectors []config.EnvDetector
	for i := 0; i < env.MaxDetectorWatchDirs+4; i++ {
		dir := strings.Repeat("d", i+1)
		dirs := "/remote/proj/" + dir
		files[dirs] = true
		detectors = append(detectors, config.EnvDetector{Name: dir, Markers: []string{dir + "/marker"}, Kind: config.EnvKindShell})
	}
	fs := &statDirFS{fakeWatchFS: raw, dirs: files}
	slot := watchSlot(raw)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	defer watch.Close()
	waitWatchCount(t, raw, 1)
	watch.Replan(detectors)
	waitWatchCount(t, raw, 1+env.MaxDetectorWatchDirs)
	if notice := provider.WatchNotice(); !strings.HasPrefix(notice, "Remote environment:") || !strings.Contains(notice, "not watched") || !strings.Contains(notice, "watch bound") {
		t.Fatalf("replan bound notice = %q", notice)
	}
}
