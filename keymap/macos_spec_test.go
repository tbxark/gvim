package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// specKeys are the macOS chords of migration/PHASE1-EDITOR.md section 4 and
// the action each must resolve to in the normal context.
var specKeys = []struct {
	chord  string
	action string
}{
	{"F1", "show_help"},
	{"ctrl+n", "new"},
	{"ctrl+o", "open"},
	{"alt+s", "save_as"},
	{"ctrl+pagedown", "next_buffer"},
	{"ctrl+pageup", "prev_buffer"},
	{"alt+w", "close_tab"},
	{"ctrl+w", "close_tab"},
	{"ctrl+q", "quit"},
	{"ctrl+s", "save"},
	{"ctrl+z", "undo"},
	{"ctrl+y", "redo"},
	{"ctrl+r", "redo"},
	{"ctrl+x", "cut"},
	{"ctrl+c", "copy"},
	{"ctrl+v", "paste"},
	{"ctrl+a", "select_all"},
	{"ctrl+f", "search"},
	{"F3", "find_next"},
	{"shift+F3", "find_previous"},
	{"ctrl+g", "find_next"},
	{"ctrl+l", "goto_line"},
	{"ctrl+d", "duplicate_line"},
	{"alt+r", "replace"},
	{"ctrl+alt+d", "add_cursor_next_match"},
	{"ctrl+k", "delete_line"},
	{"ctrl+b", "toggle_file_explorer"},
	{"ctrl+e", "focus_file_explorer"},
	{"ctrl+t", "command_palette"},
}

func TestMacosSpecKeysResolve(t *testing.T) {
	m := mustPreset(t, "macos")
	for _, tc := range specKeys {
		b, ok := m.Resolve(tc.chord, keymap.ContextNormal)
		if !ok || b.Action != tc.action {
			t.Errorf("macos %s = %q (%v), want %q", tc.chord, b.Action, ok, tc.action)
		}
	}
}

// TestMacosAltFIsNoLongerAMenuKey: the menu-activation keys are gone from every preset. The
// old test pinned Alt+F as File menu; Alt+F is now fold toggle (inherited from default).
func TestMacosAltFIsNoLongerAMenuKey(t *testing.T) {
	m := mustPreset(t, "macos")
	b, ok := m.Resolve("alt+f", keymap.ContextNormal)
	if !ok || b.Action != "toggle_fold" {
		t.Errorf("macos alt+f = %+v, %v; want toggle_fold", b, ok)
	}
	if _, ok := m.Resolve("alt+f", keymap.ContextGlobal); ok {
		t.Error("macos still binds alt+f globally")
	}
}

func TestMacosPrimaryShortcuts(t *testing.T) {
	m := mustPreset(t, "macos")
	tests := map[string]string{
		"show_help":  "F1",
		"new":        "ctrl+n",
		"open":       "ctrl+o",
		"save":       "ctrl+s",
		"save_as":    "alt+s",
		"close_tab":  "ctrl+w",
		"quit":       "ctrl+q",
		"undo":       "ctrl+z",
		"redo":       "ctrl+r",
		"cut":        "ctrl+x",
		"copy":       "ctrl+c",
		"paste":      "ctrl+v",
		"select_all": "ctrl+a",
		"search":     "ctrl+f",
		// ctrl+g is the macos file's own binding and F3 only inherited: Primary prefers the
		// preset's own idiom (AK-08), where Fresh's menu shows the simplest chord, F3.
		"find_next":            "ctrl+g",
		"goto_line":            "ctrl+l",
		"duplicate_line":       "ctrl+d",
		"delete_line":          "ctrl+k",
		"toggle_file_explorer": "ctrl+b",
		"focus_file_explorer":  "ctrl+e",
		"command_palette":      "ctrl+t",
		"next_buffer":          "ctrl+pagedown",
		"prev_buffer":          "ctrl+pageup",
	}
	for action, want := range tests {
		b, ok := m.Primary(action)
		if !ok || b.Chord != want {
			t.Errorf("macos Primary(%s) = %q (%v), want %q", action, b.Chord, ok, want)
		}
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
