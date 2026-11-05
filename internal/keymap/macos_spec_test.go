package keymap_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/keymap"
)

// specKeys are the macOS chords of migration/PHASE1-EDITOR.md section 4 and
// the action each must resolve to in the normal context.
var specKeys = []struct {
	chord  string
	action string
}{
	{"F10", "menu_activate"},
	{"F1", "show_help"},
	{"ctrl+n", "new"},
	{"ctrl+o", "open"},
	{"alt+s", "save_as"},
	{"alt+o", "open_folder"},
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
	{"ctrl+k", "delete_line"},
	{"ctrl+b", "toggle_file_explorer"},
	{"ctrl+e", "focus_file_explorer"},
	{"ctrl+t", "command_palette"},
	{"alt+f", "menu_open"},
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

// TestMacosAltFOpensFileMenu checks that Alt+F is no longer move_word_right in
// the macOS preset and opens the File menu.
func TestMacosAltFOpensFileMenu(t *testing.T) {
	m := mustPreset(t, "macos")
	for _, ctx := range []keymap.Context{keymap.ContextNormal, keymap.ContextGlobal} {
		b, ok := m.Resolve("alt+f", ctx)
		if !ok || b.Action != "menu_open" || b.Args["name"] != "File" {
			t.Errorf("macos alt+f in %s = %+v, %v; want menu_open name=File", ctx, b, ok)
		}
	}
	for _, b := range m.BindingsFor("move_word_right") {
		if b.Chord == "alt+f" {
			t.Errorf("macos still binds alt+f to move_word_right in %s", b.When)
		}
	}
}

func TestMacosPrimaryShortcuts(t *testing.T) {
	m := mustPreset(t, "macos")
	tests := map[string]string{
		"menu_activate":        "F10",
		"show_help":            "F1",
		"new":                  "ctrl+n",
		"open":                 "ctrl+o",
		"save":                 "ctrl+s",
		"save_as":              "alt+s",
		"open_folder":          "alt+o",
		"close_tab":            "ctrl+w",
		"quit":                 "ctrl+q",
		"undo":                 "ctrl+z",
		"redo":                 "ctrl+r",
		"cut":                  "ctrl+x",
		"copy":                 "ctrl+c",
		"paste":                "ctrl+v",
		"select_all":           "ctrl+a",
		"search":               "ctrl+f",
		"find_next":            "F3",
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

func TestAcceleratorsSpecMenus(t *testing.T) {
	m := mustPreset(t, "macos")
	titles := m.Accelerators([]keymap.MenuEntry{
		{Label: "File", Letter: 'F'},
		{Label: "Edit", Letter: 'E'},
		{Label: "View", Letter: 'V'},
		{Label: "Help", Letter: 'H'},
	})
	for i, want := range []rune{'F', 'E', 'V', 'H'} {
		if titles[i].Letter != want || titles[i].Index != 0 || titles[i].Bound {
			t.Errorf("title %q = %+v, want letter %q at 0 with no shortcut", titles[i].Label, titles[i], want)
		}
	}

	file := m.Accelerators([]keymap.MenuEntry{
		{Label: "New File", Action: "new", Letter: 'N'},
		{Label: "Open File...", Action: "open", Letter: 'O'},
		{Label: "Open Folder...", Action: "open_folder", Letter: 'F'},
		{Label: "Save", Action: "save", Letter: 'S'},
		{Label: "Save As...", Action: "save_as", Letter: 'A'},
		{Label: "Close Tab", Action: "close_tab", Letter: 'W'},
		{Label: "Quit", Action: "quit", Letter: 'Q'},
	})
	wantFile := []struct {
		letter rune
		index  int
		chord  string
	}{
		{'N', 0, "ctrl+n"}, {'O', 0, "ctrl+o"}, {'F', 5, "alt+o"}, {'S', 0, "ctrl+s"},
		{'A', 5, "alt+s"}, {'C', 0, "ctrl+w"}, {'Q', 0, "ctrl+q"},
	}
	checkAccelerators(t, file, wantFile)

	edit := m.Accelerators([]keymap.MenuEntry{
		{Label: "Undo", Action: "undo", Letter: 'U'},
		{Label: "Redo", Action: "redo", Letter: 'R'},
		{Label: "Cut", Action: "cut", Letter: 'X'},
		{Label: "Copy", Action: "copy", Letter: 'C'},
		{Label: "Paste", Action: "paste", Letter: 'P'},
		{Label: "Select All", Action: "select_all", Letter: 'S'},
		{Label: "Find...", Action: "search", Letter: 'F'},
		{Label: "Find Next", Action: "find_next", Letter: 'N'},
		{Label: "Go to Line...", Action: "goto_line", Letter: 'G'},
		{Label: "Duplicate", Action: "duplicate_line", Letter: 'D'},
		{Label: "Cut Line", Action: "delete_line", Letter: 'L'},
	})
	wantEdit := []struct {
		letter rune
		index  int
		chord  string
	}{
		{'U', 0, "ctrl+z"}, {'R', 0, "ctrl+r"}, {'t', 2, "ctrl+x"}, {'C', 0, "ctrl+c"},
		{'P', 0, "ctrl+v"}, {'S', 0, "ctrl+a"}, {'F', 0, "ctrl+f"}, {'N', 5, "F3"},
		{'G', 0, "ctrl+l"}, {'D', 0, "ctrl+d"}, {'L', 4, "ctrl+k"},
	}
	checkAccelerators(t, edit, wantEdit)

	view := m.Accelerators([]keymap.MenuEntry{
		{Label: "File Explorer", Action: "toggle_file_explorer", Letter: 'E'},
		{Label: "Focus Explorer", Action: "focus_file_explorer", Letter: 'F'},
		{Label: "Next Tab", Action: "next_buffer", Letter: 'N'},
		{Label: "Previous Tab", Action: "prev_buffer", Letter: 'V'},
		{Label: "Command Palette...", Action: "command_palette", Letter: 'C'},
	})
	wantView := []struct {
		letter rune
		index  int
		chord  string
	}{
		{'E', 5, "ctrl+b"}, {'F', 0, "ctrl+e"}, {'N', 0, "ctrl+pagedown"},
		{'v', 3, "ctrl+pageup"}, {'C', 0, "ctrl+t"},
	}
	checkAccelerators(t, view, wantView)
}

func checkAccelerators(t *testing.T, got []keymap.Accelerator, want []struct {
	letter rune
	index  int
	chord  string
}) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d accelerators, want %d", len(got), len(want))
	}
	seen := map[string]bool{}
	for i, w := range want {
		a := got[i]
		if a.Letter != w.letter || a.Index != w.index {
			t.Errorf("%q letter = %q at %d, want %q at %d", a.Label, a.Letter, a.Index, w.letter, w.index)
		}
		if a.Index >= 0 && []rune(a.Label)[a.Index] != a.Letter {
			t.Errorf("%q: Index %d does not point at Letter %q", a.Label, a.Index, a.Letter)
		}
		if !a.Bound || a.Binding.Chord != w.chord {
			t.Errorf("%q shortcut = %q (%v), want %q", a.Label, a.Binding.Chord, a.Bound, w.chord)
		}
		lower := strings.ToLower(string(a.Letter))
		if seen[lower] {
			t.Errorf("%q: letter %q repeats within the menu", a.Label, a.Letter)
		}
		seen[lower] = true
	}
}

func TestAcceleratorsFallback(t *testing.T) {
	m := mustPreset(t, "macos")
	got := m.Accelerators([]keymap.MenuEntry{
		{Label: "Save", Action: "save", Letter: 'S'},
		// S is taken by Save, so the fallback is the first free letter, "a".
		{Label: "Save As", Action: "save_as", Letter: 'S'},
		{Label: "Zoom", Action: "no_such_action"},
	})
	if got[1].Letter != 'a' || got[1].Index != 1 {
		t.Errorf("Save As = %q at %d, want a at 1", got[1].Letter, got[1].Index)
	}
	if got[2].Letter != 'Z' || got[2].Index != 0 {
		t.Errorf("Zoom = %q at %d, want Z at 0", got[2].Letter, got[2].Index)
	}
	if got[2].Bound {
		t.Errorf("unknown action must have no shortcut, got %q", got[2].Binding.Chord)
	}
	if got[0].Binding.Chord != "ctrl+s" {
		t.Errorf("Save shortcut = %q, want ctrl+s", got[0].Binding.Chord)
	}
}
