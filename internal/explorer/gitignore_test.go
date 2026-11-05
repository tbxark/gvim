package explorer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// writeTree creates files under root. A key ending in "/" is a directory; any
// other key is a file whose content is the value.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			mustMkdir(t, p)
			continue
		}
		mustMkdir(t, filepath.Dir(p))
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// diskEntries returns every non-dot entry under root as a slash-separated
// relative path, parents first. Directories end in "/".
func diskEntries(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			name += "/"
		}
		out = append(out, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// expandAll expands every directory on disk, so the tree has loaded all of
// them. Ignored directories are loaded too, though they are not drawn.
func expandAll(t *testing.T, tree *Tree, root string) {
	t.Helper()
	for _, e := range diskEntries(t, root) {
		if before, ok := strings.CutSuffix(e, "/"); ok {
			if err := tree.Expand(filepath.Join(root, filepath.FromSlash(before))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// shownEntries returns the relative paths of the drawn rows below the root,
// in the same form as diskEntries.
func shownEntries(t *testing.T, tree *Tree) []string {
	t.Helper()
	var out []string
	for _, r := range tree.Rows()[1:] {
		rel, err := filepath.Rel(tree.Root(), r.Path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.ToSlash(rel)
		if r.IsDir {
			name += "/"
		}
		out = append(out, name)
	}
	return out
}

// hiddenEntries is the set difference: entries on disk that are not drawn.
func hiddenEntries(t *testing.T, tree *Tree, root string) []string {
	t.Helper()
	shown := make(map[string]bool)
	for _, s := range shownEntries(t, tree) {
		shown[s] = true
	}
	var hidden []string
	for _, e := range diskEntries(t, root) {
		if !shown[e] {
			hidden = append(hidden, e)
		}
	}
	return hidden
}

// TestHiddenSetMatchesRule checks, for each fixture, that the entries left out
// of the drawn rows are exactly the ones the .gitignore rules select. An entry
// under a hidden directory is hidden with it. The expected lists are written by
// hand from the gitignore rules, not computed by the code under test.
func TestHiddenSetMatchesRule(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		hidden []string // sorted, relative, directories end in "/"
	}{
		{
			name:   "no ignore file hides nothing",
			files:  map[string]string{"a.log": "", "dir/": "", "dir/b.txt": ""},
			hidden: nil,
		},
		{
			name:   "unanchored pattern matches at any depth",
			files:  map[string]string{".gitignore": "*.log\n", "a.log": "", "a.txt": "", "x/": "", "x/b.log": "", "x/c.txt": ""},
			hidden: []string{"a.log", "x/b.log"},
		},
		{
			name:   "negation re-includes a matched file",
			files:  map[string]string{".gitignore": "*.log\n!keep.log\n", "keep.log": "", "other.log": ""},
			hidden: []string{"other.log"},
		},
		{
			name:   "anchored pattern matches only at its own directory",
			files:  map[string]string{".gitignore": "/top.txt\n", "top.txt": "", "x/": "", "x/top.txt": ""},
			hidden: []string{"top.txt"},
		},
		{
			name:   "directory-only pattern hides the directory and its contents",
			files:  map[string]string{".gitignore": "build/\n", "build/": "", "build/out.txt": "", "build.txt": ""},
			hidden: []string{"build/", "build/out.txt"},
		},
		{
			name: "nested ignore file applies only below its directory",
			files: map[string]string{
				"docs/.gitignore":        "notes.md\n",
				"notes.md":               "",
				"docs/notes.md":          "",
				"docs/sub/":              "",
				"docs/sub/notes.md":      "",
				"docs/sub/readme.md":     "",
				"docs/sub/.gitignore":    "",
				"docs/sub/deep/":         "",
				"docs/sub/deep/n.md":     "",
				"docs/sub/deep/notes.md": "",
			},
			hidden: []string{"docs/notes.md", "docs/sub/deep/notes.md", "docs/sub/notes.md"},
		},
		{
			name: "deeper ignore file overrides an ancestor",
			files: map[string]string{
				".gitignore":      "*.log\n",
				"keep.log":        "",
				"docs/.gitignore": "!keep.log\n",
				"docs/keep.log":   "",
				"docs/x.log":      "",
			},
			hidden: []string{"docs/x.log", "keep.log"},
		},
		{
			name:   "comments, blank lines and CRLF endings",
			files:  map[string]string{".gitignore": "# a comment\n\n*.tmp\r\n", "a.tmp": "", "a.txt": ""},
			hidden: []string{"a.tmp"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, tc.files)
			tree, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			expandAll(t, tree, root)
			got := hiddenEntries(t, tree, root)
			slices.Sort(got)
			want := slices.Clone(tc.hidden)
			slices.Sort(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("hidden = %v\nwant    %v", got, want)
			}
		})
	}
}

// TestRevealRefusesIgnoredEntry checks that Reveal does not select an entry the
// tree is hiding.
func TestRevealRefusesIgnoredEntry(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".gitignore": "*.log\n", "a.log": ""})
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Reveal(filepath.Join(root, "a.log")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reveal(ignored) = %v, want ErrNotFound", err)
	}
}

// TestNegationCannotReincludeUnderIgnoredDir checks that a negation cannot
// re-include an entry below an excluded directory. git excludes everything under
// an excluded directory, so build/keep.txt stays hidden.
func TestNegationCannotReincludeUnderIgnoredDir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":     "build/\n!build/keep.txt\n",
		"build/":         "",
		"build/keep.txt": "",
		"build/o":        "",
	})
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	expandAll(t, tree, root)
	if got := hiddenEntries(t, tree, root); len(got) != 3 {
		t.Fatalf("hidden = %v, want build/, build/keep.txt, build/o", got)
	}
}
