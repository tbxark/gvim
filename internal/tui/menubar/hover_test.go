package menubar

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHoverHighlightsSelectableRow(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	m, _ = send(m, tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 3})) // Open File...
	if m.Selected() != 1 {
		t.Fatalf("hover on row 1 selected %d", m.Selected())
	}
	m, _ = send(m, tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 4})) // separator
	if m.Selected() != -1 {
		t.Fatalf("hover on a separator left row %d highlighted", m.Selected())
	}
	off := &fakePorts{on: map[string]bool{}}
	d := newBar(off, 60, 30).Activate()
	d, _ = send(d, tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 5})) // Save, disabled
	if d.Selected() == 3 {
		t.Fatal("hover highlighted a disabled row")
	}
}

func TestHoverOverLabelSwitchesTheOpenDropdown(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	hover := tea.MouseMotionMsg(tea.Mouse{X: labelRuns(false)[1].x, Y: 0})
	m, _ = send(m, hover)
	if m.Title() != "Edit" {
		t.Fatalf("hover over Edit opened %q", m.Title())
	}
	m, _ = send(m, hover)
	if m.Title() != "Edit" || m.Selected() != -1 {
		t.Fatalf("second hover on the same label: dropdown %q, highlight %d", m.Title(), m.Selected())
	}
}
