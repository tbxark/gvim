package explorer

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// TestTreeShowOptionsTable checks that SetShowHidden and SetShowIgnored change
// which rows are drawn, without a reload, and that the defaults hide both.
func TestTreeShowOptionsTable(t *testing.T) {
	tests := []struct {
		name        string
		showHidden  bool
		showIgnored bool
		want        []string
	}{
		{name: "defaults hide dot-entries and ignored entries", want: []string{"root", "  a.txt"}},
		{name: "show hidden", showHidden: true, want: []string{"root", "  .env", "  .gitignore", "  a.txt"}},
		{name: "show ignored", showIgnored: true, want: []string{"root", "  a.txt", "  build.log"}},
		{name: "show both", showHidden: true, showIgnored: true, want: []string{"root", "  .env", "  .gitignore", "  a.txt", "  build.log"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, map[string]string{".gitignore": "*.log\n", ".env": "", "a.txt": "", "build.log": ""})
			tree, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			tree.SetShowHidden(tc.showHidden)
			tree.SetShowIgnored(tc.showIgnored)
			if tree.ShowHidden() != tc.showHidden || tree.ShowIgnored() != tc.showIgnored {
				t.Fatalf("getters = %v, %v", tree.ShowHidden(), tree.ShowIgnored())
			}
			got := names(tree.Rows())
			// The root's name is the temp directory's base name; compare the rest.
			got[0] = "root"
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rows = %v\nwant    %v", got, tc.want)
			}
		})
	}
}

// TestTreeHiddenNodesStayLoaded checks that toggling hidden entries back on
// shows them without a reload, and that Reveal and Toggle refuse a hidden entry
// while they are off.
func TestTreeHiddenNodesStayLoaded(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".secret/": "", ".secret/key": "", "a.txt": ""})
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, ".secret")
	if err := tree.Reveal(filepath.Join(secret, "key")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reveal(hidden) = %v, want ErrNotFound", err)
	}
	if err := tree.Toggle(secret); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Toggle(hidden) = %v, want ErrNotFound", err)
	}
	tree.SetShowHidden(true)
	if err := tree.Toggle(secret); err != nil {
		t.Fatalf("Toggle(hidden) after SetShowHidden: %v", err)
	}
	if err := tree.Reveal(filepath.Join(secret, "key")); err != nil {
		t.Fatalf("Reveal(hidden) after SetShowHidden: %v", err)
	}
	tree.SetShowHidden(false)
	if err := tree.Select(secret); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Select(hidden row) = %v, want ErrNotFound", err)
	}
}

// TestWalkFilesTable checks the walk fallback's rules: gitignore, hidden names,
// skipped directories, and early stop.
func TestWalkFilesTable(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		opts  WalkOptions
		want  []string
	}{
		{
			name:  "gitignore prunes files and directories",
			files: map[string]string{".gitignore": "*.log\nout/\n", "a.go": "", "b.log": "", "out/x.go": "", "src/c.go": ""},
			want:  []string{"a.go", "src/c.go"},
		},
		{
			name:  "hidden names skipped unless asked",
			files: map[string]string{".env": "", ".github/ci.yml": "", "a.go": ""},
			want:  []string{"a.go"},
			opts:  WalkOptions{},
		},
		{
			name:  "ShowHidden walks dot-directories",
			files: map[string]string{".env": "", ".github/ci.yml": "", "a.go": ""},
			opts:  WalkOptions{ShowHidden: true},
			want:  []string{".env", ".github/ci.yml", "a.go"},
		},
		{
			name:  "SkipDirs not entered at any depth",
			files: map[string]string{"node_modules/x.js": "", "pkg/node_modules/y.js": "", "pkg/z.js": ""},
			opts:  WalkOptions{SkipDirs: []string{"node_modules"}},
			want:  []string{"pkg/z.js"},
		},
		{
			name:  "ShowIgnored walks gitignored files",
			files: map[string]string{".gitignore": "*.log\n", "b.log": "", "a.go": ""},
			opts:  WalkOptions{ShowIgnored: true},
			want:  []string{"a.go", "b.log"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, tc.files)
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

func TestWalkFilesStopsAndCancels(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a": "", "b": "", "c": ""})
	calls := 0
	if err := WalkFiles(context.Background(), root, WalkOptions{}, func(string) bool {
		calls++
		return false
	}); err != nil || calls != 1 {
		t.Fatalf("stop: err=%v calls=%d, want nil and 1", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WalkFiles(ctx, root, WalkOptions{}, func(string) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled walk = %v, want context.Canceled", err)
	}
	if err := WalkFiles(context.Background(), filepath.Join(root, "missing"), WalkOptions{}, func(string) bool { return true }); err == nil {
		t.Fatal("walk of a missing root returned no error")
	}
}
