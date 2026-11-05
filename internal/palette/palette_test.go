package palette_test

import (
	"fmt"
	"testing"

	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/palette"
)

func find(t *testing.T, entries []palette.Entry, name string) palette.Entry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no entry named %q", name)
	return palette.Entry{}
}

func TestCommandsFromBuiltinTable(t *testing.T) {
	entries := palette.Commands(nil)
	if len(entries) != 206 {
		t.Fatalf("Commands() = %d entries, want 206", len(entries))
	}
	if entries[0].Name != "Add Cursor Above" || entries[1].Name != "Add Cursor Below" {
		t.Fatalf("first entries = %q, %q; want byte order", entries[0].Name, entries[1].Name)
	}
	for _, e := range entries {
		if e.Source != palette.SourceBuiltin {
			t.Fatalf("%s: source = %q", e.Name, e.Source)
		}
		if e.Action == "" || e.Description == "" {
			t.Fatalf("%s: missing action or description", e.Name)
		}
	}
}

func TestKeybindingColumnFromKeymap(t *testing.T) {
	entries := palette.Commands(keymap.Default())
	tests := map[string]string{
		"Add Cursor Above":         "⌃⌥↑",
		"Add Cursor Below":         "⌃⌥↓",
		"Add Cursor at Next Match": "⌃D",
		"Add Cursors to Line Ends": "⌥⇧I",
	}
	for name, want := range tests {
		if got := find(t, entries, name).Keybinding; got != want {
			t.Errorf("%s keybinding = %q, want %q", name, got, want)
		}
	}
}

func TestKeybindingFollowsKeymap(t *testing.T) {
	// A custom keymap changes the column; an unbound action shows nothing.
	km, err := keymap.Load([]byte(`{"bindings":[
		{"key":"Up","modifiers":["ctrl","alt"],"action":"add_cursor_above","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	entries := palette.Commands(km)
	if got := find(t, entries, "Add Cursor Above").Keybinding; got != "⌃⌥↑" {
		t.Fatalf("custom keymap keybinding = %q", got)
	}
	if got := find(t, entries, "Add Ruler").Keybinding; got != "" {
		t.Fatalf("unbound command keybinding = %q, want empty", got)
	}
}

func TestKeybindingShowsOnlyNormalOrGlobalContext(t *testing.T) {
	// Close Buffer's only default key is "q" in the compositeBuffer context, so
	// the palette shows no key for it (legacy mouse case M18).
	if got := find(t, palette.Commands(keymap.Default()), "Close Buffer").Keybinding; got != "" {
		t.Fatalf("Close Buffer keybinding = %q, want empty", got)
	}
	// Other contexts are skipped even when they come first in the file; normal
	// wins over global whatever the order.
	km, err := keymap.Load([]byte(`{"bindings":[
		{"key":"q","modifiers":[],"action":"close","when":"compositeBuffer"},
		{"key":"x","modifiers":["ctrl"],"action":"close","when":"global"},
		{"key":"w","modifiers":["alt"],"action":"close","when":"normal"},
		{"key":"z","modifiers":[],"action":"undo","when":"prompt"},
		{"key":"y","modifiers":["ctrl"],"action":"redo"},
		{"key":"v","modifiers":["ctrl"],"action":"redo","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	entries := palette.Commands(km)
	if got := find(t, entries, "Close Buffer").Keybinding; got != "⌥W" {
		t.Errorf("Close Buffer keybinding = %q, want normal ⌥W", got)
	}
	if got := find(t, entries, "Redo").Keybinding; got != "⌃V" {
		t.Errorf("Redo keybinding = %q, want normal ⌃V", got)
	}
	if got := find(t, entries, "Undo").Keybinding; got != "" {
		t.Errorf("Undo keybinding = %q, want empty (prompt-only)", got)
	}
}

func TestFormatChord(t *testing.T) {
	tests := map[string]string{
		"ctrl+d":        "⌃D",
		"ctrl+alt+up":   "⌃⌥↑",
		"alt+shift+i":   "⌥⇧I",
		"F10":           "F10",
		"shift+tab":     "⇧⇥",
		"ctrl+pagedown": "⌃⇟",
		"weird+key+x":   "weird+key+x",
	}
	for chord, want := range tests {
		if got := palette.FormatChord(chord); got != want {
			t.Errorf("FormatChord(%q) = %q, want %q", chord, got, want)
		}
	}
}

func TestParseQueryModes(t *testing.T) {
	tests := []struct {
		query string
		mode  palette.Mode
		text  string
	}{
		{"", palette.ModeFile, ""},
		{"main.go", palette.ModeFile, "main.go"},
		{">save", palette.ModeCommand, "save"},
		{":42", palette.ModeLine, "42"},
		{"#notes", palette.ModeBuffer, "notes"},
	}
	for _, tc := range tests {
		mode, text := palette.ParseQuery(tc.query)
		if mode != tc.mode || text != tc.text {
			t.Errorf("ParseQuery(%q) = %v, %q; want %v, %q", tc.query, mode, text, tc.mode, tc.text)
		}
	}
}

func TestSearchOnlyHandlesCommandMode(t *testing.T) {
	entries := palette.Commands(nil)
	if got := palette.Search(entries, ":12"); got != nil {
		t.Fatalf("line mode returned %d entries, want nil", len(got))
	}
	if got := palette.Search(entries, "main.go"); got != nil {
		t.Fatalf("file mode returned %d entries, want nil", len(got))
	}
	// The one command gated on the SESSION_MODE custom context is hidden.
	if got := palette.Search(entries, ">"); len(got) != len(entries)-1 {
		t.Fatalf("empty command text should list every visible command, got %d", len(got))
	}
}

func TestFilterIsCaseInsensitiveSubsequence(t *testing.T) {
	entries := palette.Commands(nil)
	got := palette.Filter(entries, "ACAB")
	if len(got) == 0 || got[0].Name != "Add Cursor Above" {
		t.Fatalf("Filter(ACAB) top = %v", names(got))
	}
	for _, e := range got {
		if !subsequence("acab", e.Name) && !subsequence("acab", e.Description) {
			t.Fatalf("%q matches a-c-a-b neither by name nor description", e.Name)
		}
	}
	if got := palette.Filter(entries, "zzqxq"); len(got) != 0 {
		t.Fatalf("no-match query returned %v", names(got))
	}
}

func TestFilterRanksWordStartsFirst(t *testing.T) {
	entries := []palette.Entry{
		{Name: "xaxb"}, // matches mid-word
		{Name: "a bc"}, // matches at word starts
	}
	got := palette.Filter(entries, "ab")
	if len(got) != 2 || got[0].Name != "a bc" {
		t.Fatalf("ranking = %v; want \"a bc\" first", names(got))
	}
}

func TestFilterBreaksTiesByName(t *testing.T) {
	entries := []palette.Entry{{Name: "b x"}, {Name: "a x"}}
	got := palette.Filter(entries, "x")
	if got[0].Name != "a x" {
		t.Fatalf("equal scores sort by name, got %v", names(got))
	}
}

func TestFilterPutsDisabledLast(t *testing.T) {
	entries := []palette.Entry{{Name: "Save All", Disabled: true}, {Name: "Save As"}}
	got := palette.Filter(entries, "save")
	if got[0].Name != "Save As" {
		t.Fatalf("disabled entries must sort last, got %v", names(got))
	}
}

func subsequence(q, s string) bool {
	qi := 0
	for _, r := range s {
		if qi < len(q) && (r == rune(q[qi]) || r == rune(q[qi]-'a'+'A')) {
			qi++
		}
	}
	return qi == len(q)
}

func names(entries []palette.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func ExampleSearch() {
	entries := palette.Commands(nil)
	for _, e := range palette.Search(entries, ">add cursor below") {
		fmt.Printf("%s %s\n", e.Name, e.Keybinding)
	}
	// Output:
	// Add Cursor Below ⌃⌥↓
}
