package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
)

// TestFindSelectionThroughTheRootIsCaseInsensitive drives
// find_selection_next on the real editor through the root. The word under the caret
// becomes the search under the bar's toggles, so "todo" also finds "TODO", and the
// search is held by the bar for F3.
func TestFindSelectionThroughTheRootIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	must(t, os.WriteFile(path, []byte("todo x TODO\n"), 0o644))
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(path))
	a := editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
	m := ui.New(ui.Deps{Editor: a, Theme: theme.Theme{}})

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
