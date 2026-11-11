package explorer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fixture builds:
//
//	root/
//	  Zeta.txt
//	  alpha/
//	    inner.txt
//	    deep/
//	      leaf.txt
//	  Beta.txt
//	  chapter-10.md
//	  chapter-2.md
//	  .hidden
//	  .git/config
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "alpha", "deep"))
	mustMkdir(t, filepath.Join(root, ".git"))
	for _, f := range []string{
		"Zeta.txt", "Beta.txt", "chapter-10.md", "chapter-2.md", ".hidden",
		"alpha/inner.txt", "alpha/deep/leaf.txt", ".git/config",
	} {
		mustWrite(t, filepath.Join(root, f))
	}
	return root
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%s%s", indent(r.Depth), r.Name)
	}
	return out
}

func indent(d int) string {
	s := ""
	for range d {
		s += "  "
	}
	return s
}

func TestRowsOrderAndHiddenFiles(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	got := names(tree.Rows())
	want := []string{
		filepath.Base(root),
		"  alpha",
		"  Beta.txt",
		"  chapter-2.md",
		"  chapter-10.md",
		"  Zeta.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v\nwant %v", got, want)
	}
}

// TestGitDirectoryNeverListed checks the repository metadata directory is not a
// node of the tree and not a row, even with Show Hidden Files on: it cannot be
// expanded, revealed or operated on, so it cannot stall the explorer.
func TestGitDirectoryNeverListed(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".git/config": "", ".git/objects/ab/cd": "", ".env": "", "a.txt": ""})
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	tree.SetShowHidden(true)
	got := names(tree.Rows())
	got[0] = "root"
	if want := []string{"root", "  .env", "  a.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	git := filepath.Join(root, ".git")
	if err := tree.Toggle(git); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Toggle(.git) = %v, want ErrNotFound", err)
	}
	if err := tree.Reveal(filepath.Join(git, "config")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reveal(.git/config) = %v, want ErrNotFound", err)
	}
}

func TestExpandShowsChildrenDirectoriesFirst(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Expand(filepath.Join(root, "alpha")); err != nil {
		t.Fatal(err)
	}
	rows := tree.Rows()
	if rows[2].Name != "deep" || !rows[2].IsDir || rows[2].Depth != 2 {
		t.Fatalf("row 2 = %+v, want subdirectory deep at depth 2", rows[2])
	}
	if rows[3].Name != "inner.txt" || rows[3].IsDir {
		t.Fatalf("row 3 = %+v, want inner.txt", rows[3])
	}
}

func TestToggleAndCollapse(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	alpha := filepath.Join(root, "alpha")
	if err := tree.Toggle(alpha); err != nil {
		t.Fatal(err)
	}
	if n := len(tree.Rows()); n != 8 {
		t.Fatalf("after toggle open rows = %d, want 8", n)
	}
	if err := tree.Toggle(alpha); err != nil {
		t.Fatal(err)
	}
	if n := len(tree.Rows()); n != 6 {
		t.Fatalf("after toggle closed rows = %d, want 6", n)
	}
	if err := tree.Collapse(root); err != nil {
		t.Fatal(err)
	}
	if n := len(tree.Rows()); n != 1 {
		t.Fatalf("collapsed root rows = %d, want 1", n)
	}
}

func TestReopenLoadedDirectory(t *testing.T) {
	for _, method := range []string{"toggle", "expand"} {
		t.Run(method, func(t *testing.T) {
			root := fixture(t)
			tr, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := tr.Collapse(root); err != nil {
				t.Fatal(err)
			}
			if rows := tr.Rows(); len(rows) != 1 || rows[0].Expanded {
				t.Fatalf("collapsed rows = %+v", rows)
			}
			if method == "toggle" {
				err = tr.Toggle(root)
			} else {
				err = tr.Expand(root)
			}
			if err != nil {
				t.Fatal(err)
			}
			if rows := tr.Rows(); len(rows) != 6 || !rows[0].Expanded {
				t.Fatalf("reopened rows = %+v, want expanded root and its children", rows)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Beta.txt")
	if err := tree.Toggle(file); !errors.Is(err, ErrNotDir) {
		t.Errorf("Toggle(file) = %v, want ErrNotDir", err)
	}
	if err := tree.Expand(filepath.Join(root, "missing")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Expand(missing) = %v, want ErrNotFound", err)
	}
	if err := tree.Collapse(file); !errors.Is(err, ErrNotDir) {
		t.Errorf("Collapse(file) = %v, want ErrNotDir", err)
	}
	if _, err := Open(file); !errors.Is(err, ErrNotDir) {
		t.Errorf("Open(file) = %v, want ErrNotDir", err)
	}
	if _, err := Open(filepath.Join(root, "nope")); err == nil {
		t.Error("Open(missing dir) succeeded")
	}
}

func TestMoveClampsAndSelects(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := tree.Selected(); r.Path != root {
		t.Fatalf("initial selection = %s, want root", r.Path)
	}
	tree.Move(1)
	if r, _ := tree.Selected(); r.Name != "alpha" {
		t.Fatalf("after Move(1) = %s, want alpha", r.Name)
	}
	tree.Move(-10)
	if r, _ := tree.Selected(); r.Path != root {
		t.Fatalf("Move(-10) should clamp to root, got %s", r.Path)
	}
	tree.Move(100)
	if r, _ := tree.Selected(); r.Name != "Zeta.txt" {
		t.Fatalf("Move(100) should clamp to last row, got %s", r.Name)
	}
}

func TestSelectionMovesOffWhenAncestorCollapses(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Reveal(filepath.Join(root, "alpha", "inner.txt")); err != nil {
		t.Fatal(err)
	}
	if r, _ := tree.Selected(); r.Name != "inner.txt" {
		t.Fatalf("Reveal selected %s, want inner.txt", r.Name)
	}
	if err := tree.Collapse(filepath.Join(root, "alpha")); err != nil {
		t.Fatal(err)
	}
	if r, _ := tree.Selected(); r.Path != root {
		t.Fatalf("hidden selection should fall back to first row, got %s", r.Path)
	}
}

func TestRevealExpandsAncestors(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Reveal(filepath.Join(root, "alpha", "deep", "leaf.txt")); err != nil {
		t.Fatal(err)
	}
	rows := tree.Rows()
	if got := rows[len(rows)-1]; got.Name != "Zeta.txt" {
		t.Fatalf("last row = %s", got.Name)
	}
	var found bool
	for _, r := range rows {
		if r.Name == "leaf.txt" && r.Depth == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("leaf.txt not visible after Reveal: %v", names(rows))
	}
	if err := tree.Reveal(filepath.Dir(root)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reveal outside root = %v, want ErrNotFound", err)
	}
	if err := tree.Select("/nowhere"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Select unknown = %v, want ErrNotFound", err)
	}
}

func TestNaturalCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"chapter-2.md", "chapter-10.md", -1},
		{"chapter-21.md", "chapter-3.md", 1},
		{"Foo.txt", "foo.txt", 0},
		{"bar.txt", "Foo.txt", -1},
		{"v1", "v01", -1},
		{"v01", "v1", 1},
		{"v002.txt", "v10.txt", -1},
		{"img2a.png", "img10a.png", -1},
		{"img2b.png", "img2a.png", 1},
		{"img.png", "img2.png", -1},
		{"", "a", -1},
		{"same", "same", 0},
	}
	for _, tc := range tests {
		if got := naturalCompare(tc.a, tc.b); sign(got) != tc.want {
			t.Errorf("naturalCompare(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func TestSymlinkToDirectoryIsExpandable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	mustMkdir(t, target)
	mustWrite(t, filepath.Join(target, "inside.txt"))
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range tree.Rows() {
		if r.Name == "link" && !r.IsDir {
			t.Fatalf("symlink to directory should be a dir row: %+v", r)
		}
	}
}

func ExampleTree_Rows() {
	dir, _ := os.MkdirTemp("", "explorer-example")
	defer func() { _ = os.RemoveAll(dir) }()
	_ = os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "src", "main.go"), nil, 0o644)

	tree, _ := Open(dir)
	_ = tree.Expand(filepath.Join(dir, "src"))
	for _, r := range tree.Rows()[1:] { // row 0 is the temporary root
		fmt.Printf("%d %s dir=%v\n", r.Depth, r.Name, r.IsDir)
	}
	// Output:
	// 1 src dir=true
	// 2 main.go dir=false
	// 1 README.md dir=false
}

func TestRevealAndRootEdgeCases(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Root(); got != root {
		t.Fatalf("Root() = %q, want %q", got, root)
	}
	// Revealing the root selects it without expanding anything new.
	if err := tree.Reveal(root); err != nil {
		t.Fatalf("Reveal(root) = %v", err)
	}
	// A path that runs through a file cannot be revealed.
	if err := tree.Reveal(filepath.Join(root, "Beta.txt", "x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reveal through a file = %v, want ErrNotFound", err)
	}
	// A path that does not exist below a real directory is not found.
	if err := tree.Reveal(filepath.Join(root, "alpha", "ghost.txt")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reveal missing = %v, want ErrNotFound", err)
	}
	if err := tree.Toggle(filepath.Join(root, "ghost")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Toggle missing = %v, want ErrNotFound", err)
	}
}

func TestMoveOnEmptyDirectory(t *testing.T) {
	empty := t.TempDir()
	tree, err := Open(empty)
	if err != nil {
		t.Fatal(err)
	}
	if rows := tree.Rows(); len(rows) != 1 || rows[0].Path != tree.Root() {
		t.Fatalf("empty tree rows = %+v", rows)
	}
	tree.Move(3) // clamps to the only row without panicking
	if r, ok := tree.Selected(); !ok || r.Path != tree.Root() {
		t.Fatalf("Selected() = %+v, %v", r, ok)
	}
}

func TestDanglingSymlinkIsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(root, "dangling")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	rows := tree.Rows()
	if len(rows) != 2 || rows[1].IsDir {
		t.Fatalf("dangling symlink rows = %+v", rows)
	}
}
