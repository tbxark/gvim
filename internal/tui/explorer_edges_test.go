package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHomeAndEndMoveTheExplorerCursorToTheEdges(t *testing.T) {
	t.Parallel()
	m, ed, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt", "gamma.txt")
	press(m, special(tea.KeyEnd))
	last := len(tr.rows) - 1
	if tr.selected != last {
		t.Fatalf("End selected row %d, want the last row %d", tr.selected, last)
	}
	press(m, special(tea.KeyHome))
	if tr.selected != 0 {
		t.Fatalf("Home selected row %d, want 0", tr.selected)
	}
	if len(ed.actions) != 0 {
		t.Errorf("Home and End reached the editor: %v", ed.actions)
	}
}

func TestHomeAndEndWithAModifierAreNotTheExplorerEdges(t *testing.T) {
	t.Parallel()
	m, _, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt")
	press(m, tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if tr.selected != 0 {
		t.Errorf("Ctrl+End moved the explorer cursor to row %d", tr.selected)
	}
}
