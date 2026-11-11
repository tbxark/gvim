package app

import (
	"fmt"
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

// startEmptyApp runs the real UI with no tab open, as gvim started without files.
// The recent list is kept in a temporary config directory.
func startEmptyApp(t *testing.T, w, h int) *uitest.Session {
	t.Helper()
	isolateConfig(t)
	ed := editor.New(editor.Options{Rows: h - 3, Cols: w, Empty: true})
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(uiadapter.NewDeps(ed, keymap.Default(), th, nil, nil))
	return uitest.Start(t, m, w, h)
}

// startFilesApp runs the real UI with one tab per name, the last one active, in
// a w by h frame. The files are created in a temporary directory.
func startFilesApp(t *testing.T, w, h int, names ...string) *uitest.Session {
	t.Helper()
	isolateConfig(t)
	dir := t.TempDir()
	ed := editor.New(editor.Options{Rows: h - 3, Cols: w})
	for _, n := range names {
		path := filepath.Join(dir, n)
		if err := os.WriteFile(path, []byte(n+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenFile(path); err != nil {
			t.Fatal(err)
		}
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(uiadapter.NewDeps(ed, keymap.Default(), th, nil, nil))
	return uitest.Start(t, m, w, h)
}

// The welcome screen shows at startup without files, and its doors are drawn.
func TestWelcomeShowsAtStartupWithoutFiles(t *testing.T) {
	s := startEmptyApp(t, 80, 24)
	sc := s.Screen()
	for _, want := range []string{"a terminal editor", "New File", "Open File", "Command Palette"} {
		if !sc.Contains(want) {
			t.Fatalf("welcome lacks %q:\n%s", want, sc)
		}
	}
	if sc.Height() != 24 || sc.Width() != 80 {
		t.Fatalf("screen is %dx%d, want 80x24", sc.Width(), sc.Height())
	}
}

// Ctrl+N hides the welcome screen and opens an untitled tab.
func TestCtrlNHidesWelcomeAndOpensUntitledTab(t *testing.T) {
	s := startEmptyApp(t, 80, 24)
	s.Key("ctrl+n")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Untitled-1") })
	if sc.Contains("a terminal editor") {
		t.Fatalf("the welcome screen is still shown after Ctrl+N:\n%s", sc)
	}
}

// Closing the last clean tab with Ctrl+W shows the welcome screen again.
func TestCloseTabKeyOnLastTabShowsWelcomeAgain(t *testing.T) {
	s, _ := startApp(t, "hello\n")
	s.Key(closeTabChord(t))
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("a terminal editor") })
	if sc.Contains("a.txt") {
		t.Fatalf("the tab is still shown after closing it:\n%s", sc)
	}
}

// A click on the New File row opens an untitled tab.
func TestClickingNewFileRowOpensUntitledTab(t *testing.T) {
	s := startEmptyApp(t, 80, 24)
	x, y, ok := s.Screen().Find("New File")
	if !ok {
		t.Fatal("New File row not drawn")
	}
	s.Click(x+1, y)
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Untitled-1") })
	if sc.Contains("a terminal editor") {
		t.Fatalf("the welcome screen is still shown after New File:\n%s", sc)
	}
}

// The "+" stays on screen at the right end of the strip when twelve tabs overflow
// it, the active tab is in view, and a click on ">" scrolls by one tab.
func TestTabStripOverflowAtSixtyColumns(t *testing.T) {
	names := make([]string, 12)
	for i := range names {
		names[i] = fmt.Sprintf("f%02d.txt", i)
	}
	s := startFilesApp(t, 60, 20, names...)
	sc := s.Screen()
	if !sc.Contains("f11.txt") {
		t.Fatalf("active tab f11.txt is not in the strip:\n%s", sc)
	}
	// The menu occupies row 0, the frame's top border row 1 and the tab strip row 2;
	// the "+" is the three-cell button at columns 56..58 with its glyph in the middle.
	if got := sc.Cell(57, 2).Char; got != "+" {
		t.Fatalf("cell (57,2) = %q, want the pinned +:\n%s", got, sc)
	}
	if !strings.Contains(sc.Row(2), "<") {
		t.Fatalf("no left marker on a scrolled strip:\n%s", sc)
	}
	before := sc.Row(2)
	s.Click(1, 2) // the left marker scrolls one tab back
	scrolled := s.Screen().Row(2)
	if before == scrolled || !strings.Contains(scrolled, ">") {
		t.Fatalf("a click on < did not scroll the strip back one tab:\n%s", scrolled)
	}
	s.Click(55, 2) // the right marker, one cell left of the button, scrolls forward again
	if got := s.Screen().Row(2); got != before {
		t.Fatalf("a click on > did not scroll forward:\nwant %q\ngot  %q", before, got)
	}
	if got := s.Screen().Cell(57, 2).Char; got != "+" {
		t.Fatalf("plus moved after scrolling: %q", got)
	}
	// A click on + opens its menu, as Fresh's does. Its second row, New File, opens the
	// untitled tab.
	s.Click(57, 2)
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("New Terminal") })
	if !sc.Contains("New File") {
		t.Fatalf("clicking + did not open its menu:\n%s", sc)
	}
	s.Key("down")
	s.Key("enter")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Untitled-1") })
	if !sc.Contains("Untitled-1") {
		t.Fatalf("New File from the + menu did not open an untitled tab:\n%s", sc)
	}
}
