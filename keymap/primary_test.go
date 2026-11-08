package keymap_test

import (
	"testing"
)

// A menu or palette shows Primary. Close Buffer runs "close", which the keys bind as
// "close_tab", and a preset's own idiom (Emacs's C-x C-w) beats a key it only inherits.
// The gvim-specific keys keep a key for the commands Fresh leaves to the palette (Save As,
// fold, duplicate line).
func TestPrimaryLabelsCloseAndPrefersThePresetsOwnBinding(t *testing.T) {
	tests := []struct {
		preset, action, want string
	}{
		{"default", "close", "alt+w"},
		{"macos", "close", "alt+w"},
		{"macos-gui", "close", "super+w"},
		{"vscode", "close", "alt+w"},
		{"emacs", "close", "ctrl+x k"},
		{"emacs", "save", "ctrl+x ctrl+s"},
		{"emacs", "save_as", "ctrl+x ctrl+w"},
		{"default", "save_as", "ctrl+shift+s"},
		{"macos", "save_as", "alt+shift+s"},
		{"macos-gui", "save_as", "shift+super+s"},
		{"vscode", "save_as", "ctrl+shift+s"},
		{"default", "close_tab", "alt+w"},
		{"default", "toggle_fold", "alt+z"},
		{"macos", "toggle_fold", "alt+z"},
		{"macos-gui", "toggle_fold", "alt+z"},
		{"emacs", "toggle_fold", "ctrl+x f"},
		{"vscode", "toggle_fold", "ctrl+k ctrl+l"},
		{"default", "duplicate_line", "ctrl+shift+d"},
		{"macos", "duplicate_line", "ctrl+shift+d"},
		{"macos-gui", "duplicate_line", "shift+super+d"},
	}
	for _, tc := range tests {
		t.Run(tc.preset+"/"+tc.action, func(t *testing.T) {
			m := mustPreset(t, tc.preset)
			b, ok := m.Primary(tc.action)
			if !ok || b.Chord != tc.want {
				t.Errorf("Primary(%s) = %q, %v; want %q", tc.action, b.Chord, ok, tc.want)
			}
		})
	}
}
