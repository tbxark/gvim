package explorer

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRefreshPicksUpCreatedFile(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "Created.txt"))
	if hasRow(tree, "Created.txt") {
		t.Fatal("row visible before Refresh")
	}
	if err := tree.Refresh(); err != nil {
		t.Fatalf("Refresh() = %v", err)
	}
	if !hasRow(tree, "Created.txt") {
		t.Fatalf("Created.txt not listed after Refresh: %v", names(tree.Rows()))
	}
}

func TestExpandCollapseErrorsAndIdempotence(t *testing.T) {
	root := fixture(t)
	tree, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	alpha := filepath.Join(root, "alpha")
	file := filepath.Join(root, "Zeta.txt")

	if err := tree.Expand(alpha); err != nil {
		t.Fatalf("Expand(alpha) = %v", err)
	}
	if err := tree.Expand(alpha); err != nil {
		t.Fatalf("second Expand(alpha) = %v, want nil", err)
	}
	if !hasRow(tree, "deep") {
		t.Fatalf("alpha children not shown: %v", names(tree.Rows()))
	}
	if err := tree.Collapse(alpha); err != nil {
		t.Fatalf("Collapse(alpha) = %v", err)
	}
	if hasRow(tree, "deep") {
		t.Fatal("alpha children still shown after Collapse")
	}
	if err := tree.Collapse(alpha); err != nil {
		t.Fatalf("second Collapse(alpha) = %v, want nil", err)
	}
	if err := tree.Expand(file); !errors.Is(err, ErrNotDir) {
		t.Errorf("Expand(file) = %v, want ErrNotDir", err)
	}
	if err := tree.Collapse(file); !errors.Is(err, ErrNotDir) {
		t.Errorf("Collapse(file) = %v, want ErrNotDir", err)
	}
	if err := tree.Expand(filepath.Join(root, "missing")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Expand(missing) = %v, want ErrNotFound", err)
	}
}
