package explorer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// countReads makes every directory read of the package count in the returned
// counter. It swaps the lister, so a test that calls it must not run in parallel.
func countReads(t *testing.T) *int {
	t.Helper()
	reads := new(int)
	prev := listDir
	listDir = func(dir string, opts ListOptions) ([]Entry, error) {
		*reads++
		return prev(dir, opts)
	}
	t.Cleanup(func() { listDir = prev })
	return reads
}

// TestRevealOfUnloadedFolderReadsOnlyInTheListing checks that the folders a reveal
// needs are read by the listing's Read, and that Apply and RevealLoaded read nothing.
// So the update loop can apply the listing and reveal the path without disk work.
func TestRevealOfUnloadedFolderReadsOnlyInTheListing(t *testing.T) {
	tests := []struct {
		name    string
		preload []string // folders expanded before the snapshot is taken
		target  string   // path revealed, relative to the root
		extra   int      // folders read for the reveal, beyond the listing of loaded folders
		gone    string   // folder removed after the snapshot, so its listing fails
		wantErr error    // error from RevealLoaded
	}{
		{name: "one unloaded folder", target: "alpha/leaf.txt", extra: 1},
		{name: "two unloaded folders", target: "alpha/deep/leaf.txt", extra: 2},
		{name: "folders already loaded", preload: []string{"alpha", "alpha/deep"}, target: "alpha/deep/leaf.txt"},
		{name: "the root itself", target: "."},
		{name: "outside the tree", target: "..", wantErr: ErrNotFound},
		{name: "a folder that cannot be listed", target: "alpha/leaf.txt", extra: 1, gone: "alpha", wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			must(t, os.MkdirAll(filepath.Join(dir, "alpha", "deep"), 0o755))
			must(t, os.WriteFile(filepath.Join(dir, "alpha", "leaf.txt"), nil, 0o644))
			must(t, os.WriteFile(filepath.Join(dir, "alpha", "deep", "leaf.txt"), nil, 0o644))
			tree, err := Open(dir)
			must(t, err)
			for _, rel := range tt.preload {
				must(t, tree.Expand(filepath.Join(dir, rel)))
			}
			target := filepath.Join(dir, tt.target)
			reads := countReads(t)

			before := *reads
			_ = tree.Snapshot().Read()
			loaded := *reads - before

			before = *reads
			snap := tree.SnapshotFor(target)
			if n := *reads - before; n != 0 {
				t.Fatalf("SnapshotFor read %d folders, want none", n)
			}
			if tt.gone != "" {
				must(t, os.RemoveAll(filepath.Join(dir, tt.gone)))
			}

			before = *reads
			listing := snap.Read()
			if got, want := *reads-before, loaded+tt.extra; got != want {
				t.Errorf("Read made %d folder reads, want %d", got, want)
			}

			before = *reads
			applyErr := tree.Apply(listing)
			if n := *reads - before; n != 0 {
				t.Errorf("Apply read %d folders, want none", n)
			}
			if (applyErr != nil) != (tt.gone != "") {
				t.Errorf("Apply error = %v, want an error only when a folder cannot be listed", applyErr)
			}

			before = *reads
			err = tree.RevealLoaded(target)
			if n := *reads - before; n != 0 {
				t.Errorf("RevealLoaded read %d folders, want none", n)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RevealLoaded error = %v, want %v", err, tt.wantErr)
			}
			if err == nil {
				if row, ok := tree.Selected(); !ok || row.Path != target {
					t.Errorf("selected = %+v, want %s", row, target)
				}
			}
		})
	}
}
