package app

// Regression tests for path-spelling mismatches in project-wide replace: the finder
// index can hold canonicalised paths (/private/var on macOS) while the editor's tab
// keeps the spelling it was opened with (/var), so open-buffer matching must resolve
// symlinks before comparing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/uitest"
)

// TestProjectReplaceReachesAnOpenTabWithCanonicalisedPaths searches a t.TempDir project
// (which on macOS is a /var path that canonicalises to /private/var) from the real app
// wiring and replaces across it: the file is open as a tab, so the replacement must
// land in that tab, be counted as a tab, and leave the disk untouched until Save All.
func TestProjectReplaceReachesAnOpenTabWithCanonicalisedPaths(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"alpha.txt": "one two three\ntwo four\n",
		"beta.txt":  "two\n",
	})
	alpha := filepath.Join(root, "alpha.txt")
	m, files, err := buildModel(t.Context(), Options{
		Keymap: "default",
		Root:   root,
		Files:  []FileTarget{{Path: alpha}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	s := uitest.Start(t, m, 120, 32)
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("one two three") })
	s.Key("ctrl+shift+p")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Commands") })
	s.Type("search and replace in project")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Search and Replace in Project") })
	s.Key("enter")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Search & Replace") })
	s.Type("two")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("matches in 2 files") })
	s.Key("tab")
	s.Type("TWO")
	s.Key("alt+enter")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Replace 3 matches in 2 files?") })
	s.Key("y")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("files changed in") })
	row := noticeRow(sc.String())
	if row == "" {
		t.Fatalf("no replace notice on screen:\n%s", sc)
	}
	if strings.Contains(row, "0 files changed in 0 tabs") || strings.Contains(row, "skipped") {
		t.Fatalf("replace did not reach the open tab and the other file: %q", row)
	}
	if !strings.Contains(row, "2 files changed in 2 tabs") {
		t.Fatalf("replace notice = %q, want 2 files changed in 2 tabs", row)
	}
	// The deliberate divergence: unsaved tabs until Save All, so the disk keeps the
	// old text.
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "TWO") {
			t.Fatalf("%s was written to disk:\n%s", name, data)
		}
	}
}

// noticeRow returns the last screen row that holds a replace notice, trimmed.
func noticeRow(screen string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "files changed in") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
