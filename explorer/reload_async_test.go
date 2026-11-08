package explorer

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// rowNames returns the names of the visible rows of t, in display order.
func rowNames(t *testing.T, tree *Tree) []string {
	t.Helper()
	var names []string
	for _, r := range tree.Rows() {
		names = append(names, r.Name)
	}
	return names
}

// TestSnapshotReadDoesNotChangeTheTree checks that reading the loaded directories
// from disk leaves the tree as it was, so the tree can be read off the update loop.
func TestSnapshotReadDoesNotChangeTheTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	tree, err := Open(dir)
	must(t, err)
	before := rowNames(t, tree)
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
	_ = tree.Snapshot().Read()
	if got := rowNames(t, tree); !slices.Equal(got, before) {
		t.Fatalf("rows after Read = %q, want the rows before %q", got, before)
	}
}

// TestApplyMergesTheDiskStateLikeReload checks that applying a listing read after
// a change on disk gives the tree the same rows that Reload gives an identical
// tree. The change must show up, so the comparison is not vacuous.
func TestApplyMergesTheDiskStateLikeReload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{
			name: "new and removed entries",
			change: func(t *testing.T, dir string) {
				must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
				must(t, os.Remove(filepath.Join(dir, "a.txt")))
			},
		},
		{
			name: "entry inside an expanded directory",
			change: func(t *testing.T, dir string) {
				must(t, os.WriteFile(filepath.Join(dir, "sub", "new.txt"), nil, 0o644))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			must(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
			must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
			viaApply, err := Open(dir)
			must(t, err)
			viaReload, err := Open(dir)
			must(t, err)
			for _, tree := range []*Tree{viaApply, viaReload} {
				must(t, tree.Expand(filepath.Join(dir, "sub")))
			}
			before := rowNames(t, viaApply)
			tt.change(t, dir)
			if err := viaApply.Apply(viaApply.Snapshot().Read()); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			must(t, viaReload.Reload())
			got, want := rowNames(t, viaApply), rowNames(t, viaReload)
			if !slices.Equal(got, want) {
				t.Fatalf("Apply rows = %q, Reload rows = %q", got, want)
			}
			if slices.Equal(got, before) {
				t.Fatalf("rows = %q did not change with the disk", got)
			}
		})
	}
}

// must fails the test when err is not nil.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
