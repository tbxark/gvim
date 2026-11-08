package configview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// userConfigPath writes body as a user config file in a temp directory and
// returns its path.
func userConfigPath(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gvim", "config.json")
	if body != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// TestLoadReportsKeybindingWarnings checks that the view reports the user's
// keybindings that the keymap cannot apply, in file order, and warns about
// nothing when the list is clean.
func TestLoadReportsKeybindingWarnings(t *testing.T) {
	path := userConfigPath(t, `{"keybindings": [
  {"key": "Weird", "modifiers": ["ctrl"], "action": "save"},
  {"key": "u", "modifiers": ["alt"], "action": "no_such_action"},
  {"key": "esc", "modifiers": ["control"], "action": "save"}
]}`)
	view, err := Load(t.TempDir(), Options{Config: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Warnings) != 2 ||
		!strings.Contains(view.Warnings[0], `keybindings[0]: unknown key name "Weird"`) ||
		!strings.Contains(view.Warnings[1], `keybindings[1]: unknown action "no_such_action"`) {
		t.Fatalf("warnings = %q, want the two bad entries and not the valid Fresh-form one", view.Warnings)
	}
	clean := userConfigPath(t, `{"keybindings": [{"key": "u", "modifiers": ["alt"], "action": "save"}]}`)
	view, err = Load(t.TempDir(), Options{Config: clean})
	if err != nil || len(view.Warnings) != 0 {
		t.Fatalf("a clean config warns: %q, %v", view.Warnings, err)
	}
}
