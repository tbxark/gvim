package ui

import (
	"strings"
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
			"save":         {Action: "save", Chord: "ctrl+s", Context: "normal"},
			"quit":         {Action: "quit", Chord: "ctrl+q", Context: "global"},
			"find_next":    {Action: "find_next", Chord: "f3", Context: "file_explorer"},
			"show_help":    {Action: "show_help", Chord: "f1", Context: "normal"},
			"save_chord":   {Action: "save_chord", Chord: "ctrl+x ctrl+s", Context: "normal"},
			"popup_cancel": {Action: "popup_cancel", Chord: "escape", Context: "popup"},
		},
	}
}

// TestHelpRowsGroupByContextWithPrimaryShortcuts: the rows are described in words, show the
// shortcut as every other label does, and sit under a readable context heading. The old
// version of this test pinned the raw action ids and chords ("save=ctrl+s"). The popup row is
// left out: gvim resolves no keys in the popup context, so the help does not list one.
func TestHelpRowsGroupByContextWithPrimaryShortcuts(t *testing.T) {
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
				"# Editor",
				"Save=Ctrl+S",
				"Show help=F1",
				"Save chord=Ctrl+X Ctrl+S",
				"# File Explorer",
				"Find next=F3",
				"# Everywhere",
				"Quit=Ctrl+Q",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rows []listRow
			if tt.l != nil {
				rows = helpRows(tt.l, nil)
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
			if !frameHas(m, tt.title) || !frameHas(m, "Save File") || !frameHas(m, "Ctrl+S") || !frameHas(m, "Show Manual") {
				t.Fatalf("frame does not show the title %q and the bindings", tt.title)
			}
			// The footer names only keys that work: Enter does nothing in a read-only help.
			if frameHas(m, "Enter run") || !frameHas(m, "Type to filter, Up/Down move, Esc close") {
				t.Fatalf("the footer of %s is wrong:\n%s", tt.title, strings.Join(frameText(m), "\n"))
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
