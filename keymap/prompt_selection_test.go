package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// TestPromptSelectionBindingsAreDeliberatelyAbsent: Fresh's prompt has a real text
// selection and binds Shift+Left/Right/Home/End and Ctrl+Shift+Left/Right to extend it.
// gvim's prompt text field has no selection model, so those move-selecting/select-word
// actions must not be faked into the keymap. Ctrl+A is the invisible whole-input prefill
// (default/VS Code); macOS and Emacs keep their Fresh prompt Ctrl+A Home binding.
func TestPromptSelectionBindingsAreDeliberatelyAbsent(t *testing.T) {
	selectAll := map[string]string{
		"default":   "prompt_select_all",
		"macos":     "prompt_move_start",
		"macos-gui": "prompt_move_start",
		"emacs":     "prompt_move_start",
		"vscode":    "prompt_select_all",
	}
	for preset, want := range selectAll {
		m := mustPreset(t, preset)
		if b, ok := m.ResolveExact("ctrl+a", keymap.ContextPrompt); !ok || b.Action != want {
			t.Errorf("%s: Ctrl+A prompt binding = %+v, %v; want %s", preset, b, ok, want)
		}
		for _, chord := range []string{
			"shift+left", "shift+right", "shift+home", "shift+end",
			"ctrl+shift+left", "ctrl+shift+right",
		} {
			if b, ok := m.ResolveExact(chord, keymap.ContextPrompt); ok {
				t.Errorf("%s: %s prompt binding = %s, want deliberately unbound (no selection model)", preset, chord, b.Action)
			}
		}
	}
}
