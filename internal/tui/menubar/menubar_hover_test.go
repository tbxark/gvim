package menubar

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func motion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y})
}

// rowY is the screen row of dropdown row i.
func rowY(b dropBox, i int) int { return b.top + 1 + i - b.first }

// rowIndex returns the index of the row with the given label.
func rowIndex(t *testing.T, rows []Row, label string) int {
	t.Helper()
	for i, r := range rows {
		if r.Label == label {
			return i
		}
	}
	t.Fatalf("no row %q", label)
	return -1
}

// firstSeparator returns the index of the first separator row.
func firstSeparator(rows []Row) int {
	for i, r := range rows {
		if r.Separator {
			return i
		}
	}
	return -1
}

// TestHoverOnANonSelectableAreaClearsTheHighlight drives the pointer onto every
// kind of area that is not a selectable row. The row that was highlighted must not
// stay highlighted, and the dropdown stays open.
func TestHoverOnANonSelectableAreaClearsTheHighlight(t *testing.T) {
	ports := allOn()
	ports.on["can_save"] = false // File > Save is disabled
	open := func() Model { return newBar(ports, 80, 30).openTitle("File") }
	m0 := open()
	box := m0.geometry(80, 30).box
	items := m0.Rows()
	sep := firstSeparator(items)
	disabled := rowIndex(t, items, "Save")
	cases := []struct {
		name string
		x, y int
	}{
		{"separator", box.x + 3, rowY(box, sep)},
		{"disabled row", box.x + 3, rowY(box, disabled)},
		{"top border", box.x + 3, box.top},
		{"bottom border", box.x + 3, box.bottom},
		{"left border", box.x, rowY(box, 1)},
		{"right border", box.x + box.w - 1, rowY(box, 1)},
		{"right of the dropdown", box.x + box.w + 5, rowY(box, 1)},
		{"below the dropdown", box.x + 3, box.bottom + 2},
		{"bar blank space", 70, 0},
		{"editor area", 5, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := send(open(), motion(box.x+3, rowY(box, 1)))
			if m.Selected() != 1 {
				t.Fatalf("setup: highlight %d, want 1", m.Selected())
			}
			m, _ = send(m, motion(c.x, c.y))
			if m.Selected() != -1 {
				t.Errorf("highlight stays on row %d after the pointer moved to %s", m.Selected(), c.name)
			}
			if !m.IsOpen() || m.Title() != "File" {
				t.Errorf("the dropdown closed or switched: open %v, title %q", m.IsOpen(), m.Title())
			}
			// Back on a row: it highlights again.
			m, _ = send(m, motion(box.x+3, rowY(box, 1)))
			if m.Selected() != 1 {
				t.Errorf("highlight %d after returning to a row, want 1", m.Selected())
			}
		})
	}
}

func TestHoverOnTheOpenLabelClearsTheRowHighlight(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	if m.Selected() < 0 {
		t.Fatal("setup: a freshly opened dropdown highlights its first row")
	}
	m, _ = send(m, motion(labelRuns(false)[0].x, 0))
	if m.Selected() != -1 || m.Title() != "File" {
		t.Errorf("highlight %d, title %q", m.Selected(), m.Title())
	}
}

// TestHoverSwitchingLabelsHighlightsTheFirstRow follows Fresh: hovering another
// label opens its dropdown through open_menu, which highlights the first selectable
// row.
func TestHoverSwitchingLabelsHighlightsTheFirstRow(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	m, _ = send(m, motion(labelRuns(false)[1].x, 0))
	if m.Title() != "Edit" || m.Selected() != 0 || m.Rows()[0].Label != "Undo" {
		t.Errorf("title %q, highlight %d: want Edit with its first row Undo highlighted", m.Title(), m.Selected())
	}
}

// TestEnterAfterALabelHoverRunsTheFirstRow checks that the row the label hover
// highlighted is the one Enter runs.
func TestEnterAfterALabelHoverRunsTheFirstRow(t *testing.T) {
	ports := allOn()
	m := newBar(ports, 80, 30).Activate()
	m, _ = send(m, motion(labelRuns(false)[1].x, 0))
	m, _ = send(m, keyMsg("enter"))
	if m.IsOpen() {
		t.Error("Enter left the dropdown open")
	}
	if len(ports.ran) != 1 || ports.ran[0] != "undo" {
		t.Errorf("Enter ran %v, want [undo]", ports.ran)
	}
}

// TestHoverOnTheOpenLabelHighlightsNoRow keeps the pointer on the bar from leaving a
// row highlighted.
func TestHoverOnTheOpenLabelHighlightsNoRow(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	m, _ = send(m, motion(labelRuns(false)[0].x, 0))
	if m.Selected() != -1 || m.Title() != "File" {
		t.Errorf("title %q, highlight %d: the pointer is on the label, not on a row", m.Title(), m.Selected())
	}
}

// TestHoverAcrossADropdownBorderKeepsTheSubmenu moves the pointer over the borders
// and separators of the dropdown while its submenu is open, on rows other than the
// submenu's parent. The submenu stays open and no row is highlighted, as Fresh keeps
// submenu_path when the hover target is a border or a separator.
func TestHoverAcrossADropdownBorderKeepsTheSubmenu(t *testing.T) {
	open := func() (Model, dropBox, int) {
		m := newBar(allOn(), 80, 40).openTitle("View")
		box := m.geometry(80, 40).box
		i := rowIndex(t, m.Rows(), "Terminal")
		m, _ = send(m, motion(box.x+3, rowY(box, i)))
		if !m.SubOpen() {
			t.Fatal("setup: hovering the Terminal row opens its submenu")
		}
		return m, box, i
	}
	_, box, i := open()
	cases := []struct {
		name string
		x, y int
	}{
		{"separator above the parent", box.x + 3, rowY(box, i-1)},
		{"right border on the row above", box.x + box.w - 1, rowY(box, i-1)},
		{"left border on the row below", box.x, rowY(box, i+1)},
		{"top border", box.x + 3, box.top},
		{"bottom border", box.x + 3, box.bottom},
		{"bar blank space", 70, 0},
		{"editor area", 5, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := open()
			m, _ = send(m, motion(c.x, c.y))
			if !m.SubOpen() {
				t.Errorf("the submenu closed on the %s", c.name)
			}
			if m.Selected() != -1 {
				t.Errorf("row %d stays highlighted on the %s", m.Selected(), c.name)
			}
			if !m.IsOpen() || m.Title() != "View" {
				t.Errorf("the dropdown closed or switched: open %v, title %q", m.IsOpen(), m.Title())
			}
		})
	}
	t.Run("the submenu is still reachable", func(t *testing.T) {
		m, box, i := open()
		m, _ = send(m, motion(box.x+box.w-1, rowY(box, i-1)))
		g := m.geometry(80, 40)
		m, _ = send(m, motion(g.sub.x+3, rowY(g.sub, 1)))
		if m.SubSelected() != 1 || !m.SubOpen() {
			t.Errorf("submenu row 1: highlight %d, open %v", m.SubSelected(), m.SubOpen())
		}
	})
}

func TestKeyboardResumesFromAClearedHighlight(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	m, _ = send(m, motion(70, 0))
	m, _ = send(m, keyMsg("down"))
	if m.Selected() != 0 {
		t.Errorf("Down from no highlight chose %d, want the first row", m.Selected())
	}
	m, _ = send(m, motion(70, 0))
	m, _ = send(m, keyMsg("up"))
	rows := m.Rows()
	if m.Selected() != len(rows)-1 {
		t.Errorf("Up from no highlight chose %d, want the last row %d", m.Selected(), len(rows)-1)
	}
}

func TestHoverInTheSubmenu(t *testing.T) {
	m := selectRow(t, newBar(allOn(), 80, 30).openTitle("View"), "Terminal")
	m, _ = send(m, keyMsg("right"))
	g := m.geometry(80, 30)
	parent := m.Selected()
	sepIdx := firstSeparator(m.SubRows())
	m, _ = send(m, motion(g.sub.x+3, rowY(g.sub, 1)))
	if m.SubSelected() != 1 {
		t.Fatalf("hover on submenu row 1 highlighted %d", m.SubSelected())
	}
	for name, p := range map[string][2]int{
		"submenu separator":   {g.sub.x + 3, rowY(g.sub, sepIdx)},
		"submenu top border":  {g.sub.x + 3, g.sub.top},
		"submenu right edge":  {g.sub.x + g.sub.w - 1, rowY(g.sub, 1)},
		"submenu bottom edge": {g.sub.x + 3, g.sub.bottom},
	} {
		m2, _ := send(m, motion(p[0], p[1]))
		if m2.SubSelected() != -1 {
			t.Errorf("%s: submenu highlight %d stays", name, m2.SubSelected())
		}
		if !m2.SubOpen() || m2.Selected() != parent {
			t.Errorf("%s: the submenu closed or the parent lost its highlight (%d)", name, m2.Selected())
		}
	}
	m2, _ := send(m, motion(g.sub.x+g.sub.w+2, g.sub.top+2))
	if m2.Selected() != -1 || m2.SubSelected() != -1 {
		t.Errorf("outside: highlight %d / %d stays", m2.Selected(), m2.SubSelected())
	}
	if !m2.IsOpen() {
		t.Error("the menu closed")
	}
}

func TestHoverOpensASubmenuAsFreshDoes(t *testing.T) {
	m := newBar(allOn(), 80, 50).openTitle("View")
	box := m.geometry(80, 50).box
	i := rowIndex(t, m.Rows(), "Terminal")
	m, _ = send(m, motion(box.x+3, rowY(box, i)))
	if !m.SubOpen() || m.Selected() != i {
		t.Fatalf("hover on a submenu row: open %v, highlight %d", m.SubOpen(), m.Selected())
	}
	if m.SubSelected() != -1 {
		t.Errorf("no child is under the pointer, but %d is highlighted", m.SubSelected())
	}
	// Moving to another row of the dropdown closes it again.
	m, _ = send(m, motion(box.x+3, rowY(box, 0)))
	if m.SubOpen() {
		t.Error("the submenu stays open on another row")
	}
}

func TestHitSeparatesTheBordersOfTheSubmenu(t *testing.T) {
	m := selectRow(t, newBar(allOn(), 80, 30).openTitle("View"), "Terminal")
	m, _ = send(m, keyMsg("right"))
	g := m.geometry(80, 30)
	if got := m.Hit(g.sub.x+3, g.sub.top); got.Kind != TargetSubBorder {
		t.Errorf("submenu top border = %+v", got)
	}
	if got := m.Hit(g.box.x+2, g.box.top); got.Kind != TargetBorder {
		t.Errorf("dropdown top border = %+v", got)
	}
}
