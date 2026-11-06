package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyA and keyB bind ctrl+s to different actions, so a test can tell which
// keymap resolved a key press.
func keyA() fakeBindings {
	return fakeBindings{fakeKeymap: fakeKeymap{
		"normal|s|ctrl": {Action: "save"},
	}}
}

func keyB() fakeBindings {
	return fakeBindings{fakeKeymap: fakeKeymap{
		"normal|s|ctrl": {Action: "copy"},
	}}
}

func TestKeybindingEditorListsActionChordAndContext(t *testing.T) {
	keys := fakeBindings{list: []KeyBinding{
		{Action: "save", Chord: "ctrl+s", Context: "normal"},
		{Action: "quit", Chord: "ctrl+q", Context: "global"},
	}}
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keys, Settings{}, nil)
	if err := m.Run("open_keybinding_editor"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if !frameHas(m, "Keybindings") || !frameHas(m, "save") || !frameHas(m, "ctrl+q  global") {
		t.Fatal("the keybinding editor does not list the bindings with their chord and context")
	}
	press(m, typed("q"))
	if !frameHas(m, "quit") || frameHas(m, "save") {
		t.Fatal("the filter did not narrow the keybinding list to quit")
	}
}

func TestSwitchOverlayListsPresetsAndMarksTheActiveOne(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{Keymap: "emacs"}, nil)
	if err := m.Run("switch_keybinding_map"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		if !frameHas(m, name) {
			t.Errorf("preset %s is not listed", name)
		}
	}
	found := false
	for _, row := range frameText(m) {
		if strings.Contains(row, "emacs") && strings.Contains(row, "current") {
			found = true
		}
	}
	if !found {
		t.Fatal("the active preset emacs is not marked current")
	}
}

func TestSelectKeybindingMapPaletteNameOpensTheSameOverlay(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{}, nil)
	runAction(t, m, "select_keybinding_map")
	if !frameHas(m, "vscode") {
		t.Fatal("select_keybinding_map did not open the preset list")
	}
}

func TestSwitchReplacesTheKeymapForTheNextKeyPress(t *testing.T) {
	var asked []string
	sw := func(name string) (Keymap, error) {
		asked = append(asked, name)
		return keyB(), nil
	}
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{Keymap: "default"}, sw)

	press(m, ctrl('s'))
	if got := strings.Join(ed.actionNames(), ","); got != "save" {
		t.Fatalf("before the switch ctrl+s ran %q, want save", got)
	}

	runAction(t, m, "switch_keybinding_map")
	press(m, special(tea.KeyDown)) // macos
	press(m, special(tea.KeyDown)) // macos-gui
	press(m, special(tea.KeyDown)) // emacs
	press(m, special(tea.KeyEnter))
	if strings.Join(asked, ",") != "emacs" {
		t.Fatalf("switch asked for %v, want [emacs]", asked)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("the switch overlay stayed open after the choice")
	}

	if !frameHas(m, "Keymap: emacs") {
		t.Fatal("the status row does not confirm the new keymap")
	}
	press(m, ctrl('s'))
	if got := strings.Join(ed.actionNames(), ","); got != "save,copy" {
		t.Fatalf("after the switch ctrl+s ran %q, want the new keymap's copy", got)
	}
}

func TestSwitchFailureKeepsTheCurrentKeymap(t *testing.T) {
	sw := func(string) (Keymap, error) { return nil, errSwitch }
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{Keymap: "default"}, sw)
	runAction(t, m, "switch_keybinding_map")
	press(m, special(tea.KeyEnter)) // default
	if !strings.Contains(m.notice, "no such keymap") {
		t.Fatalf("notice = %q, want the switch error", m.notice)
	}
	press(m, ctrl('s'))
	if got := strings.Join(ed.actionNames(), ","); got != "save" {
		t.Fatalf("after a failed switch ctrl+s ran %q, want save", got)
	}
}

func TestSwitchWithoutSwitcherSaysSo(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{}, nil)
	runAction(t, m, "switch_keybinding_map")
	press(m, special(tea.KeyEnter))
	if !strings.Contains(m.notice, "not supported") {
		t.Fatalf("notice = %q, want a not-supported notice", m.notice)
	}
}

func TestKeymapMenuCheckboxFollowsTheActiveKeymap(t *testing.T) {
	sw := func(name string) (Keymap, error) { return keyB(), nil }
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{Keymap: "default"}, sw)
	if !m.Holds("keymap_default") || m.Holds("keymap_emacs") {
		t.Fatal("before the switch the Default row is not the checked one")
	}
	runAction(t, m, "switch_keybinding_map")
	press(m, special(tea.KeyDown))
	press(m, special(tea.KeyDown))
	press(m, special(tea.KeyDown))
	press(m, special(tea.KeyEnter))
	if m.Holds("keymap_default") || !m.Holds("keymap_emacs") {
		t.Fatal("after the switch to emacs the menu still checks Default")
	}
}

func TestKeymapMenuCheckboxWithoutKnownNameShowsDefault(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, keyA(), Settings{}, nil)
	if !m.Holds("keymap_default") {
		t.Fatal("with no keymap name the Default row is not checked")
	}
}

func TestKeymapMenuRowsCheckTheActiveKeymap(t *testing.T) {
	tests := []struct {
		row, active string
		want        bool
	}{
		{"keymap_default", "", true},
		{"keymap_default", "default", true},
		{"keymap_default", "macos", false},
		{"keymap_emacs", "emacs", true},
		{"keymap_vscode", "default", false},
		{"keymap_macos_gui", "macos-gui", true},
		{"keymap_macos_gui", "macos", false},
	}
	for _, tt := range tests {
		t.Run(tt.row+"/"+tt.active, func(t *testing.T) {
			u := utilities{settings: Settings{Keymap: tt.active}}
			if got := u.keymapChecked(tt.row); got != tt.want {
				t.Fatalf("keymapChecked(%s) with %q = %v, want %v", tt.row, tt.active, got, tt.want)
			}
		})
	}
}
