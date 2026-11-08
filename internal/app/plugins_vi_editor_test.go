package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// at finds text on the screen and returns the cell of its first character.
func (s *pluginSession) at(text string) (x, y int) {
	s.t.Helper()
	for y, row := range s.rows() {
		if i := strings.Index(row, text); i >= 0 {
			return len([]rune(row[:i])), y
		}
	}
	s.t.Fatalf("%q is not on the screen:\n%s", text, strings.Join(s.rows(), "\n"))
	return 0, 0
}

func (s *pluginSession) drag(x0, y, x1 int) {
	s.t.Helper()
	s.send(tea.MouseClickMsg(tea.Mouse{X: x0, Y: y, Button: tea.MouseLeft}))
	s.send(tea.MouseMotionMsg(tea.Mouse{X: x1, Y: y, Button: tea.MouseLeft}))
	s.send(tea.MouseReleaseMsg(tea.Mouse{X: x1, Y: y, Button: tea.MouseLeft}))
	s.settle(20 * time.Millisecond)
}

// TestViModeTakesTheMouseSelection: a drag with the mouse is the visual mode, and d acts on it.
func TestViModeTakesTheMouseSelection(t *testing.T) {
	s := viSession(t, "alpha beta\ngamma delta\nepsilon\n")
	x, y := s.at("alpha beta")
	s.drag(x+6, y, x+10)
	s.until("the visual label", func() bool { return strings.Contains(s.statusRow(), "-- VISUAL --") })
	s.typeText("d")
	s.until("normal again", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	if got := s.text(); got != "alpha \ngamma delta\nepsilon\n" {
		t.Fatalf("text after the drag and d = %q", got)
	}
}

// TestViModePaste: a paste in the normal mode is put before the caret, in the insert mode it is
// inserted, and none of it is discarded or run as keys.
func TestViModePaste(t *testing.T) {
	s := viSession(t, "alpha beta\ngamma delta\nepsilon\n")
	s.send(tea.PasteMsg{Content: "dd\nxyz"})
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "dd\nxyzalpha beta\ngamma delta\nepsilon\n" {
		t.Fatalf("text after a paste in the normal mode = %q", got)
	}
	if !strings.Contains(s.statusRow(), "-- NORMAL --") {
		t.Fatalf("status = %q", s.statusRow())
	}
	s.typeText("u")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "alpha beta\ngamma delta\nepsilon\n" {
		t.Fatalf("text after undoing the paste = %q", got)
	}
	s.typeText("A")
	s.until("insert", func() bool { return strings.Contains(s.statusRow(), "-- INSERT --") })
	s.send(tea.PasteMsg{Content: "!!"})
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "alpha beta!!\ngamma delta\nepsilon\n" {
		t.Fatalf("text after a paste in the insert mode = %q", got)
	}
}

// TestViModeCtrlC: ctrl+c is an Esc in vi: it leaves the insert mode without copying, and it
// cancels the : and / prompts.
func TestViModeCtrlC(t *testing.T) {
	s := viSession(t, "alpha beta\n")
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	s.typeText("i")
	s.until("insert", func() bool { return strings.Contains(s.statusRow(), "-- INSERT --") })
	s.press(ctrlC)
	s.until("normal after ctrl+c", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	if strings.Contains(s.statusRow(), "Copied") {
		t.Fatalf("ctrl+c copied: %q", s.statusRow())
	}
	for _, open := range []string{":", "/"} {
		s.typeText(open)
		s.until("the prompt", func() bool { return s.screenHas(open) && !strings.Contains(s.statusRow(), "-- NORMAL --") })
		s.press(ctrlC)
		s.until("the prompt to close", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
		s.settle(50 * time.Millisecond) // vi drops keys while it waits for the prompt's answer
		before := s.text()
		s.typeText("x")
		s.settle(20 * time.Millisecond)
		if got := s.text(); len(got) != len(before)-1 {
			t.Fatalf("after ctrl+c in the %s prompt, x is not a command: %q (was %q)", open, got, before)
		}
		s.typeText("u")
		s.settle(20 * time.Millisecond)
	}
}

// TestViModeEscMashingIsNotThePanicChord: Esc, Esc, Esc with a prompt in between does not switch vi off either.
func TestViModeEscMashingIsNotThePanicChord(t *testing.T) {
	s := viSession(t, "alpha beta\n")
	s.typeText(":")
	s.until("the prompt", func() bool { return !strings.Contains(s.statusRow(), "-- NORMAL --") })
	s.esc()
	s.esc()
	s.esc()
	s.until("normal", func() bool { return strings.Contains(s.statusRow(), "-- NORMAL --") })
	s.settle(50 * time.Millisecond)
	if !strings.Contains(s.statusRow(), "-- NORMAL --") {
		t.Fatalf("vi went off: %q", s.statusRow())
	}
}
