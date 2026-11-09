package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// TestPromptSelectionBindings: Fresh's prompt has a real text selection. gvim now binds
// the move-selecting and select-word chords to the prompt's selection model; Ctrl+A is
// select-all in the presets that do not override it with their Fresh Home binding.
func TestPromptSelectionBindings(t *testing.T) {
	moveSelecting := map[string]string{
		"shift+left":       "prompt_move_left_selecting",
		"shift+right":      "prompt_move_right_selecting",
		"shift+home":       "prompt_move_home_selecting",
		"shift+end":        "prompt_move_end_selecting",
		"ctrl+shift+left":  "prompt_select_word_left",
		"ctrl+shift+right": "prompt_select_word_right",
	}
	selectAll := map[string]string{
		"default":   "prompt_select_all",
		"macos":     "prompt_move_start",
		"macos-gui": "prompt_move_start",
		"emacs":     "prompt_move_start",
		"vscode":    "prompt_select_all",
	}
	for preset, wantCtrlA := range selectAll {
		m := mustPreset(t, preset)
		if b, ok := m.ResolveExact("ctrl+a", keymap.ContextPrompt); !ok || b.Action != wantCtrlA {
			t.Errorf("%s: Ctrl+A prompt binding = %+v, %v; want %s", preset, b, ok, wantCtrlA)
		}
		for chord, action := range moveSelecting {
			if b, ok := m.ResolveExact(chord, keymap.ContextPrompt); !ok || b.Action != action {
				t.Errorf("%s: %s prompt binding = %+v, %v; want %s", preset, chord, b, ok, action)
			}
		}
	}
}
