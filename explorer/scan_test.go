package explorer

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func entryByName(entries []Entry, name string) (Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// TestListDirScanExclusionsTable checks that file_scan_exclusions entries never
// appear in a listing, hidden or shown, and that they beat inclusions.
func TestListDirScanExclusionsTable(t *testing.T) {
	tests := []struct {
		name string
		opts ListOptions
		want []string
	}{
		{
			name: "excluded entries are dropped even with ShowIgnored",
			opts: ListOptions{ShowHidden: true, ShowIgnored: true, ScanExclusions: []string{".DS_Store", "build/"}},
			want: []string{".env", ".gitignore", "a.txt", "build.log"},
		},
		{
			name: "inclusions rescue gitignored entries",
			opts: ListOptions{ShowHidden: true, ScanInclusions: []string{".env*", "build.log"}},
			want: []string{".DS_Store", ".env", ".gitignore", "a.txt", "build.log"},
		},
		{
			name: "exclusions beat inclusions",
			opts: ListOptions{ShowHidden: true, ShowIgnored: true, ScanExclusions: []string{".env"}, ScanInclusions: []string{".env*"}},
			want: []string{"build/", ".DS_Store", ".gitignore", "a.txt", "build.log"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, map[string]string{
				".gitignore": "*.log\n.env\nbuild/\n",
				".env":       "",
				"a.txt":      "",
				"build.log":  "",
				".DS_Store":  "",
				"build/x":    "",
			})
			if got := listNames(t, root, tc.opts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("names = %v\nwant   %v", got, tc.want)
			}
		})
	}
}

// TestListDirScanIncludedFlag checks that an entry a .gitignore matches but a
// scan inclusion rescues reports IsIgnored false and IsIncluded true.
func TestListDirScanIncludedFlag(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".gitignore": ".env\n", ".env": "", "a.txt": ""})
	entries, err := ListDir(root, ListOptions{ShowHidden: true, ScanInclusions: []string{".env*"}})
	if err != nil {
		t.Fatal(err)
	}
	env, ok := entryByName(entries, ".env")
	if !ok {
		t.Fatalf(".env not listed: %v", entries)
	}
	if env.IsIgnored || !env.IsIncluded {
		t.Fatalf(".env = ignored:%v included:%v, want not ignored and included", env.IsIgnored, env.IsIncluded)
	}
	a, _ := entryByName(entries, "a.txt")
	if a.IsIgnored || a.IsIncluded {
		t.Fatalf("a.txt = ignored:%v included:%v, want neither", a.IsIgnored, a.IsIncluded)
	}
}

// TestTreeShowsIgnoredByDefault checks that the tree draws gitignored entries by
// default, marked Row.Ignored, and that HideGitignore and a scan inclusion shape
// the flags.
func TestTreeShowsIgnoredByDefault(t *testing.T) {
	write := func(t *testing.T) string {
		root := t.TempDir()
		writeTree(t, root, map[string]string{".gitignore": "*.log\n", "a.txt": "", "build.log": "", ".env": ""})
		return root
	}
	t.Run("default rows include ignored entries marked Ignored", func(t *testing.T) {
		tree, err := Open(write(t))
		if err != nil {
			t.Fatal(err)
		}
		rows := tree.Rows()
		got := map[string]bool{}
		for _, r := range rows {
			got[r.Name] = r.Ignored
		}
		if v, ok := got["build.log"]; !ok || !v {
			t.Fatalf("build.log row missing or not ignored: %v", got)
		}
		if v := got["a.txt"]; v {
			t.Fatalf("a.txt marked ignored: %v", got)
		}
	})
	t.Run("HideGitignore option hides ignored rows", func(t *testing.T) {
		tree, err := OpenWithOptions(write(t), Options{HideGitignore: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range tree.Rows() {
			if r.Name == "build.log" {
				t.Fatalf("build.log drawn with HideGitignore: %v", tree.Rows())
			}
		}
	})
	t.Run("scan inclusions clear the ignored flag", func(t *testing.T) {
		tree, err := OpenWithOptions(write(t), Options{ScanInclusions: []string{"*.log"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range tree.Rows() {
			if r.Name == "build.log" && r.Ignored {
				t.Fatalf("build.log still ignored under an inclusion")
			}
		}
	})
}

// TestTreeIncludedChildOfIgnoredDir checks that a file matching a scan inclusion
// inside a gitignored directory is not ignored, while its siblings keep the
// inherited flag.
func TestTreeIncludedChildOfIgnoredDir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":  "build/\n",
		"a.txt":       "",
		"build/.env":  "",
		"build/x.bin": "",
	})
	tree, err := OpenWithOptions(root, Options{ScanInclusions: []string{".env*"}})
	if err != nil {
		t.Fatal(err)
	}
	tree.SetShowHidden(true)
	if err := tree.Expand(filepath.Join(root, "build")); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range tree.Rows() {
		got[r.Name] = r.Ignored
	}
	if v, ok := got["build"]; !ok || !v {
		t.Fatalf("build row missing or not ignored: %v", got)
	}
	if v, ok := got[".env"]; !ok || v {
		t.Fatalf(".env under an ignored dir = %v (present %v), want present and not ignored", v, ok)
	}
	if v, ok := got["x.bin"]; !ok || !v {
		t.Fatalf("x.bin under an ignored dir = %v (present %v), want present and ignored", v, ok)
	}
}

// TestTreeScanExclusionsDropRows checks that an excluded entry never becomes a
// node: no row, no find, no reveal.
func TestTreeScanExclusionsDropRows(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".DS_Store": "", "a.txt": ""})
	tree, err := OpenWithOptions(root, Options{ScanExclusions: []string{".DS_Store"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range tree.Rows() {
		if r.Name == ".DS_Store" {
			t.Fatalf(".DS_Store drawn: %v", tree.Rows())
		}
	}
	if err := tree.Select(filepath.Join(root, ".DS_Store")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Select(.DS_Store) = %v, want ErrNotFound", err)
	}
}

// TestWalkScanTable checks that WalkFiles drops excluded entries, rescues
// inclusion matches, and descends into an ignored directory only as far as the
// inclusions reach.
func TestWalkScanTable(t *testing.T) {
	files := map[string]string{
		".gitignore":  "*.log\nbuild/\n",
		"a.go":        "",
		"b.log":       "",
		".DS_Store":   "",
		"build/.env":  "",
		"build/x.bin": "",
	}
	tests := []struct {
		name string
		opts WalkOptions
		want []string
	}{
		{
			name: "exclusions drop entries and directories",
			opts: WalkOptions{ShowHidden: true, ShowIgnored: true, ScanExclusions: []string{".DS_Store", "build/"}},
			want: []string{".gitignore", "a.go", "b.log"},
		},
		{
			name: "inclusions rescue files under ignored directories",
			opts: WalkOptions{ShowHidden: true, ScanInclusions: []string{".env*"}},
			want: []string{".DS_Store", ".gitignore", "a.go", "build/.env"},
		},
		{
			name: "exclusions beat inclusions",
			opts: WalkOptions{ShowHidden: true, ScanExclusions: []string{".env"}, ScanInclusions: []string{".env*"}},
			want: []string{".DS_Store", ".gitignore", "a.go"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, files)
			var got []string
			err := WalkFiles(context.Background(), root, tc.opts, func(rel string) bool {
				got = append(got, rel)
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("walk = %v\nwant   %v", got, tc.want)
			}
		})
	}
}
