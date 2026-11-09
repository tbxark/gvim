package commands_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/keymap"
)

func find(t *testing.T, entries []commands.Entry, name string) commands.Entry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no entry named %q", name)
	return commands.Entry{}
}

func TestCommandsFromBuiltinTable(t *testing.T) {
	entries := commands.Commands(nil)
	// 206 generated rows, then the hand-written rows: the Open Menu Bar and Live Grep rows
	// of keys_commands.go, the Reload Configuration row of settings_commands.go, the
	// Search and Replace in Project, Git Grep and Project Search Results in List rows of
	// search_commands.go, the Edit Theme row of theme_commands.go, the 2 Compare
	// rows of compare_commands.go, the 5 Markdown Contents rows of markdown_commands.go,
	// the Toggle Diagnostics Sidebar and Diagnostics List rows of lsp_commands.go, the
	// 4 Plugin: rows of plugin_commands.go, the Run Agent and Discover Agent Sessions
	// rows of terminal_commands.go and agent_discover_commands.go, and the 7 terminal-tab
	// rows of terminal_commands.go.
	if len(entries) != 242 {
		t.Fatalf("Commands() = %d entries, want 242", len(entries))
	}
	if entries[0].Name != "Add Cursor Above" || entries[1].Name != "Add Cursor Below" {
		t.Fatalf("first entries = %q, %q; want byte order", entries[0].Name, entries[1].Name)
	}
	for _, e := range entries {
		if e.Source != commands.SourceBuiltin {
			t.Fatalf("%s: source = %q", e.Name, e.Source)
		}
		if e.Action == "" || e.Description == "" {
			t.Fatalf("%s: missing action or description", e.Name)
		}
	}
}

func TestKeybindingColumnFromKeymap(t *testing.T) {
	def, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	entries := commands.Commands(def)
	tests := map[string]string{
		"Add Cursor Above": "Ctrl+Alt+Up",
		"Add Cursor Below": "Ctrl+Alt+Down",
		// Fresh's default keymap has no Duplicate Line key; gvim keeps one on Ctrl+Shift+D
		// (Fresh uses Alt+Shift+Down for block selection).
		"Duplicate Line": "Ctrl+Shift+D",
	}
	for name, want := range tests {
		if got := find(t, entries, name).Keybinding; got != want {
			t.Errorf("%s keybinding = %q, want %q", name, got, want)
		}
	}
}

// A chord a preset gives to another action is not the old action's shortcut, so the
// palette shows none for it: the default preset binds Ctrl+R to Replace, and Fresh's macOS
// preset makes Ctrl+R Redo there. Add Cursor at Next Match is Ctrl+D in both.
func TestKeybindingIgnoresShadowedBinding(t *testing.T) {
	def, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, commands.Commands(def), "Replace").Keybinding; got != "Ctrl+R" {
		t.Errorf("default Replace keybinding = %q, want Ctrl+R", got)
	}
	mac, err := keymap.ByName("macos")
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, commands.Commands(mac), "Replace").Keybinding; got != "" {
		t.Errorf("macos Replace keybinding = %q, want none: Ctrl+R is Redo there", got)
	}
	if got := find(t, commands.Commands(mac), "Add Cursor at Next Match").Keybinding; got != "Ctrl+D" {
		t.Errorf("macos Add Cursor at Next Match keybinding = %q, want Ctrl+D", got)
	}
}

func TestKeybindingFollowsKeymap(t *testing.T) {
	// A custom keymap changes the column; an unbound action shows nothing.
	km, err := keymap.Load([]byte(`{"bindings":[
		{"key":"Up","modifiers":["ctrl","alt"],"action":"add_cursor_above","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	entries := commands.Commands(km)
	if got := find(t, entries, "Add Cursor Above").Keybinding; got != "Ctrl+Alt+Up" {
		t.Fatalf("custom keymap keybinding = %q", got)
	}
	if got := find(t, entries, "Add Ruler").Keybinding; got != "" {
		t.Fatalf("unbound command keybinding = %q, want empty", got)
	}
}

// A chord that a user override takes away from an action is not its shortcut,
// so the palette must not show it. The preset binds Save File to ctrl+s.
func TestKeybindingHidesChordTakenByOverride(t *testing.T) {
	base, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		override string
	}{
		{"global noop", `{"key":"s","modifiers":["ctrl"],"action":"noop"}`},
		{"global non-noop", `{"key":"s","modifiers":["ctrl"],"action":"quick_open"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km, err := base.WithOverrides([]byte(`{"bindings":[` + tt.override + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			if got := find(t, commands.Commands(km), "Save File").Keybinding; got != "" {
				t.Fatalf("Save File keybinding = %q, want empty: ctrl+s no longer saves", got)
			}
		})
	}
}

func TestKeybindingShowsOnlyNormalOrGlobalContext(t *testing.T) {
	// Close Buffer's own keys are in the compositeBuffer context ("q"), which a palette does
	// not show. It shows the key that closes the tab instead, which the keymaps bind as
	// close_tab (AK-08; it used to show none, legacy mouse case M18).
	def, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, commands.Commands(def), "Close Buffer").Keybinding; got != "Alt+W" {
		t.Fatalf("Close Buffer keybinding = %q, want Alt+W", got)
	}
	only, err := keymap.Load([]byte(`{"bindings":[{"key":"q","modifiers":[],"action":"close","when":"compositeBuffer"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, commands.Commands(only), "Close Buffer").Keybinding; got != "" {
		t.Fatalf("Close Buffer keybinding = %q, want empty for a key of another context", got)
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
	entries := commands.Commands(km)
	if got := find(t, entries, "Close Buffer").Keybinding; got != "Alt+W" {
		t.Errorf("Close Buffer keybinding = %q, want normal Alt+W", got)
	}
	if got := find(t, entries, "Redo").Keybinding; got != "Ctrl+V" {
		t.Errorf("Redo keybinding = %q, want normal Ctrl+V", got)
	}
	if got := find(t, entries, "Undo").Keybinding; got != "" {
		t.Errorf("Undo keybinding = %q, want empty (prompt-only)", got)
	}
}

func TestFormatChord(t *testing.T) {
	tests := map[string]string{
		"ctrl+d":        "Ctrl+D",
		"ctrl+alt+up":   "Ctrl+Alt+Up",
		"alt+shift+i":   "Alt+Shift+I",
		"F10":           "F10",
		"shift+tab":     "Shift+Tab",
		"ctrl+pagedown": "Ctrl+PgDn",
		"ctrl+x ctrl+s": "Ctrl+X Ctrl+S",
		"weird+key+x":   "weird+key+x",
	}
	for chord, want := range tests {
		if got := commands.FormatChord(chord); got != want {
			t.Errorf("FormatChord(%q) = %q, want %q", chord, got, want)
		}
	}
}

func TestParseQueryModes(t *testing.T) {
	tests := []struct {
		query string
		mode  commands.Mode
		text  string
	}{
		{"", commands.ModeFile, ""},
		{"main.go", commands.ModeFile, "main.go"},
		{">save", commands.ModeCommand, "save"},
		{":42", commands.ModeLine, "42"},
		{"#notes", commands.ModeBuffer, "notes"},
	}
	for _, tc := range tests {
		mode, text := commands.ParseQuery(tc.query)
		if mode != tc.mode || text != tc.text {
			t.Errorf("ParseQuery(%q) = %v, %q; want %v, %q", tc.query, mode, text, tc.mode, tc.text)
		}
	}
}

func TestSearchOnlyHandlesCommandMode(t *testing.T) {
	entries := commands.Commands(nil)
	if got := commands.Search(entries, ":12"); got != nil {
		t.Fatalf("line mode returned %d entries, want nil", len(got))
	}
	if got := commands.Search(entries, "main.go"); got != nil {
		t.Fatalf("file mode returned %d entries, want nil", len(got))
	}
	// The one command gated on the SESSION_MODE custom context is hidden.
	if got := commands.Search(entries, ">"); len(got) != len(entries)-1 {
		t.Fatalf("empty command text should list every visible command, got %d", len(got))
	}
}

func TestFilterIsCaseInsensitiveSubsequence(t *testing.T) {
	entries := commands.Commands(nil)
	got := commands.Filter(entries, "ACAB")
	if len(got) == 0 || got[0].Name != "Add Cursor Above" {
		t.Fatalf("Filter(ACAB) top = %v", names(got))
	}
	for _, e := range got {
		if !subsequence("acab", e.Name) && !subsequence("acab", e.Description) {
			t.Fatalf("%q matches a-c-a-b neither by name nor description", e.Name)
		}
	}
	if got := commands.Filter(entries, "zzqxq"); len(got) != 0 {
		t.Fatalf("no-match query returned %v", names(got))
	}
}

func TestFilterRanksWordStartsFirst(t *testing.T) {
	entries := []commands.Entry{
		{Name: "xaxb"}, // matches mid-word
		{Name: "a bc"}, // matches at word starts
	}
	got := commands.Filter(entries, "ab")
	if len(got) != 2 || got[0].Name != "a bc" {
		t.Fatalf("ranking = %v; want \"a bc\" first", names(got))
	}
}

func TestFilterBreaksTiesByName(t *testing.T) {
	entries := []commands.Entry{{Name: "b x"}, {Name: "a x"}}
	got := commands.Filter(entries, "x")
	if got[0].Name != "a x" {
		t.Fatalf("equal scores sort by name, got %v", names(got))
	}
}

func TestFilterPutsDisabledLast(t *testing.T) {
	entries := []commands.Entry{{Name: "Save All", Disabled: true}, {Name: "Save As"}}
	got := commands.Filter(entries, "save")
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

func names(entries []commands.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func ExampleSearch() {
	entries := commands.Commands(nil)
	for _, e := range commands.Search(entries, ">add cursor below") {
		fmt.Printf("%s %s\n", e.Name, e.Keybinding)
	}
	// Output:
	// Add Cursor Below Ctrl+Alt+Down
}

// The palette runs in the normal context, so a global chord that a normal binding
// of another action takes is not the shortcut of the global action.
func TestKeybindingIgnoresGlobalShadowedInNormal(t *testing.T) {
	km, err := keymap.Load([]byte(`{"bindings":[
		{"key":"s","modifiers":["ctrl"],"action":"save","when":"global"},
		{"key":"s","modifiers":["ctrl"],"action":"quick_open","when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, commands.Commands(km), "Save File").Keybinding; got != "" {
		t.Fatalf("Save File keybinding = %q, want empty: ctrl+s runs Quick Open in normal", got)
	}
}

// A multi-key sequence is a shortcut like any other: the column shows it when the action has
// no single chord, and a single chord wins when it has both.
func TestKeybindingShowsSequenceBinding(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "only a sequence",
			src: `{"bindings":[
				{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"save","when":"normal"}]}`,
			want: "Ctrl+X Ctrl+S",
		},
		{
			name: "sequence beside a global single chord",
			src: `{"bindings":[
				{"keys":[{"key":"x","modifiers":["ctrl"]},{"key":"s","modifiers":["ctrl"]}],"action":"save","when":"normal"},
				{"key":"s","modifiers":["ctrl"],"action":"save","when":"global"}]}`,
			want: "Ctrl+S",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km, err := keymap.Load([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if got := find(t, commands.Commands(km), "Save File").Keybinding; got != tt.want {
				t.Fatalf("Save File keybinding = %q, want %q", got, tt.want)
			}
		})
	}
}

// PC54: the macos-gui rows read "super+d" next to rows with glyphs. Every row is now one style, and
// the Super key is named Cmd on macOS.
func TestKeybindingColumnOfTheGUIKeymapIsOneStyle(t *testing.T) {
	gui, err := keymap.ByName("macos-gui")
	if err != nil {
		t.Fatal(err)
	}
	entries := commands.Commands(gui)
	if got, want := find(t, entries, "Add Cursor at Next Match").Keybinding, keymap.SuperName+"+D"; got != want {
		t.Errorf("Add Cursor at Next Match = %q, want %q", got, want)
	}
	if got := find(t, entries, "Query Replace").Keybinding; got != "" && got != keymap.SuperName+"+H" {
		t.Errorf("Query Replace = %q", got)
	}
	for _, e := range entries {
		for _, raw := range []string{"super+", "ctrl+", "alt+", "shift+", "⌃", "⌥", "⇧"} {
			if strings.Contains(e.Keybinding, raw) {
				t.Errorf("%s shows %q: not the plain label style", e.Name, e.Keybinding)
			}
		}
	}
}

// There is no project-wide Search and Replace, so the palette says that Live Grep only
// searches, and that Replace works on one buffer.
func TestSearchAndReplaceScopeIsSaidInThePalette(t *testing.T) {
	entries := commands.Commands(nil)
	for name, want := range map[string]string{
		"Live Grep": "search only",
		"Replace":   "current buffer",
	} {
		if got := find(t, entries, name).Description; !strings.Contains(got, want) {
			t.Errorf("%s: description %q does not say %q", name, got, want)
		}
	}
}

func TestSearchCommandRows(t *testing.T) {
	entries := commands.Commands(nil)
	for _, want := range []struct {
		name, action string
	}{
		{"Search and Replace in Project", "start_search_replace"},
		{"Git Grep", "start_git_grep"},
		{"Project Search Results in List", "project_search_results"},
	} {
		got := find(t, entries, want.name)
		if got.Action != want.action {
			t.Errorf("%s: action = %q, want %q", want.name, got.Action, want.action)
		}
	}
}
