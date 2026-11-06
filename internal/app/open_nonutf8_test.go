package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// latin1File writes a Latin-1 file of about 5 KiB, which is large for a 1 KiB threshold.
func latin1File(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "l1.txt")
	must(t, os.WriteFile(path, []byte(strings.Repeat("caf\xe9 line\n", 500)), 0o644))
	return path
}

func TestFileArgumentsOpenANonUTF8FileOfLargeSize(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(root, "a.txt")
	must(t, os.WriteFile(other, []byte("a\n"), 0o644))
	l1 := latin1File(t, root)
	rt := largeRuntime(t, root, `{"editor": {"large_file_threshold_bytes": 1024}}`, other, l1)
	tabs := rt.ed.Tabs()
	if len(tabs) != 2 || tabs[1].Path != l1 {
		t.Fatalf("tabs = %+v, want both files", tabs)
	}
	d := rt.ed.Active()
	if d.Large() || d.Buffer.Line(0) != "café line" {
		t.Errorf("large %v, line 0 = %q: want the file decoded whole", d.Large(), d.Buffer.Line(0))
	}
}

func TestSessionRestoreOpensANonUTF8FileOfLargeSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l1 := latin1File(t, dir)
	sess := filepath.Join(dir, "session")
	ed := editor.New(largeOpts())
	load := ed.LoadFullFunc(l1)
	d, err := load()
	must(t, err)
	ed.OpenLoaded(d)
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	must(t, err)

	ed2 := editor.New(largeOpts())
	if failed := restoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	if tabs := ed2.Tabs(); len(tabs) != 1 || tabs[0].Path != l1 {
		t.Fatalf("tabs = %+v, want the file back", tabs)
	}

	// Unsaved text of such a file comes back as well.
	ed.Do("insert_char", map[string]any{"text": "typed "})
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err = session.Load(sess)
	must(t, err)
	ed3 := editor.New(largeOpts())
	if failed := restoreSession(ed3, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	if d := ed3.Active(); d == nil || !d.Modified() || !strings.HasPrefix(d.Buffer.Line(0), "typed ") {
		t.Fatalf("the unsaved text of the file did not come back")
	}
}

func TestFileArgumentOverTheFullLoadLimitIsANoticeNotAnAbort(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(root, "a.txt")
	must(t, os.WriteFile(other, []byte("a\n"), 0o644))
	huge := filepath.Join(root, "huge.txt")
	must(t, os.WriteFile(huge, []byte(strings.Repeat("caf\xe9 line\n", 8000)), 0o644))
	// The full-load limit is not an option; a sparse file makes the real one reachable.
	f, err := os.OpenFile(huge, os.O_WRONLY, 0)
	must(t, err)
	must(t, f.Truncate(300<<20))
	must(t, f.Close())
	rt := largeRuntime(t, root, `{"editor": {"large_file_threshold_bytes": 1024}}`, huge, other)
	if tabs := rt.ed.Tabs(); len(tabs) != 1 || tabs[0].Path != other {
		t.Fatalf("tabs = %+v, want only the file that can be opened", tabs)
	}
}
