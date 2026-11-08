package uiadapter

import (
	"github.com/tbxark/gvim/internal/apptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
)

// A goto reads the file in a command and opens it in Update. The adapter's read and
// open must leave the same tab that OpenFile leaves.
func TestEditorAdapterOpensAFileReadBeforehand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.go")
	apptest.Must(t, os.WriteFile(path, []byte("package b\n"), 0o644))
	ed := editor.New(editor.Options{})
	a := &Editor{ed: ed}

	doc, err := a.LoadFunc(path)()
	apptest.Must(t, err)
	a.OpenLoaded(doc)

	tabs := ed.Tabs()
	if len(tabs) != 1 || tabs[0].Path != path || !tabs[0].Active {
		t.Fatalf("tabs = %+v, want the one active tab for %s", tabs, path)
	}
}
