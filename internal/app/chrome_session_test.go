package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/internal/uitest"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// startAppWithKeymap runs the real UI on one file with the named built-in keymap.
func startAppWithKeymap(t *testing.T, name, text string) *uitest.Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{Rows: 27, Cols: 100})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	km, err := keymap.ByName(name)
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(uiadapter.NewDeps(ed, km, th, nil, nil))
	return uitest.Start(t, m, 100, 30)
}

// TestAltLettersOpenMenus runs the real UI on the built-in default keymap: the menu
// accelerators are keymap bindings (Alt+letter -> menu_open, F10 -> menu_activate), so
// each chord opens its dropdown. The platform keymap shadows some letters with
// normal-context bindings (macOS Alt+F moves by word), which is why this asks for the
// default preset explicitly.
func TestAltLettersOpenMenus(t *testing.T) {
	tests := []struct{ key, row string }{
		{"alt+f", "New File"},
		{"alt+e", "Undo"},
		{"alt+v", "Line Numbers"},
		{"alt+s", "Select All"},
		{"alt+g", "Go to Line"},
		{"alt+h", "Show Help"},
		{"f10", "New File"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			s := startAppWithKeymap(t, "default", "hello\n")
			s.Key(tt.key)
			s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains(tt.row) })
		})
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
