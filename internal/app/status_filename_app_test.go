package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/uitest"
)

// TestStatusFilenameShowsRootRelativeNameAcrossPathSpellings is the end-to-end
// regression for the macOS class the round-5 visual check found: the workspace root
// is canonicalised (/private/var/...) while the open document keeps the spelling it
// was opened with (/var/...), so the {filename} element showed an absolute path
// instead of the root-relative name. On a filesystem without that quirk a symlinked
// spelling of the file stands in for it, and the test skips when symlinks are
// unavailable.
func TestStatusFilenameShowsRootRelativeNameAcrossPathSpellings(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"alpha.txt": "hello alpha\n"})
	canon, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	openPath := filepath.Join(root, "alpha.txt")
	if canon == root {
		link := root + "-spelled"
		if err := os.Symlink(root, link); err != nil {
			t.Skipf("no path spelling difference available: %v", err)
		}
		openPath = filepath.Join(link, "alpha.txt")
	}
	cfg := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, cfg, `{"editor": {"status_bar": {"left": ["{filename}"], "right": []}}}`)
	m, files, err := buildModel(t.Context(), Options{
		Keymap: "default",
		Root:   canon,
		Config: cfg,
		Files:  []FileTarget{{Path: openPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	s := uitest.Start(t, m, 100, 30)
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("hello alpha") })
	s.Settle()
	sc := s.Screen()
	row := strings.TrimSpace(sc.Row(sc.Height() - 1))
	if !strings.Contains(row, "alpha.txt") {
		t.Fatalf("status row = %q, want the root-relative file name", row)
	}
	if strings.Contains(row, openPath) || strings.Contains(row, root) {
		t.Fatalf("status row = %q, want no absolute path spelling", row)
	}
}
