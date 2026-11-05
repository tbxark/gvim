package explorer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// listNames returns the names ListDir gives for dir, with a "/" after each
// directory, so tests can compare order and kind in one slice.
func listNames(t *testing.T, dir string, opts ListOptions) []string {
	t.Helper()
	entries, err := ListDir(dir, opts)
	if err != nil {
		t.Fatalf("ListDir(%q): %v", dir, err)
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
		if e.IsDir {
			out[i] += "/"
		}
	}
	return out
}

func TestListDirOrderAndHiddenRules(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // a trailing "/" makes a directory
		opts  ListOptions
		want  []string
	}{
		{
			name:  "directories first then natural case-insensitive names",
			files: map[string]string{"Zeta.txt": "", "beta.txt": "", "Alpha/": "", "chapter-10.md": "", "chapter-2.md": "", "apple/": ""},
			want:  []string{"Alpha/", "apple/", "beta.txt", "chapter-2.md", "chapter-10.md", "Zeta.txt"},
		},
		{
			name:  "dot-entries hidden by default",
			files: map[string]string{".hidden": "", ".config/": "", "a.txt": ""},
			want:  []string{"a.txt"},
		},
		{
			name:  "ShowHidden lists dot-entries in the same order",
			files: map[string]string{".hidden": "", ".config/": "", "a.txt": ""},
			opts:  ListOptions{ShowHidden: true},
			want:  []string{".config/", ".hidden", "a.txt"},
		},
		{
			name:  "gitignored entries hidden by default",
			files: map[string]string{".gitignore": "*.log\nbuild/\n", "a.log": "", "b.txt": "", "build/": ""},
			want:  []string{"b.txt"},
		},
		{
			name:  "ShowIgnored lists gitignored entries",
			files: map[string]string{".gitignore": "*.log\n", "a.log": "", "b.txt": ""},
			opts:  ListOptions{ShowIgnored: true},
			want:  []string{"a.log", "b.txt"},
		},
		{
			name:  "negation re-includes a file",
			files: map[string]string{".gitignore": "*.log\n!keep.log\n", "keep.log": "", "x.log": ""},
			want:  []string{"keep.log"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.files)
			got := listNames(t, dir, tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ListDir = %v\nwant       %v", got, tc.want)
			}
		})
	}
}

// TestListDirIgnoreRootAppliesAncestorRules checks that a nested directory is
// listed with the .gitignore files from IgnoreRoot down, as the tree applies them.
func TestListDirIgnoreRootAppliesAncestorRules(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":      "*.tmp\n",
		"sub/.gitignore":  "local.txt\n",
		"sub/local.txt":   "",
		"sub/a.tmp":       "",
		"sub/b.go":        "",
		"sub/deeper/z.go": "",
	})
	got := listNames(t, filepath.Join(root, "sub"), ListOptions{IgnoreRoot: root})
	want := []string{"deeper/", "b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListDir = %v, want %v", got, want)
	}
	// Without IgnoreRoot, dir is its own root, so sub/.gitignore alone applies.
	got = listNames(t, filepath.Join(root, "sub"), ListOptions{})
	want = []string{"deeper/", "a.tmp", "b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListDir without IgnoreRoot = %v, want %v", got, want)
	}
}

func TestListDirIgnoredAncestorHidesEverything(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":    "build/\n",
		"build/keep.go": "",
	})
	got := listNames(t, filepath.Join(root, "build"), ListOptions{IgnoreRoot: root})
	if len(got) != 0 {
		t.Fatalf("ListDir inside an ignored directory = %v, want none", got)
	}
}

func TestListDirEntryFields(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"dir/": "", "file.txt": "hello", ".dot": ""})
	if err := os.Symlink(filepath.Join(root, "dir"), filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	entries, err := ListDir(root, ListOptions{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	if e := byName["file.txt"]; e.IsDir || e.IsSymlink || e.IsHidden || e.Size != 5 || e.ModTime.IsZero() {
		t.Errorf("file.txt = %+v", e)
	}
	if e := byName[".dot"]; !e.IsHidden {
		t.Errorf(".dot = %+v, want IsHidden", e)
	}
	if e := byName["link-dir"]; !e.IsDir || !e.IsSymlink {
		t.Errorf("link-dir = %+v, want IsDir and IsSymlink", e)
	}
	if e := byName["dangling"]; e.IsDir || !e.IsSymlink {
		t.Errorf("dangling = %+v, want a file symlink", e)
	}
	if got := listNames(t, root, ListOptions{ShowHidden: true}); !reflect.DeepEqual(got, []string{"dir/", "link-dir/", ".dot", "dangling", "file.txt"}) {
		t.Errorf("order = %v", got)
	}
}

func TestListDirErrors(t *testing.T) {
	if _, err := ListDir(filepath.Join(t.TempDir(), "absent"), ListOptions{}); err == nil {
		t.Fatal("ListDir of a missing directory returned no error")
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{"f.txt": ""})
	if _, err := ListDir(filepath.Join(root, "f.txt"), ListOptions{}); err == nil {
		t.Fatal("ListDir of a file returned no error")
	}
	writeTree(t, root, map[string]string{"sub/": "", "other/": ""})
	if _, err := ListDir(filepath.Join(root, "sub"), ListOptions{IgnoreRoot: filepath.Join(root, "other")}); err == nil {
		t.Fatal("ListDir with dir outside IgnoreRoot returned no error")
	}
}
