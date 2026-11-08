package explorer

import (
	"path/filepath"
	"reflect"
	"testing"
)

// chainFixture builds a tree whose "a/b/c" folders each hold exactly one folder:
//
//	root/
//	  a/
//	    b/
//	      c/
//	        leaf.txt
//	  plain/
//	    one.txt
//	    two.txt
func chainFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/b/c/leaf.txt": "",
		"plain/one.txt":  "",
		"plain/two.txt":  "",
	})
	return root
}

// TestCompactDirectoriesFoldChains checks Fresh's rule: an expanded folder whose
// only visible child is a folder has no row, and the deepest folder's row carries
// the chain, drawn at the depth of the folded ancestors.
func TestCompactDirectoriesFoldChains(t *testing.T) {
	root := chainFixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// Expand the whole chain so the rows are fresh.
	if err := tree.Expand(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	tree.SetCompactDirectories(true)
	if !tree.CompactDirectories() {
		t.Fatal("CompactDirectories() = false after SetCompactDirectories(true)")
	}
	if err := tree.Reveal(filepath.Join(root, "a", "b", "c", "leaf.txt")); err != nil {
		t.Fatal(err)
	}
	rows := tree.Rows()
	var anchor Row
	found := false
	for _, r := range rows {
		if r.Name == "c" {
			anchor, found = r, true
		}
	}
	if !found {
		t.Fatalf("rows = %v, want the a/b/c row", names(rows))
	}
	wantChain := []ChainSegment{{Name: "a", Path: filepath.Join(root, "a")}, {Name: "b", Path: filepath.Join(root, "a", "b")}}
	if !reflect.DeepEqual(anchor.Chain, wantChain) {
		t.Fatalf("anchor chain = %+v, want %+v", anchor.Chain, wantChain)
	}
	if anchor.Depth != 1 {
		t.Errorf("anchor depth = %d, want 1: the folded ancestors do not indent", anchor.Depth)
	}
	if want := []string{"root", "  a/b/c", "    leaf.txt", "  plain"}; !reflect.DeepEqual(rowNamesWithChains(rows), want) {
		t.Fatalf("rows = %v\nwant    %v", rowNamesWithChains(rows), want)
	}
}

// rowNamesWithChains names each row by its chain plus its own name, indented by
// its depth. The root's name is replaced by "root" so a temp directory does not
// enter the expectations.
func rowNamesWithChains(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		label := ""
		for _, seg := range r.Chain {
			label += seg.Name + "/"
		}
		label += r.Name
		if i == 0 {
			label = "root"
		}
		out[i] = indent(r.Depth) + label
	}
	return out
}

// TestParentIsTheNearestRowAncestor checks Row.Parent, which the sticky band
// follows: it is the index of the nearest ancestor that has a row, so folded
// ancestors never appear in the chain.
func TestParentIsTheNearestRowAncestor(t *testing.T) {
	root := chainFixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Reveal(filepath.Join(root, "a", "b", "c", "leaf.txt")); err != nil {
		t.Fatal(err)
	}
	tree.SetCompactDirectories(true)
	rows := tree.Rows()
	if len(rows) < 2 || rows[0].Parent != -1 {
		t.Fatalf("root parent = %v in %v", rows[0].Parent, rowNamesWithChains(rows))
	}
	for i, r := range rows[1:] {
		if r.Parent < 0 || r.Parent >= i+1 || rows[r.Parent].Depth != r.Depth-1 {
			t.Errorf("row %d (%s) parent = %d, want the row above with one less depth", i+1, r.Name, r.Parent)
		}
	}
}

// TestCompactToggleExpandsTheWholeChain checks expanding a folder under compact
// mode also expands the single-child folders below it, so the chain stays on one
// row, and that collapsing the anchor closes only its own folder.
func TestCompactToggleExpandsTheWholeChain(t *testing.T) {
	root := chainFixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	tree.SetCompactDirectories(true)
	if err := tree.Toggle(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	// a, b and c are all expanded: the chain renders as one row.
	rows := tree.Rows()
	if len(rows) != 4 || len(rows[1].Chain) != 2 || !rows[1].Expanded {
		t.Fatalf("rows after expanding a = %v, want the expanded a/b/c row and its leaf", rowNamesWithChains(rows))
	}
	// Collapsing the anchor closes c only; the chain row stays a/b/c.
	if err := tree.Toggle(filepath.Join(root, "a", "b", "c")); err != nil {
		t.Fatal(err)
	}
	rows = tree.Rows()
	if len(rows) != 3 || len(rows[1].Chain) != 2 || rows[1].Expanded {
		t.Fatalf("rows after collapsing c = %v, want the collapsed a/b/c row", rowNamesWithChains(rows))
	}
}

// TestCompactRevealSelectsTheAnchor checks a reveal of a folded folder: it has no
// row, so the selection moves to the deepest row that shows it.
func TestCompactRevealSelectsTheAnchor(t *testing.T) {
	root := chainFixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	tree.SetCompactDirectories(true)
	// Reveal b: it is collapsed, so it has a row inside the a/b chain.
	if err := tree.Reveal(filepath.Join(root, "a", "b")); err != nil {
		t.Fatal(err)
	}
	if sel, _ := tree.Selected(); sel.Name != "b" {
		t.Fatalf("selected after revealing b = %s, want b's row", sel.Path)
	}
	// Expanding b folds it into c's row; the selection follows the anchor.
	if err := tree.Toggle(filepath.Join(root, "a", "b")); err != nil {
		t.Fatal(err)
	}
	sel, ok := tree.Selected()
	if !ok {
		t.Fatal("no selection after expanding the chain")
	}
	if want := filepath.Join(root, "a", "b", "c"); sel.Path != want {
		t.Errorf("selected = %s, want the anchor %s", sel.Path, want)
	}
}
