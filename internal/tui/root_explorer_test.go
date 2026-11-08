package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// hiddenTree is a fake tree that keeps its show-hidden setting and counts its reloads.
type hiddenTree struct {
	*fakeTree
	hidden  bool
	reloads int
}

func (t *hiddenTree) ShowHidden() bool     { return t.hidden }
func (t *hiddenTree) SetShowHidden(v bool) { t.hidden = v }
func (t *hiddenTree) Reload() error        { t.reloads++; return nil }

// TestToggleHiddenFilesFlipsTheSettingAndRescans checks that File Explorer: Toggle
// Hidden Files flips the tree's show-hidden setting both ways, and reloads the tree
// through the same reload the file watcher uses.
func TestToggleHiddenFilesFlipsTheSettingAndRescans(t *testing.T) {
	t.Parallel()
	tree := &hiddenTree{fakeTree: newFakeTree(2)}
	m := newTestModel(t, newFakeEditor("x"), tree, 80, 24)

	rootPaletteRun(m, "file_explorer_toggle_hidden")
	if !tree.hidden || tree.reloads != 1 {
		t.Fatalf("after one toggle: hidden = %v, reloads = %d, want true and 1", tree.hidden, tree.reloads)
	}
	if m.notice != "Hidden files: shown" {
		t.Fatalf("notice = %q, want the shown notice", m.notice)
	}

	rootPaletteRun(m, "file_explorer_toggle_hidden")
	if tree.hidden || tree.reloads != 2 {
		t.Fatalf("after two toggles: hidden = %v, reloads = %d, want false and 2", tree.hidden, tree.reloads)
	}
	if m.notice != "Hidden files: hidden" {
		t.Fatalf("notice = %q, want the hidden notice", m.notice)
	}
}

// TestToggleHiddenFilesWithoutExplorerSaysSo checks that the toggle with no tree
// changes nothing and says there is no explorer.
func TestToggleHiddenFilesWithoutExplorerSaysSo(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("x"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rootPaletteRun(m, "file_explorer_toggle_hidden")
	if m.notice != "No file explorer" {
		t.Fatalf("notice = %q, want the no-explorer notice", m.notice)
	}
}
