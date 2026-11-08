package explorer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The fixture's rows are: root, alpha, Beta.txt, chapter-2.md, chapter-10.md,
// Zeta.txt. Expanding alpha adds deep and inner.txt after it.

func TestRowAtAppliesScrollOffset(t *testing.T) {
	tests := []struct {
		name   string
		scroll []int // ScrollBy calls, in order
		y      int
		wantOK bool
		want   string // row name; the root is "" and has Depth 0
	}{
		{name: "first line is the root", y: 0, wantOK: true, want: ""},
		{name: "last row", y: 5, wantOK: true, want: "Zeta.txt"},
		{name: "past the last row", y: 6, wantOK: false},
		{name: "above the body", y: -1, wantOK: false},
		{name: "scrolled by two", scroll: []int{2}, y: 0, wantOK: true, want: "Beta.txt"},
		{name: "scrolled by two, last visible", scroll: []int{2}, y: 3, wantOK: true, want: "Zeta.txt"},
		{name: "scrolled by two, past the end", scroll: []int{2}, y: 4, wantOK: false},
		{name: "scroll clamps at the last row", scroll: []int{99}, y: 0, wantOK: true, want: "Zeta.txt"},
		{name: "scroll clamps at the top", scroll: []int{5, -99}, y: 0, wantOK: true, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			tree, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range tc.scroll {
				tree.ScrollBy(n)
			}
			row, ok := tree.RowAt(tc.y)
			if ok != tc.wantOK {
				t.Fatalf("RowAt(%d) ok = %v, want %v", tc.y, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if tc.want == "" {
				if row.Path != root || row.Depth != 0 {
					t.Fatalf("RowAt(%d) = %+v, want the root", tc.y, row)
				}
				return
			}
			if row.Name != tc.want {
				t.Fatalf("RowAt(%d) = %q, want %q", tc.y, row.Name, tc.want)
			}
		})
	}
}

func TestScrollByClampsToRows(t *testing.T) {
	tests := []struct {
		name        string
		expandAlpha bool  // 8 rows instead of 6
		collapse    bool  // collapse alpha after scrolling
		deltas      []int // ScrollBy calls, in order
		want        int   // ScrollOffset afterwards
	}{
		{name: "no moves", deltas: nil, want: 0},
		{name: "two single steps", deltas: []int{1, 1}, want: 2},
		{name: "up from the top stays at zero", deltas: []int{-3}, want: 0},
		{name: "down past the end clamps to the last row", deltas: []int{50}, want: 5},
		{name: "steps accumulate then clamp", deltas: []int{3, 3, 3}, want: 5},
		{name: "expanded tree has more room", expandAlpha: true, deltas: []int{7}, want: 7},
		{name: "expanded tree clamps to its own last row", expandAlpha: true, deltas: []int{50}, want: 7},
		{
			name: "offset past the end after collapse reads as the last row", expandAlpha: true,
			collapse: true, deltas: []int{7}, want: 5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			tree, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			alpha := filepath.Join(root, "alpha")
			if tc.expandAlpha {
				if err := tree.Expand(alpha); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range tc.deltas {
				tree.ScrollBy(d)
			}
			if tc.collapse {
				if err := tree.Collapse(alpha); err != nil {
					t.Fatal(err)
				}
			}
			if got := tree.ScrollOffset(); got != tc.want {
				t.Fatalf("ScrollOffset() = %d, want %d (rows %d)", got, tc.want, len(tree.Rows()))
			}
		})
	}
}

func TestSelectAndToggleTable(t *testing.T) {
	tests := []struct {
		name    string
		op      string // "select" or "toggle"
		path    string // relative to the root
		wantErr error
		wantRow int // rows after the call
	}{
		{name: "select visible file", op: "select", path: "Beta.txt", wantRow: 6},
		{name: "select hidden child of collapsed dir", op: "select", path: "alpha/inner.txt", wantErr: ErrNotFound, wantRow: 6},
		{name: "select missing path", op: "select", path: "nowhere", wantErr: ErrNotFound, wantRow: 6},
		{name: "toggle collapsed dir opens it", op: "toggle", path: "alpha", wantRow: 8},
		{name: "toggle a file", op: "toggle", path: "Beta.txt", wantErr: ErrNotDir, wantRow: 6},
		{name: "toggle missing path", op: "toggle", path: "ghost", wantErr: ErrNotFound, wantRow: 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			tree, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, filepath.FromSlash(tc.path))
			var got error
			switch tc.op {
			case "select":
				got = tree.Select(path)
			case "toggle":
				got = tree.Toggle(path)
			}
			if !errors.Is(got, tc.wantErr) {
				t.Fatalf("%s(%s) = %v, want %v", tc.op, tc.path, got, tc.wantErr)
			}
			if n := len(tree.Rows()); n != tc.wantRow {
				t.Fatalf("rows = %d, want %d", n, tc.wantRow)
			}
		})
	}
}

func TestReloadTable(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		check  func(t *testing.T, tree *Tree, root string)
	}{
		{
			name: "new file in the root appears",
			mutate: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "Added.txt"))
			},
			check: func(t *testing.T, tree *Tree, root string) {
				if !hasRow(tree, "Added.txt") {
					t.Fatal("Added.txt not listed after Reload")
				}
			},
		},
		{
			name: "removed file disappears and the selection is kept",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "chapter-2.md")); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, tree *Tree, root string) {
				if hasRow(tree, "chapter-2.md") {
					t.Fatal("removed file still listed")
				}
				if r, _ := tree.Selected(); r.Name != "Zeta.txt" {
					t.Fatalf("selection = %s, want Zeta.txt kept", r.Name)
				}
			},
		},
		{
			name: "expanded directory stays expanded and gains children",
			mutate: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "alpha", "new.txt"))
			},
			check: func(t *testing.T, tree *Tree, root string) {
				if !hasRow(tree, "new.txt") || !hasRow(tree, "inner.txt") {
					t.Fatalf("alpha children after Reload = %v", names(tree.Rows()))
				}
			},
		},
		{
			name: "deleted directory and its paths are forgotten",
			mutate: func(t *testing.T, root string) {
				if err := os.RemoveAll(filepath.Join(root, "alpha")); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, tree *Tree, root string) {
				if hasRow(tree, "alpha") {
					t.Fatal("deleted directory still listed")
				}
				if err := tree.Toggle(filepath.Join(root, "alpha")); !errors.Is(err, ErrNotFound) {
					t.Fatalf("Toggle(deleted) = %v, want ErrNotFound", err)
				}
			},
		},
		{
			name: "new .gitignore hides entries on the next reload",
			mutate: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("Beta.txt\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, tree *Tree, root string) {
				if hasRow(tree, "Beta.txt") {
					t.Fatal("Beta.txt still listed after its .gitignore rule was added")
				}
				if !hasRow(tree, "Zeta.txt") {
					t.Fatal("Zeta.txt wrongly hidden")
				}
			},
		},
		{
			name: "removed .gitignore shows entries again",
			mutate: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.md\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, tree *Tree, root string) {
				if hasRow(tree, "chapter-2.md") {
					t.Fatal("chapter-2.md listed while *.md is ignored")
				}
				if err := os.Remove(filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
				if err := tree.Reload(); err != nil {
					t.Fatal(err)
				}
				if !hasRow(tree, "chapter-2.md") {
					t.Fatal("chapter-2.md still hidden after .gitignore removed")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			tree, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := tree.Expand(filepath.Join(root, "alpha")); err != nil {
				t.Fatal(err)
			}
			if err := tree.Select(filepath.Join(root, "Zeta.txt")); err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, root)
			if err := tree.Reload(); err != nil {
				t.Fatalf("Reload() = %v", err)
			}
			tc.check(t, tree, root)
		})
	}
}

// hasRow reports whether a drawn row has the given name.
func hasRow(tree *Tree, name string) bool {
	for _, r := range tree.Rows() {
		if r.Name == name {
			return true
		}
	}
	return false
}
