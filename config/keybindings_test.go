package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// The project layer cannot rebind keys: a project file that names keybindings or keymap is
// ignored with a diagnostic, as it can only restrict plugins, never widen them. Fresh lets a
// project's keybindings replace the user's; here a repository could take over Ctrl+S, so the
// keys stay the user's.
func TestProjectLayerCannotRebindKeys(t *testing.T) {
	tests := []struct {
		name    string
		project string
		key     string
	}{
		{"keybindings list", `{"keybindings": [{"key": "s", "modifiers": ["ctrl"], "action": "quit"}]}`, "keybindings"},
		{"keybindings object", `{"keybindings": {"bindings": [{"key": "s", "modifiers": ["ctrl"], "action": "quit"}]}}`, "keybindings"},
		{"keymap", `{"keymap": "emacs"}`, "keymap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := `{"keymap": "vscode", "keybindings": [{"key": "u", "modifiers": ["alt"], "action": "to_upper_case"}]}`
			cfg, diags := loadLayers(t, user, tt.project)
			if cfg.Keymap != "vscode" {
				t.Errorf("keymap = %q, want the user's vscode", cfg.Keymap)
			}
			if !strings.Contains(string(cfg.Keybindings), "to_upper_case") || strings.Contains(string(cfg.Keybindings), "quit") {
				t.Errorf("keybindings = %s, want the user's list only", cfg.Keybindings)
			}
			var found bool
			for _, d := range diags {
				if d.Key == tt.key && strings.Contains(d.Message, "project config cannot") && strings.Contains(d.Path, ".gvim") {
					found = true
				}
			}
			if !found {
				t.Errorf("no project diagnostic for %s: %v", tt.key, diags)
			}
			if got := cfg.Source(tt.key); strings.Contains(got, ".gvim") {
				t.Errorf("Source(%s) = %q, want the user file", tt.key, got)
			}
		})
	}
}

// The documented form of the keybindings value is the list of Fresh's keybinding editor.
func TestKeybindingsAcceptTheDocumentedList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{
  // my keys
  "keybindings": [
    { "key": "s", "modifiers": ["ctrl"], "action": "save", "when": "normal" },
  ],
}`)
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if !jsonEqual(t, cfg.Keybindings, `[{"key":"s","modifiers":["ctrl"],"action":"save","when":"normal"}]`) {
		t.Fatalf("keybindings = %s", cfg.Keybindings)
	}
}
