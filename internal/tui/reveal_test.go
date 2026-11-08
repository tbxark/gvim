package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/theme"
)

// revealGuardTree is a real explorer tree whose loading Reveal fails the test. The
// update loop reveals through RevealLoaded, which reads no folder, so a call to
// Reveal from Update would read the disk on the UI goroutine.
type revealGuardTree struct {
	*explorer.Tree
	t *testing.T
}

func (g *revealGuardTree) Reveal(path string) error {
	g.t.Errorf("Reveal(%q) loads folders from disk, so the update loop must not call it", path)
	return g.Tree.Reveal(path)
}

// TestFileOperationRevealLoadsOnlyInTheCommand checks that revealing a file operation's
// new entry does not read folders inside Update. The folder that holds the entry may be
// unloaded, and then its listing comes from the command, so the entry shows only once
// the listing is applied.
func TestFileOperationRevealLoadsOnlyInTheCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		folder string // relative folder that the new file is created in
		expand bool   // the folder is expanded before the operation
	}{
		{name: "an unloaded folder", folder: "sub"},
		{name: "a loaded folder", folder: "sub", expand: true},
		{name: "two unloaded folders", folder: filepath.Join("sub", "deep")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			folder := filepath.Join(dir, tt.folder)
			must(t, os.MkdirAll(folder, 0o755))
			tree, err := explorer.Open(dir)
			must(t, err)
			if tt.expand {
				must(t, tree.Expand(folder))
			}
			m := New(Deps{
				Editor:   newFakeEditor("x"),
				Explorer: &revealGuardTree{Tree: tree, t: t},
				Keys:     testKeys(),
				Theme:    theme.Theme{},
				Clock:    testClock,
			})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

			created := filepath.Join(folder, "new.txt")
			must(t, os.WriteFile(created, nil, 0o644))
			_, cmd := m.Update(fileResultMsg{job: fileJob{kind: jobCreate, dir: folder, name: "new.txt"}, out: created})
			if got := treeRowNames(tree); slices.Contains(got, "new.txt") {
				t.Fatalf("rows after the file operation = %q, want the tree untouched until the listing", got)
			}

			m.Update(listingFrom(t, cmd))
			if row, ok := tree.Selected(); !ok || row.Path != created {
				t.Fatalf("selected = %+v after the listing, want %s", row, created)
			}
		})
	}
}
