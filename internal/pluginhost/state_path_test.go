package pluginhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestLoaderOptionsMigratesPluginState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("APPDATA", home)
	oldDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	stateDir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(oldDir, "plugin-state.json")
	dest := filepath.Join(stateDir, "plugin-state.json")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := loaderOptions(config.Defaults(), Options{})
	if opts.StateFile != dest {
		t.Fatalf("state path = %q, want %q", opts.StateFile, dest)
	}
	if data, err := os.ReadFile(dest); err != nil || string(data) != "legacy" {
		t.Fatalf("migrated = %q, %v", data, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy state remains: %v", err)
	}
	if err := os.WriteFile(old, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	loaderOptions(config.Defaults(), Options{})
	if data, err := os.ReadFile(dest); err != nil || string(data) != "legacy" {
		t.Fatalf("existing state overwritten: %q, %v", data, err)
	}
}
