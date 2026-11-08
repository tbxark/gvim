package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
	"github.com/tbxark/gvim/internal/uitest"
)

// startApp runs the real UI on a file in a temporary directory, sized 100 by
// 30. It returns the session and the file path.
func startApp(t *testing.T, text string) (*uitest.Session, string) {
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
	m := ui.New(newUIDeps(ed, keymap.Default(), th, nil, nil))
	return uitest.Start(t, m, 100, 30), path
}

// TestPaletteOpensTheMenuBar: with F10 gone, the "Open Menu Bar" palette command opens the File
// menu, and no cell of the bar is underlined.
func TestPaletteOpensTheMenuBar(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	s.Key("ctrl+p")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Open Menu Bar") || sc.Contains(">") })
	s.Type("open menu bar")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Open Menu Bar") })
	s.Key("enter")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("New File") })
	if got := sc.Underlined(0); len(got) != 0 {
		t.Fatalf("underlined menu-bar cells = %q, want none", got)
	}
}

func TestHoverSlidesTheOpenDropdownAcrossTitles(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	fx, fy, ok := s.Screen().Find("File")
	if !ok || fy != 0 {
		t.Fatal("File title not on the menu bar")
	}
	s.Click(fx, fy)
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("New File") })
	// Find the Edit title on the bar, then hover it: its dropdown must replace
	// the File dropdown, with no click.
	sc := s.Screen()
	x, y, ok := sc.Find("Edit")
	if !ok || y != 0 {
		t.Fatalf("Edit title not on the menu bar:\n%s", sc)
	}
	s.Hover(x, y)
	sc = s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("New File") })
	if !sc.Contains("Undo") {
		t.Fatalf("hovering Edit did not open its dropdown:\n%s", sc)
	}
}

func TestCtrlWOnADirtyTabAsksBeforeClosing(t *testing.T) {
	s, _ := startApp(t, "alpha\n")
	s.Type("X")
	s.Key("ctrl+w")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Unsaved Changes") })
	s.Key("esc")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("Unsaved Changes") })
	if !sc.Contains("a.txt") {
		t.Fatalf("Esc closed the tab:\n%s", sc)
	}
}

func TestMenuBarHasNoUnderlinedMnemonicsWhileClosed(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	sc := s.Screen()
	if got := sc.Underlined(0); len(got) != 0 {
		t.Fatalf("closed menu bar underlines = %q, want none:\n%s", got, sc)
	}
}

func TestHelpMenuShowsGvimNotFresh(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	x, y, ok := s.Screen().Find("Help")
	if !ok || y != 0 {
		t.Fatal("Help title not on the menu bar")
	}
	s.Click(x, y)
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Show Help") })
	if !sc.Contains("gvim") {
		t.Fatalf("Help menu has no gvim header:\n%s", sc)
	}
	if strings.Contains(sc.String(), "Fresh") {
		t.Fatalf("Help menu names Fresh:\n%s", sc)
	}
}
