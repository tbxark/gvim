package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCalibrateShowsTheLastKeyAsTheTerminalReportedIt(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
		want []string // the text each row must contain
	}{
		{
			name: "ctrl letter",
			key:  tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl},
			want: []string{"Key: a", "Modifiers: ctrl", "Text: (none)"},
		},
		{
			name: "typed letter",
			key:  tea.KeyPressMsg{Code: 'x', Text: "x"},
			want: []string{"Key: x", "Modifiers: none", `Text: "x"`},
		},
		{
			name: "alt shift function key",
			key:  tea.KeyPressMsg{Code: tea.KeyF1, Mod: tea.ModAlt | tea.ModShift},
			want: []string{"Key: f1", "Modifiers: alt+shift", "Text: (none)"},
		},
		{
			name: "enter",
			key:  special(tea.KeyEnter),
			want: []string{"Key: enter", "Modifiers: none"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
			runAction(t, m, "calibrate_input")
			press(m, tt.key)
			for _, w := range tt.want {
				if !frameHas(m, w) {
					t.Errorf("calibration does not show %q", w)
				}
			}
			if len(ed.actions) != 0 {
				t.Errorf("the calibration let the key reach the editor: %v", ed.actions)
			}
		})
	}
}

func TestCalibratePromptsUntilAKeyArrivesAndEscCloses(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "calibrate_input")
	if !frameHas(m, "Press a key") {
		t.Fatal("calibration does not prompt for a key")
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("Esc left %d overlays open", len(m.overlays))
	}
}

func TestEventRingKeepsTheLastTwentyNewestFirst(t *testing.T) {
	var r eventRing
	for i := 1; i <= 25; i++ {
		r.add(fmt.Sprintf("e%d", i))
	}
	got := r.newest()
	if len(got) != eventRingSize || eventRingSize != 20 {
		t.Fatalf("ring holds %d events, want 20", len(got))
	}
	if got[0] != "e25" || got[19] != "e6" {
		t.Fatalf("newest = %q ... %q, want e25 ... e6", got[0], got[19])
	}
}

func TestEventRingRecordsKeysAndButtonsButNotMotion(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want bool
	}{
		{"key press", tea.KeyPressMsg{Code: 'a', Text: "a"}, true},
		{"mouse click", tea.MouseClickMsg(tea.Mouse{X: 1, Y: 2, Button: tea.MouseLeft}), true},
		{"mouse release", tea.MouseReleaseMsg(tea.Mouse{X: 1, Y: 2, Button: tea.MouseLeft}), true},
		{"wheel", tea.MouseWheelMsg(tea.Mouse{X: 1, Y: 2, Button: tea.MouseWheelUp}), true},
		{"motion is too frequent to keep", tea.MouseMotionMsg(tea.Mouse{X: 1, Y: 2}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
			m.Update(tt.msg)
			if got := len(m.util.events.newest()) == 1; got != tt.want {
				t.Fatalf("recorded = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventDebugShowsEventsFromBeforeItOpened(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	press(m, typed("a"))
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: 5, Button: tea.MouseLeft}))
	runAction(t, m, "event_debug")
	if !frameHas(m, "key a") {
		t.Fatal("event debug does not show the key recorded before it opened")
	}
	if !frameHas(m, "mouse click left 5,5") {
		t.Fatal("event debug does not show the click recorded before it opened")
	}
}

func TestEventDebugShowsOnlyTheLastTwentyEvents(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	for i := range 25 {
		press(m, typed(string(rune('a'+i))))
	}
	runAction(t, m, "event_debug")
	if frameHas(m, "key a") || frameHas(m, "key e") {
		t.Fatal("event debug shows an event older than the last twenty")
	}
	if !frameHas(m, "key y") || !frameHas(m, "key f") {
		t.Fatal("event debug does not show the newest events")
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("Esc left %d overlays open", len(m.overlays))
	}
}

func TestEventDebugNewestIsFirst(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	press(m, typed("a"))
	press(m, typed("b"))
	runAction(t, m, "event_debug")
	var rows []string
	for _, row := range frameText(m) {
		if strings.Contains(row, "key a") || strings.Contains(row, "key b") {
			rows = append(rows, row)
		}
	}
	if len(rows) != 2 || !strings.Contains(rows[0], "key b") {
		t.Fatalf("rows = %q, want key b above key a", rows)
	}
}
