package app

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/uitest"
)

// TestAltLettersNoLongerOpenMenus runs the real UI on the platform's default keymap: the
// menu-activation keys (F10, Alt+letter) are not bound, so no menu opens. The old test
// pinned Alt+F, Alt+E, Alt+V, Alt+G and Alt+H opening their dropdowns.
func TestAltLettersNoLongerOpenMenus(t *testing.T) {
	for _, key := range []string{"alt+f", "alt+e", "alt+v", "alt+g", "alt+h", "f10"} {
		s, _ := startApp(t, "hello\n")
		s.Key(key)
		// Type a probe so that the key has been handled before the screen is read.
		s.Type("x")
		sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("xhello") })
		for _, row := range []string{"New File", "Undo", "Line Numbers", "Go to Line", "Show Help"} {
			if sc.Contains(row) {
				t.Errorf("%s opened a dropdown showing %q:\n%s", key, row, sc)
			}
		}
	}
}

// TestMenuTitlesOpenTheirDropdownsWithTheMouse: every title opens by a click.
func TestMenuTitlesOpenTheirDropdownsWithTheMouse(t *testing.T) {
	tests := []struct{ title, row string }{
		{"File", "New File"},
		{"Edit", "Undo"},
		{"View", "Line Numbers"},
		{"Go", "Go to Line"},
		{"Help", "Show Help"},
	}
	for _, tt := range tests {
		s, _ := startApp(t, "hello\n")
		x, y, ok := s.Screen().Find(tt.title)
		if !ok || y != 0 {
			t.Fatalf("%s title not on the menu bar", tt.title)
		}
		s.Click(x, y)
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains(tt.row) })
	}
}

// TestLineNumbersRowInTheViewMenuHidesTheGutter clicks the View menu row that
// toggles the gutter, and checks that the body text moves to the first column.
func TestLineNumbersRowInTheViewMenuHidesTheGutter(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	vx, vy, ok := s.Screen().Find("View")
	if !ok || vy != 0 {
		t.Fatal("View title not on the menu bar")
	}
	s.Click(vx, vy)
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Line Numbers") })
	x, y, ok := sc.Find("Line Numbers")
	if !ok {
		t.Fatalf("the Line Numbers row is not on screen:\n%s", sc)
	}
	s.Click(x, y)
	sc = s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("Line Numbers") })
	// The menu band is rows 0..1 and the tab strip row 2, so the first body row is row 3.
	// With the gutter hidden the text starts one padding cell in, not at the frame edge.
	if row := sc.Row(3); !strings.HasPrefix(row, " hello") {
		t.Fatalf("body row 3 %q, want the text after one padding cell\n%s", row, sc)
	}
}
