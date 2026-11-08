package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/keymap"
)

// A menu or palette shows Primary. Close Buffer runs "close", which the keys bind as
// "close_tab", and a preset's own idiom (Emacs's C-x C-w) beats a key it only inherits.
func TestPrimaryLabelsCloseAndPrefersThePresetsOwnBinding(t *testing.T) {
	tests := []struct {
		preset, action, want string
	}{
		{"default", "close", "alt+w"},
		{"macos", "close", "ctrl+w"},
		{"macos-gui", "close", "super+w"},
		{"vscode", "close", "ctrl+w"},
		{"emacs", "close", "ctrl+x k"},
		{"emacs", "save", "ctrl+x ctrl+s"},
		{"emacs", "save_as", "ctrl+x ctrl+w"},
		{"default", "save_as", "ctrl+shift+s"},
		{"default", "close_tab", "alt+w"},
	}
	for _, tc := range tests {
		t.Run(tc.preset+"/"+tc.action, func(t *testing.T) {
			m, err := keymap.ByName(tc.preset)
			if err != nil {
				t.Fatal(err)
			}
			b, ok := m.Primary(tc.action)
			if !ok || b.Chord != tc.want {
				t.Errorf("Primary(%s) = %q, %v; want %q", tc.action, b.Chord, ok, tc.want)
			}
		})
	}
}
