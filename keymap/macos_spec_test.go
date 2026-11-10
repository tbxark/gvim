package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// TestMacosBindings pins Fresh's macOS preset: it avoids Ctrl+Shift chords and the ASCII
// control characters a terminal cannot send, and moves the editor's common keys to the
// Ctrl and Option chords macOS terminals have.
func TestMacosBindings(t *testing.T) {
	m := mustPreset(t, "macos")
	tests := []struct {
		chord  string
		ctx    keymap.Context
		action string
	}{
		{"ctrl+n", keymap.ContextNormal, "new"},
		{"ctrl+o", keymap.ContextNormal, "open"},
		{"ctrl+s", keymap.ContextNormal, "save"},
		{"alt+shift+s", keymap.ContextNormal, "save_as"},
		{"ctrl+q", keymap.ContextNormal, "quit"},
		{"ctrl+z", keymap.ContextNormal, "undo"},
		{"ctrl+r", keymap.ContextNormal, "redo"},
		{"ctrl+y", keymap.ContextNormal, "redo"},
		{"ctrl+x", keymap.ContextNormal, "cut"},
		{"ctrl+c", keymap.ContextNormal, "copy"},
		{"ctrl+v", keymap.ContextNormal, "paste"},
		{"ctrl+f", keymap.ContextNormal, "search"},
		{"ctrl+g", keymap.ContextNormal, "find_next"},
		{"ctrl+alt+g", keymap.ContextNormal, "find_previous"},
		{"F3", keymap.ContextNormal, "find_next"},
		{"shift+F3", keymap.ContextNormal, "find_previous"},
		{"ctrl+l", keymap.ContextNormal, "goto_line"},
		{"ctrl+k", keymap.ContextNormal, "delete_to_line_end"},
		{"ctrl+alt+k", keymap.ContextNormal, "delete_line"},
		{"ctrl+u", keymap.ContextNormal, "delete_to_line_start"},
		{"ctrl+d", keymap.ContextNormal, "add_cursor_next_match"},
		{"ctrl+b", keymap.ContextNormal, "toggle_file_explorer"},
		{"ctrl+e", keymap.ContextNormal, "move_line_end"},
		{"ctrl+a", keymap.ContextNormal, "move_line_start"},
		{"ctrl+t", keymap.ContextNormal, "new"},
		{"ctrl+-", keymap.ContextNormal, "navigate_back"},
		{"ctrl+=", keymap.ContextNormal, "navigate_forward"},
		{"ctrl+]", keymap.ContextNormal, "navigate_forward"},
		{"ctrl+.", keymap.ContextNormal, "lsp_goto_definition"},
		{"ctrl+,", keymap.ContextNormal, "lsp_references"},
		{"ctrl+alt+r", keymap.ContextNormal, "lsp_rename"},
		{"ctrl+pagedown", keymap.ContextNormal, "next_buffer"},
		{"ctrl+pageup", keymap.ContextNormal, "prev_buffer"},
		{"alt+up", keymap.ContextNormal, "move_line_up"},
		{"alt+down", keymap.ContextNormal, "move_line_down"},
		{"alt+left", keymap.ContextNormal, "move_word_left"},
		{"alt+right", keymap.ContextNormal, "move_word_end"},
		{"alt+f", keymap.ContextNormal, "move_word_right"},
		{"alt+b", keymap.ContextNormal, "move_word_left"},
		{"alt+backspace", keymap.ContextNormal, "delete_word_backward"},
		{"alt+delete", keymap.ContextNormal, "delete_word_forward"},
		{"ctrl+left", keymap.ContextNormal, "move_word_left"},
		{"ctrl+right", keymap.ContextNormal, "move_word_end"},
		{"ctrl+shift+left", keymap.ContextNormal, "select_word_left"},
		{"ctrl+shift+right", keymap.ContextNormal, "select_word_end"},
		{"ctrl+'", keymap.ContextNormal, "jump_to_next_error"},
		{"ctrl+;", keymap.ContextNormal, "jump_to_previous_error"},
		{"ctrl+e", keymap.ContextFileExplorer, "focus_editor"},
		{"ctrl+b", keymap.ContextFileExplorer, "focus_editor"},
		{"ctrl+backslash", keymap.ContextTerminal, "toggle_keyboard_capture"},
		{"ctrl+/", keymap.ContextTerminal, "toggle_keyboard_capture"},
		{"ctrl+p", keymap.ContextGlobal, "quick_open"},
	}
	for _, tc := range tests {
		b, ok := m.Resolve(tc.chord, tc.ctx)
		if !ok || b.Action != tc.action {
			t.Errorf("macos %s/%s = %q (%v), want %q", tc.ctx, tc.chord, b.Action, ok, tc.action)
		}
	}
}

// TestMacosAltFIsAWordMoveAndFileKeepsItsAccelerator: Fresh's macOS preset moves Alt+F to
// the next word in the normal context, and the File menu keeps the global Alt+F
// accelerator. The normal binding wins in the editor, as Fresh's resolver does.
func TestMacosAltFIsAWordMoveAndFileKeepsItsAccelerator(t *testing.T) {
	m := mustPreset(t, "macos")
	if b, ok := m.Resolve("alt+f", keymap.ContextNormal); !ok || b.Action != "move_word_right" {
		t.Errorf("macos alt+f in normal = %+v, %v; want move_word_right", b, ok)
	}
	if b, ok := m.Resolve("alt+f", keymap.ContextGlobal); !ok || b.Action != "menu_open" || b.Args["name"] != "File" {
		t.Errorf("macos alt+f globally = %+v, %v; want menu_open File", b, ok)
	}
	if letter, ok := m.MenuMnemonic("File"); !ok || letter != 'f' {
		t.Errorf("macos menu mnemonic of File = %q, %v; want f", letter, ok)
	}
}

func TestMacosPrimaryShortcuts(t *testing.T) {
	m := mustPreset(t, "macos")
	tests := map[string]string{
		"show_help":            "F1",
		"new":                  "ctrl+t",
		"open":                 "ctrl+o",
		"save":                 "ctrl+s",
		"save_as":              "alt+shift+s",
		"close_tab":            "alt+w",
		"quit":                 "ctrl+q",
		"undo":                 "ctrl+z",
		"redo":                 "ctrl+r",
		"cut":                  "ctrl+x",
		"copy":                 "ctrl+c",
		"paste":                "ctrl+v",
		"search":               "ctrl+f",
		"find_next":            "ctrl+g",
		"goto_line":            "ctrl+l",
		"delete_line":          "ctrl+alt+k",
		"delete_to_line_end":   "ctrl+k",
		"toggle_file_explorer": "ctrl+b",
		"command_palette":      "ctrl+shift+p",
		"next_buffer":          "ctrl+pagedown",
		"prev_buffer":          "ctrl+pageup",
		"navigate_back":        "ctrl+-",
		"navigate_forward":     "ctrl+=",
		"move_word_right":      "alt+f",
	}
	for action, want := range tests {
		b, ok := m.Primary(action)
		if !ok || b.Chord != want {
			t.Errorf("macos Primary(%s) = %q (%v), want %q", action, b.Chord, ok, want)
		}
	}
	// Fresh's macOS preset has no Select All key: Ctrl+A moves to the start of the line,
	// and Cmd+A belongs to the GUI preset.
	if b, ok := m.Primary("select_all"); ok {
		t.Errorf("macos Primary(select_all) = %q; want none", b.Chord)
	}
	if b, ok := m.Primary("focus_file_explorer"); ok {
		t.Errorf("macos Primary(focus_file_explorer) = %q; want none: Ctrl+E moves to the end of the line", b.Chord)
	}
}

// TestPrimaryIgnoresOverriddenBinding checks that an inherited binding replaced
// by a child keymap is not shown. macos rebinds Ctrl+G to find_next, so
// goto_line's shortcut must be Ctrl+L and never the overridden Ctrl+G.
func TestPrimaryIgnoresOverriddenBinding(t *testing.T) {
	m := mustPreset(t, "macos")
	if b, ok := m.Primary("goto_line"); !ok || b.Chord != "ctrl+l" {
		t.Fatalf("macos Primary(goto_line) = %q (%v), want ctrl+l", b.Chord, ok)
	}
	src := `{"inherits":"default","bindings":[
		{"key":"g","modifiers":["ctrl"],"action":"other","when":"normal"}]}`
	child, err := keymap.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := child.Primary("goto_line"); ok {
		t.Fatalf("goto_line shows %q after its ctrl+g was rebound; want no binding", b.Chord)
	}
}
