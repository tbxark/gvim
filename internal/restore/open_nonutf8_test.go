package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

func TestSessionRestoreOpensANonUTF8FileOfLargeSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l1 := latin1File(t, dir)
	sess := filepath.Join(dir, "session")
	ed := editor.New(apptest.LargeOpts())
	load := ed.LoadFullFunc(l1)
	d, err := load()
	apptest.Must(t, err)
	ed.OpenLoaded(d)
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	apptest.Must(t, err)

	ed2 := editor.New(apptest.LargeOpts())
	if failed := RestoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	if tabs := ed2.Tabs(); len(tabs) != 1 || tabs[0].Path != l1 {
		t.Fatalf("tabs = %+v, want the file back", tabs)
	}

	// Unsaved text of such a file comes back as well.
	ed.Do("insert_char", map[string]any{"text": "typed "})
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err = session.Load(sess)
	apptest.Must(t, err)
	ed3 := editor.New(apptest.LargeOpts())
	if failed := RestoreSession(ed3, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	if d := ed3.Active(); d == nil || !d.Modified() || !strings.HasPrefix(d.Buffer.Line(0), "typed ") {
		t.Fatalf("the unsaved text of the file did not come back")
	}
}

func latin1File(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "l1.txt")
	apptest.Must(t, os.WriteFile(path, []byte(strings.Repeat("caf\xe9 line\n", 500)), 0o644))
	return path
}
