package pluginhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

// The plugin state file lives in the state directory; a plugin-state.json left in the
// config directory by an older gvim is not moved there.
func TestLoaderOptionsStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("APPDATA", home)
	cfgDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	stateDir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(cfgDir, "plugin-state.json")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(stateDir, "plugin-state.json")
	if opts := loaderOptions(config.Defaults(), Options{}); opts.StateFile != dest {
		t.Fatalf("state path = %q, want %q", opts.StateFile, dest)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("state file was created from the old one: %v", err)
	}
	if data, err := os.ReadFile(old); err != nil || string(data) != "old" {
		t.Fatalf("old file = %q, %v; want it untouched", data, err)
	}
}
