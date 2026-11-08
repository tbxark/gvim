package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// latin1File writes a Latin-1 file of about 5 KiB, which is large for a 1 KiB threshold.

// A large file that is not UTF-8 is not decoded whole before the first frame: it gets no
// tab, and the UI asks about it once it runs (tui.RootPorts.FullLoadPrompts).
func TestFileArgumentsLeaveANonUTF8FileOfLargeSizeToThePrompt(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(root, "a.txt")
	must(t, os.WriteFile(other, []byte("a\n"), 0o644))
	l1 := latin1File(t, root)
	rt := largeRuntime(t, root, `{"editor": {"large_file_threshold_bytes": 1024}}`, other, l1)
	tabs := rt.ed.Tabs()
	if len(tabs) != 1 || tabs[0].Path != other {
		t.Fatalf("tabs = %+v, want only the file that opens at once", tabs)
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

// latin1File writes a Latin-1 file of about 5 KiB, which is large for a 1 KiB threshold.
func latin1File(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "l1.txt")
	must(t, os.WriteFile(path, []byte(strings.Repeat("caf\xe9 line\n", 500)), 0o644))
	return path
}
