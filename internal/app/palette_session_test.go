package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/internal/uitest"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// startPaletteSession runs the real UI over root with the palette's file index, the
// file preview reader and Live Grep wired, on the file named file, so the palette slice
// is exercised end to end.
func startPaletteSession(t *testing.T, root, file string, w, h int) *uitest.Session {
	t.Helper()
	ed := editor.New(editor.Options{Rows: h - 3, Cols: w})
	if err := ed.OpenFile(filepath.Join(root, file)); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	// The tree gives the palette its root; the index lists the files.
	tree, err := explorer.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	index := finder.Start(context.Background(), root)
	t.Cleanup(index.Close)
	deps := uiadapter.NewDeps(ed, keymap.Default(), th, tree, index)
	deps.Root.Grep = grepFor(editor.Options{})
	deps.Root.Preview = previewReader{}
	return uitest.Start(t, tui.New(deps), w, h)
}

// TestPalettePreviewShowsTheFileHead: the file mode reads the selected file in a
// command and paints its head in the preview column, and Enter still opens the file.
func TestPalettePreviewShowsTheFileHead(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.txt": "first line\nsecond line\nthird line\n",
		"b.txt": "other\n",
	})
	s := startPaletteSession(t, root, "b.txt", 120, 34)
	// Ctrl+P opens the palette in command mode; Backspace clears the ">" and gives the
	// file mode.
	s.Key("ctrl+p")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Commands") })
	s.Key("backspace")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Files") })
	s.Type("a.txt")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("second line") })
	if !sc.Contains("1 │ first line") {
		t.Fatalf("the preview does not show the file head:\n%s", sc)
	}
	x, _, ok := sc.Find("1 │ first line")
	if !ok || x < 60 {
		t.Fatalf("the preview is not the right-hand column (x=%d):\n%s", x, sc)
	}
	s.Key("enter")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("first line") })
	if sc.Contains("Files") {
		t.Fatalf("the palette is still open:\n%s", sc)
	}
}

// TestLiveGrepTogglesAndScopesEndToEnd: the Live Grep toolbar shows the modes and
// scopes, Alt+I adds the ignored and hidden files, Alt+U searches the open buffers'
// unsaved text, and a buffer hit activates its tab. The title carries the count.
func TestLiveGrepTogglesAndScopesEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":  "ignored.txt\n",
		"a.txt":       "alpha needle\n",
		"ignored.txt": "needle ignored\n",
		".hidden.txt": "needle hidden\n",
	})
	s := startPaletteSession(t, root, "a.txt", 120, 34)
	// An unsaved line is what the Buffers scope searches.
	s.Key("ctrl+end")
	s.Key("enter")
	s.Type("zzz unsaved")
	// Live Grep opens on Alt+/, Fresh's key for it.
	s.Key("alt+/")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Live Grep") })
	for _, want := range []string{"Alt+C Case:off", "Alt+G Regex:on", "Alt+O Word:off", "Alt+L Files:on", "Alt+U Buffers:off", "Alt+I Ignored:off"} {
		if !sc.Contains(want) {
			t.Fatalf("the toolbar does not show %q:\n%s", want, sc)
		}
	}
	s.Type("needle")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("a.txt:1: alpha needle") })
	if sc.Contains("needle ignored") {
		t.Fatalf("the ignored scope is on by default:\n%s", sc)
	}
	if !sc.Contains("matches") {
		t.Fatalf("the title carries no match count:\n%s", sc)
	}
	// The Ignored scope adds the hidden and .gitignore-matched files.
	s.Key("alt+i")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("needle ignored") })
	if !sc.Contains("Ignored:on") || !sc.Contains("needle hidden") {
		t.Fatalf("the ignored scope did not search hidden and ignored files:\n%s", sc)
	}
	// The Buffers scope searches the unsaved text.
	s.Key("alt+u")
	for range len("needle") {
		s.Key("backspace")
	}
	s.Type("zzz")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("zzz unsaved") })
	if !sc.Contains("Buffers:on") {
		t.Fatalf("the Buffers scope did not search the open buffer:\n%s", sc)
	}
	s.Key("enter")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return !sc.Contains("Live Grep") })
	if !sc.Contains("zzz unsaved") {
		t.Fatalf("Enter on a buffer hit did not go to the line:\n%s", sc)
	}
}
