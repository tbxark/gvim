package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// TestOverlayContextPresets pins the Fresh rows the presets carry for the overlay
// contexts, per preset: the default set, each child's own idiom, and the two commands
// gvim's dock and Settings surfaces support.
func TestOverlayContextPresets(t *testing.T) {
	tests := []struct {
		preset string
		ctx    keymap.Context
		chord  string
		action string
	}{
		// The default set.
		{"default", keymap.ContextPrompt, "enter", "prompt_confirm"},
		{"default", keymap.ContextPrompt, "escape", "prompt_cancel"},
		{"default", keymap.ContextPrompt, "ctrl+e", "prompt_move_end"},
		{"default", keymap.ContextPrompt, "alt+b", "prompt_move_word_left"},
		{"default", keymap.ContextPrompt, "ctrl+w", "prompt_delete_word_backward"},
		{"default", keymap.ContextPrompt, "ctrl+k", "prompt_delete_to_line_end"},
		{"default", keymap.ContextPrompt, "ctrl+v", "prompt_paste"},
		{"default", keymap.ContextSearchPrompt, "f3", "find_next"},
		{"default", keymap.ContextSearchPrompt, "shift+f3", "find_previous"},
		{"default", keymap.ContextSearchPrompt, "alt+c", "toggle_search_case_sensitive"},
		{"default", keymap.ContextSearchPrompt, "alt+r", "toggle_search_regex"},
		{"default", keymap.ContextPopup, "down", "popup_select_next"},
		{"default", keymap.ContextPopup, "pagedown", "popup_page_down"},
		{"default", keymap.ContextPopup, "enter", "popup_confirm"},
		{"default", keymap.ContextPopup, "escape", "popup_cancel"},
		{"default", keymap.ContextMenu, "right", "menu_right"},
		{"default", keymap.ContextMenu, "enter", "menu_execute"},
		{"default", keymap.ContextMenu, "escape", "menu_close"},
		{"default", keymap.ContextSettings, "escape", "close_settings"},
		{"default", keymap.ContextSettings, "enter", "settings_activate"},
		{"default", keymap.ContextSettings, "delete", "settings_inherit"},
		{"default", keymap.ContextDock, "alt+o", "toggle_dock_focus"},
		{"default", keymap.ContextDock, "alt+shift+n", "focus_next_sidebar_section"},
		// The macOS preset adds its own word-move and find-step rows.
		{"macos", keymap.ContextPrompt, "ctrl+a", "prompt_move_start"},
		{"macos", keymap.ContextPrompt, "alt+left", "prompt_move_word_left"},
		{"macos", keymap.ContextPrompt, "alt+backspace", "prompt_delete_word_backward"},
		{"macos", keymap.ContextSearchPrompt, "ctrl+g", "find_next"},
		{"macos", keymap.ContextSearchPrompt, "ctrl+alt+g", "find_previous"},
		// macos-gui inherits macOS and adds the Cmd find steps.
		{"macos-gui", keymap.ContextSearchPrompt, "super+g", "find_next"},
		{"macos-gui", keymap.ContextSearchPrompt, "super+shift+g", "find_previous"},
		// Emacs adds its C-x-free readline keys and overrides the default Ctrl+F.
		{"emacs", keymap.ContextPrompt, "ctrl+g", "prompt_cancel"},
		{"emacs", keymap.ContextPrompt, "ctrl+f", "prompt_move_right"},
		{"emacs", keymap.ContextPrompt, "ctrl+n", "prompt_select_next"},
		{"emacs", keymap.ContextPrompt, "alt+d", "prompt_delete_word_forward"},
		{"emacs", keymap.ContextPrompt, "ctrl+y", "prompt_paste"},
		{"emacs", keymap.ContextSearchPrompt, "ctrl+s", "find_next"},
		{"emacs", keymap.ContextSearchPrompt, "ctrl+r", "find_previous"},
		{"emacs", keymap.ContextPopup, "ctrl+n", "popup_select_next"},
		{"emacs", keymap.ContextPopup, "ctrl+g", "popup_cancel"},
		{"emacs", keymap.ContextMenu, "ctrl+f", "menu_right"},
		{"emacs", keymap.ContextMenu, "ctrl+g", "menu_close"},
		// A child that only inherits still resolves the default overlay rows.
		{"vscode", keymap.ContextPrompt, "enter", "prompt_confirm"},
		{"vscode", keymap.ContextSearchPrompt, "f3", "find_next"},
	}
	for _, tt := range tests {
		m := mustPreset(t, tt.preset)
		b, ok := m.Resolve(tt.chord, tt.ctx)
		if !ok || b.Action != tt.action {
			t.Errorf("%s %s %s = %+v, %v; want %s", tt.preset, tt.ctx, tt.chord, b, ok, tt.action)
		}
	}
}
