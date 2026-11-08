package uiadapter

import (
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/highlight"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/theme"
)

// TestFindSelectionThroughTheRootIsCaseInsensitive drives
// find_selection_next on the real editor through the root. The word under the caret
// becomes the search under the bar's toggles, so "todo" also finds "TODO", and the
// search is held by the bar for F3.
func TestFindSelectionThroughTheRootIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("todo x TODO\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	m := tui.New(tui.Deps{Editor: a, Theme: theme.Theme{}})

	a.SetCursor(buffer.Pos{Line: 0, Col: 2}, false)
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatalf("find_selection_next: %v", err)
	}
	d := ed.Active()
	if !d.HasSelection() {
		t.Fatal("nothing selected")
	}
	if from, _ := d.Sel.Ordered(); from != (buffer.Pos{Line: 0, Col: 7}) {
		t.Fatalf("selection from %v, want the TODO at 0:7", from)
	}
}
