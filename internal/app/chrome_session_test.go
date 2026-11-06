package app

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/uitest"
)

// TestAltLettersOpenTheirDropdownsWithTheDefaultKeymap runs the real UI on the
// platform's default keymap, where these Alt+letters are bound to menu_open with
// a menu name. Alt+S is left out: the macOS preset binds it to Save As, which
// wins over the global binding in the normal context.
func TestAltLettersOpenTheirDropdownsWithTheDefaultKeymap(t *testing.T) {
	tests := []struct {
		key, row string
	}{
		{"alt+f", "New File"},
		{"alt+e", "Undo"},
		{"alt+v", "Line Numbers"},
		{"alt+g", "Go to Line"},
		{"alt+h", "Show Help"},
	}
	for _, tt := range tests {
		s, _ := startApp(t, "hello\n")
		s.Key(tt.key)
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains(tt.row) })
	}
}

// TestLineNumbersRowInTheViewMenuHidesTheGutter clicks the View menu row that
// toggles the gutter, and checks that the body text moves to the first column.
func TestLineNumbersRowInTheViewMenuHidesTheGutter(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	s.Key("alt+v")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Line Numbers") })
	x, y, ok := sc.Find("Line Numbers")
	if !ok {
		t.Fatalf("the Line Numbers row is not on screen:\n%s", sc)
	}
	s.Click(x, y)
	sc = s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("Line Numbers") })
	// The menu bar is row 0 and the tab strip row 1, so the first body row is row 2.
	if row := sc.Row(2); !strings.HasPrefix(row, "hello") {
		t.Fatalf("body row 2 %q, want the text from the first column\n%s", row, sc)
	}
}
