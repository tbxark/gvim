package app

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
)

func detectorsConfig(name, marker string) config.Config {
	cfg := config.Defaults()
	cfg.Env = config.EnvConfig{Detectors: []config.EnvDetector{{
		Name: name, Markers: []string{marker}, Kind: config.EnvKindShell, Snippet: "true",
	}}}
	return cfg
}

func TestSettingsDetectorChangeReplansProviderWithoutWatcher(t *testing.T) {
	provider := env.NewRemote("/remote/proj", nil, func() bool { return true }, func(context.Context, string) (string, error) {
		return "", nil
	})
	svc := &settingsService{env: provider}
	previous := detectorsConfig("old", ".old")
	next := detectorsConfig("new", ".nvmrc")
	svc.applyEnvDetectorChange(previous, next)
	got := provider.Detectors()
	if len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("provider detectors = %+v", got)
	}
	// An unchanged detector list is a no-op.
	svc.applyEnvDetectorChange(next, next)
}

func TestSettingsDetectorChangeReplansWatcher(t *testing.T) {
	var detect atomic.Int32
	run := func(context.Context, string) (string, error) {
		detect.Add(1)
		return "", nil
	}
	previous := detectorsConfig("old", ".old")
	provider := env.NewRemote("/remote/proj", previous.EnvDetectors(), func() bool { return true }, run)
	raw := &fakeWatchFS{}
	fs := &statDirFS{fakeWatchFS: raw, dirs: map[string]bool{"/remote/proj/config": true}}
	slot := watchSlot(raw)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	defer watch.Close()
	waitWatchCount(t, raw, 1)

	next := detectorsConfig("custom", "config/env.toml")
	svc := &settingsService{env: provider, envWatch: watch}
	svc.applyEnvDetectorChange(previous, next)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := provider.Detectors()
		if len(got) == 1 && got[0].Name == "custom" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := provider.Detectors(); len(got) != 1 || got[0].Name != "custom" {
		t.Fatalf("provider detectors = %+v", got)
	}
	waitWatchCount(t, raw, 3) // initial root, re-armed root, custom config dir
	if w := watcherForDir(raw, "/remote/proj/config"); w == nil {
		t.Fatalf("settings detector change did not re-plan the watcher: %v", raw.dirs)
	}
}

func TestSettingsDetectorChangeAppliesToLocalProvider(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".nvmrc"), []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := detectorsConfig("old", ".old")
	provider := env.New(root, previous.EnvDetectors(), func() bool { return true })
	if det, ok := provider.Detected(); ok || det.Name != "" {
		t.Fatalf("initial local detection = %+v, %v", det, ok)
	}
	svc := &settingsService{env: provider}
	next := detectorsConfig("node", ".nvmrc")
	svc.applyEnvDetectorChange(previous, next)
	if got := provider.Detectors(); len(got) != 1 || got[0].Name != "node" {
		t.Fatalf("local provider detectors = %+v", got)
	}
	if det, ok := provider.Detected(); !ok || det.Name != "node" {
		t.Fatalf("local detection after Reload = %+v, %v", det, ok)
	}
}
