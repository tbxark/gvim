package pluginhost

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPluginOSCopyRefusesAliasesIntoSourceBeforeWrite: a lexical parent check is not
// enough. A symlinked source or a destination reached through a symlinked parent must be
// canonicalized and refused before the first write, and a dangling destination link must
// not be followed/guessed.
func TestPluginOSCopyRefusesAliasesIntoSourceBeforeWrite(t *testing.T) {
	o, dir := newTestOS(t)
	writePluginTree(t, dir, map[string]string{
		"tree/x.txt":      "X",
		"tree/deep/y.txt": "Y",
		"other.txt":       "O",
	})
	mustSymlink := func(target, link string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
	}
	mustSymlink("tree", "alias")
	mustSymlink("tree", "selfdir")
	mustSymlink("tree/new", "dangling")
	mustSymlink("tree", "parentalias")

	cases := []struct{ from, to string }{
		{"tree", "tree/new"},
		{"tree", "alias/new"},
		{"alias", "tree/new"},
		{"alias", "alias/new"},
		{"tree", "dangling"},
		{"tree", "selfdir"},
		{"tree", "tree"},
	}
	for _, tc := range cases {
		if err := o.Copy(t.Context(), tc.from, tc.to); err == nil {
			t.Fatalf("Copy(%q, %q) succeeded, want a safe refusal", tc.from, tc.to)
		}
		if _, err := os.Lstat(filepath.Join(dir, "tree", "new")); !os.IsNotExist(err) {
			t.Fatalf("Copy(%q, %q) wrote into the source tree", tc.from, tc.to)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "tree", "x.txt")); err != nil || string(b) != "X" {
		t.Fatalf("source changed: %q, %v", b, err)
	}
	// A resolved alias source to a proven-safe destination is still allowed: the safety
	// rule is about self/subtree and unprovable aliases, not about rejecting every link.
	if err := o.Copy(t.Context(), "alias", "copy"); err != nil {
		t.Fatalf("safe alias copy: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "copy", "x.txt")); err != nil || string(b) != "X" {
		t.Fatalf("safe alias copy = %q, %v", b, err)
	}
}
