package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/theme"
)

// hintKeys is a keymap that lists the bindings of chords: action to chord, all in the
// normal context.
func hintKeys(chords map[string]string) fakeBindings {
	primary := map[string]KeyBinding{}
	for action, chord := range chords {
		primary[action] = KeyBinding{Action: action, Chord: chord, Context: "normal"}
	}
	return fakeBindings{fakeKeymap: testKeys(), primary: primary}
}

func hintModel(t *testing.T, keys Keymap, sw func(string) (Keymap, error)) *Model {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock, SwitchKeymap: sw,
		Commands: palmodel.Commands(nil)})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestMenuShortcutsComeFromTheActiveKeymap(t *testing.T) {
	m := hintModel(t, hintKeys(map[string]string{"redo": "ctrl+y", "select_all": "ctrl+a", "goto_line": "ctrl+g", "quick_open": "ctrl+p"}), nil)
	want := map[string]map[string]string{
		"Edit": {"Redo": "Ctrl+Y", "Select All": "Ctrl+A", "Delete Line": "", "Undo": ""},
		"Go":   {"Go to Line...": "Ctrl+G", "Command Palette...": "Ctrl+P"},
	}
	for title, rows := range want {
		menu := m.menu
		for i, l := range []string{"File", "Edit", "View", "Selection", "Go"} {
			if l == title {
				menu = menu.Open(i)
			}
		}
		for _, r := range menu.Rows() {
			if keys, ok := rows[r.Label]; ok && r.Keys != keys {
				t.Errorf("%s > %s shows %q, want %q", title, r.Label, r.Keys, keys)
			}
		}
	}
}

// TestMenuShowsNoShortcutWhenTheKeymapCannotBeAsked: the menu's shortcuts come from the active
// keymap only. The table's keys are Fresh's macOS labels, so with no keymap to ask a row shows
// none, rather than a key that the active keymap may not bind.
func TestMenuShowsNoShortcutWhenTheKeymapCannotBeAsked(t *testing.T) {
	m := hintModel(t, testKeys(), nil)
	for _, r := range m.menu.Open(1).Rows() {
		if r.Label == "Redo" && r.Keys != "" {
			t.Errorf("Redo shows %q, want no shortcut without a keymap", r.Keys)
		}
	}
}

func TestPaletteHintInTheStatusRowFollowsTheKeymap(t *testing.T) {
	tests := []struct {
		name string
		keys Keymap
		want string
	}{
		{"no keymap listing", testKeys(), "Palette: Ctrl+P"},
		{"quick_open", hintKeys(map[string]string{"quick_open": "alt+x"}), "Palette: Alt+X"},
		{"command_palette", hintKeys(map[string]string{"command_palette": "ctrl+shift+p"}), "Palette: Ctrl+Shift+P"},
		{"a sequence", hintKeys(map[string]string{"quick_open": "ctrl+x ctrl+q"}), "Palette: Ctrl+X Ctrl+Q"},
		{"unbound", hintKeys(nil), "Palette"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := hintModel(t, tc.keys, nil)
			rows := frameText(m)
			if got := rows[len(rows)-1]; !strings.Contains(got, tc.want+" ") && !strings.HasSuffix(strings.TrimRight(got, " "), tc.want) {
				t.Errorf("status row %q lacks %q", got, tc.want)
			}
			if tc.want != "Palette: Ctrl+P" && strings.Contains(rows[len(rows)-1], "Ctrl+P") {
				t.Errorf("status row %q still shows the fallback hint", rows[len(rows)-1])
			}
		})
	}
}

func TestWelcomeShortcutsComeFromTheActiveKeymap(t *testing.T) {
	keys := hintKeys(map[string]string{"new": "ctrl+n", "open": "ctrl+o", "quick_open": "ctrl+p", "focus_file_explorer": "ctrl+e"})
	tf := newTabFake()
	m := New(Deps{Editor: tf, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := m.welcomeDoorKeys()
	want := []string{"Ctrl+N", "Ctrl+O", "", "Ctrl+P", "Ctrl+E"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("door keys = %q, want %q", got, want)
	}
	all := strings.Join(m.paint().Plain(), "\n")
	for _, bad := range []string{"Ctrl+T", "Alt+O", "Ctrl+B"} {
		if strings.Contains(all, bad) {
			t.Errorf("welcome screen still shows %q:\n%s", bad, all)
		}
	}
	if !strings.Contains(all, "Ctrl+P") {
		t.Errorf("welcome screen lacks the palette key:\n%s", all)
	}
	// Without a keymap that lists its bindings the doors keep the table's keys.
	plain, _ := welcomeModel(t, 80, 24)
	if keys := plain.welcomeDoorKeys(); keys != nil {
		t.Errorf("door keys = %q, want nil", keys)
	}
}

func TestSwitchingTheKeymapRelabelsThePalette(t *testing.T) {
	emacs := hintKeys(map[string]string{"search": "ctrl+s", "redo": "alt+_"})
	m := hintModel(t, hintKeys(map[string]string{"search": "ctrl+f"}), func(name string) (Keymap, error) {
		if name != "emacs" {
			t.Errorf("switched to %q", name)
		}
		return emacs, nil
	})
	label := func(action string) string {
		for _, e := range m.allCommands() {
			if e.Action == action {
				return e.Keybinding
			}
		}
		t.Fatalf("no command for %s", action)
		return ""
	}
	if err := m.Run("switch_keybinding_map:emacs"); err != nil {
		t.Fatal(err)
	}
	if got := label("search"); got != "Ctrl+S" {
		t.Errorf("Search shows %q after the switch, want Ctrl+S", got)
	}
	if got := label("redo"); got != "Alt+_" {
		t.Errorf("Redo shows %q after the switch, want Alt+_", got)
	}
	if got := label("toggle_comment"); got != "" {
		t.Errorf("Toggle Comment shows %q after the switch, want no shortcut", got)
	}
}

// The palette lists no command that can only say "Not available in gvim" (PC54: Update fresh).
func TestPaletteHidesTheUpdaterRows(t *testing.T) {
	m := hintModel(t, testKeys(), nil)
	for _, e := range m.allCommands() {
		if e.Action == "update_fresh" || e.Action == "open_update_log" {
			t.Errorf("the palette lists %q", e.Name)
		}
	}
	if err := m.Run("update_fresh"); err != nil || !strings.Contains(m.notice, "does not update itself") {
		t.Errorf("the action itself must still answer: %v, notice %q", err, m.notice)
	}
}

// After a keymap switch the menu rows, the palette and the status row all follow the new
// keymap, in the one label style.
func TestSwitchingTheKeymapRelabelsTheMenusAndTheStatusRow(t *testing.T) {
	before := hintKeys(map[string]string{"save": "ctrl+s", "quick_open": "ctrl+p"})
	after := hintKeys(map[string]string{"save": "ctrl+x ctrl+s", "quick_open": "alt+x"})
	m := hintModel(t, before, func(string) (Keymap, error) { return after, nil })
	menuKey := func(label string) string {
		for _, r := range m.menu.Open(0).Rows() {
			if r.Label == label {
				return r.Keys
			}
		}
		t.Fatalf("no menu row %q", label)
		return ""
	}
	if got := menuKey("Save"); got != "Ctrl+S" {
		t.Fatalf("Save shows %q before the switch", got)
	}
	runAction(t, m, "switch_keybinding_map:emacs")
	if got := menuKey("Save"); got != "Ctrl+X Ctrl+S" {
		t.Errorf("Save shows %q after the switch, want the sequence of the new keymap", got)
	}
	if got := m.paletteHint(); got != "Palette: Alt+X" {
		t.Errorf("palette hint = %q after the switch", got)
	}
}
