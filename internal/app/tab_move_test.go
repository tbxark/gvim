package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
)

// TestMoveTabToReordersTheRealEditorAndKeepsTheActiveDocument checks the adapter of a tab
// drag: the tab goes to the index asked for, the active tab stays on its document, and a
// bad index is an error.
func TestMoveTabToReordersTheRealEditorAndKeepsTheActiveDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed := editor.New(editor.Options{})
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		path := filepath.Join(dir, name)
		must(t, os.WriteFile(path, []byte(name), 0o644))
		must(t, ed.OpenFile(path))
	}
	a := editorAdapter{ed: ed, spans: &spanCache{}}
	names := func() (out []string) {
		for _, tab := range a.Tabs() {
			out = append(out, tab.Name)
		}
		return out
	}
	a.ActivateTab(0)
	if err := a.MoveTabTo(0, 2); err != nil {
		t.Fatalf("MoveTabTo(0, 2): %v", err)
	}
	if got := names(); len(got) != 3 || got[0] != "b.txt" || got[1] != "c.txt" || got[2] != "a.txt" {
		t.Fatalf("tabs = %v, want b.txt c.txt a.txt", got)
	}
	if tabs := a.Tabs(); !tabs[2].Active {
		t.Errorf("the active tab did not follow its document: %+v", tabs)
	}
	for _, bad := range [][2]int{{-1, 0}, {0, 3}, {3, 0}} {
		if err := a.MoveTabTo(bad[0], bad[1]); err == nil {
			t.Errorf("MoveTabTo(%d, %d) succeeded", bad[0], bad[1])
		}
	}
}
