package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/uiadapter"
)

// TestProjectTreeIsLoadedWhenOpened checks that opening a folder reads its entries
// and expands its root. The open runs in the command that opens the project, so when
// the UI adopts the project on the update loop, the root's expansion reads no disk.
func TestProjectTreeIsLoadedWhenOpened(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
	p := uiadapter.NewProjects(t.Context(), nil, dir, nil)
	t.Cleanup(p.Close)
	proj, err := p.Open(dir)
	must(t, err)
	t.Cleanup(func() {
		if proj.Files != nil {
			proj.Files.Close()
		}
	})
	rows := proj.Tree.Rows()
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	if !slices.Contains(names, "b.txt") {
		t.Fatalf("rows after Open = %q, want the folder's entries", names)
	}
	if len(rows) == 0 || !rows[0].Expanded {
		t.Fatal("the root row is not expanded after Open")
	}
}
