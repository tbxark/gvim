package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// helpKeys is a keymap with a few bindings in three contexts, one action bound
// twice (Primary picks the simpler chord), a noop and a sequence-only action.
func helpKeys() fakeBindings {
	return fakeBindings{
		fakeKeymap: fakeKeymap{},
		list: []KeyBinding{
			{Action: "popup_cancel", Chord: "escape", Context: "popup"},
			{Action: "save", Chord: "ctrl+shift+s", Context: "normal"},
			{Action: "save", Chord: "ctrl+s", Context: "normal"},
			{Action: "quit", Chord: "ctrl+q", Context: "global"},
			{Action: "noop", Chord: "alt+0", Context: "normal"},
			{Action: "show_help", Chord: "f1", Context: "normal"},
			{Action: "save_chord", Chord: "ctrl+x ctrl+s", Context: "normal"},
			{Action: "find_next", Chord: "f3", Context: "file_explorer"},
		},
		primary: map[string]KeyBinding{
			"save":      {Action: "save", Chord: "ctrl+s", Context: "normal"},
			"quit":      {Action: "quit", Chord: "ctrl+q", Context: "global"},
			"find_next": {Action: "find_next", Chord: "f3", Context: "file_explorer"},
		},
	}
}

func TestHelpRowsGroupByContextWithPrimaryChords(t *testing.T) {
	tests := []struct {
		name string
		l    BindingLister
		want []string
	}{
		{
			name: "no lister gives no rows",
			l:    nil,
			want: nil,
		},
		{
			name: "groups follow context order, then other contexts; noop is left out",
			l:    helpKeys(),
			want: []string{
				"# normal",
				"save=ctrl+s",
				"show_help=f1",
				"save_chord=ctrl+x ctrl+s",
				"# file_explorer",
				"find_next=f3",
				"# global",
				"quit=ctrl+q",
				"# popup",
				"popup_cancel=escape",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rows []listRow
			if tt.l != nil {
				rows = helpRows(tt.l)
			}
			sameLines(t, "rows", rowsOf(rows), tt.want)
		})
	}
}

func TestKeybindingRowsListOnlyBindingsInEffect(t *testing.T) {
	l := fakeBindings{list: []KeyBinding{
		{Action: "copy", Chord: "ctrl+c", Context: "normal"},
		{Action: "save", Chord: "ctrl+s", Context: "normal"},
		{Action: "quit", Chord: "ctrl+c", Context: "normal"}, // replaces copy for this chord
		{Action: "quit", Chord: "ctrl+q", Context: "global"},
	}}
	tests := []struct {
		name string
		l    BindingLister
		want []string
	}{
		{"nil lister", nil, nil},
		{
			name: "a later binding of the same chord and context replaces the earlier one",
			l:    l,
			want: []string{"save=ctrl+s  normal", "quit=ctrl+c  normal", "quit=ctrl+q  global"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rows []listRow
			if tt.l != nil {
				rows = keybindingRows(tt.l)
			}
			sameLines(t, "rows", rowsOf(rows), tt.want)
		})
	}
}

func TestShowHelpOpensOverlayWithTheKeymapBindings(t *testing.T) {
	tests := []struct {
		action string
		title  string
	}{
		{"show_help", "Help"},
		{"keyboard_shortcuts", "Keyboard Shortcuts"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			ed := newFakeEditor("hello")
			m := newUtilityModel(t, ed, helpKeys(), Settings{}, nil)
			if err := m.Run(tt.action); err != nil {
				t.Fatalf("Run(%s) = %v, want nil", tt.action, err)
			}
			if len(m.overlays) != 1 {
				t.Fatalf("overlays = %d, want 1", len(m.overlays))
			}
			if !frameHas(m, tt.title) || !frameHas(m, "ctrl+s") || !frameHas(m, "show_help") {
				t.Fatalf("frame does not show the title %q and the bindings", tt.title)
			}
		})
	}
}

func TestShowHelpWithoutBindingListerSaysSo(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "show_help")
	if !frameHas(m, "No keymap bindings are available") {
		t.Fatal("the help does not say that the keymap has no bindings to list")
	}
}

func TestHelpEscClosesAndTypingFilters(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, helpKeys(), Settings{}, nil)
	runAction(t, m, "show_help")
	press(m, typed("f"))
	if !frameHas(m, "Filter: f") {
		t.Fatal("typed text did not reach the help filter")
	}
	if len(ed.actions) != 0 {
		t.Fatalf("typed text reached the editor: %v", ed.actions)
	}

	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("Esc left %d overlays open", len(m.overlays))
	}
}

func TestHelpSwallowsEditorKeys(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, helpKeys(), Settings{}, nil)
	runAction(t, m, "show_help")
	press(m, ctrl('s'))
	if len(ed.actions) != 0 {
		t.Fatalf("a key under the help ran editor actions: %v", ed.actions)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the help still open", len(m.overlays))
	}
}

func TestHelpEnterDoesNotRunEditorAction(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, helpKeys(), Settings{}, nil)
	runAction(t, m, "show_help")
	press(m, special(tea.KeyEnter))
	if len(ed.actions) != 0 {
		t.Fatalf("Enter in the help ran %v", ed.actions)
	}
}

func TestEveryUtilityOverlayClosesOnEsc(t *testing.T) {
	actions := []string{
		"show_help", "keyboard_shortcuts", "open_keybinding_editor", "switch_keybinding_map",
		"select_keybinding_map", "open_settings", "tour_next", "show_remote_indicator_menu",
		"calibrate_input", "event_debug",
	}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			ed := newFakeEditor("hello")
			m := newUtilityModel(t, ed, helpKeys(), testSettings(), nil)
			runAction(t, m, action)
			if len(m.overlays) != 1 {
				t.Fatalf("overlays = %d after %s, want 1", len(m.overlays), action)
			}
			press(m, special(tea.KeyEscape))
			if len(m.overlays) != 0 {
				t.Fatalf("Esc left %d overlays open", len(m.overlays))
			}
		})
	}
}
