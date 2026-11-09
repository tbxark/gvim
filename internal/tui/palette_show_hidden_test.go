package tui

import (
	"testing"

	"github.com/tbxark/gvim/theme"
)

// paletteHiddenIndex records the Show Hidden Files setting the palette ports to the
// local literal-path probe.
type paletteHiddenIndex struct {
	*fakeIndex
	hidden bool
	sets   int
}

func (f *paletteHiddenIndex) SetProbeShowHidden(show bool) {
	f.hidden = show
	f.sets++
}

// TestOpenPalettePassesTheExplorerShowHiddenSetting: opening the palette forwards the
// explorer tree's Show Hidden Files setting to the local probe port, so the directory
// listing matches Fresh's FileProvider::set_visibility.
func TestOpenPalettePassesTheExplorerShowHiddenSetting(t *testing.T) {
	idx := &paletteHiddenIndex{fakeIndex: &fakeIndex{}}
	tree := &hiddenTree{fakeTree: newFakeTree(1), hidden: true}
	m := New(Deps{Editor: newFakeEditor("x"), Explorer: tree, Files: idx, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.openPalette("")
	if idx.sets != 1 || !idx.hidden {
		t.Fatalf("show-hidden forwarded = %v after %d sets, want true after one", idx.hidden, idx.sets)
	}

	defaults := &paletteHiddenIndex{fakeIndex: &fakeIndex{}}
	m2 := New(Deps{Editor: newFakeEditor("x"), Explorer: &hiddenTree{fakeTree: newFakeTree(1)}, Files: defaults, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m2.openPalette("")
	if defaults.sets != 1 || defaults.hidden {
		t.Fatalf("default show-hidden forwarded = %v after %d sets, want false", defaults.hidden, defaults.sets)
	}
}
