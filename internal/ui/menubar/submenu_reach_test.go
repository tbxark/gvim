package menubar

import "testing"

// TestSlidingIntoASubmenuCellByCellKeepsItOpen moves the pointer one cell at a time
// along the parent row, over the border column that separates the dropdown from
// its submenu, and onto a submenu row.
func TestSlidingIntoASubmenuCellByCellKeepsItOpen(t *testing.T) {
	m := newBar(allOn(), 80, 50).openTitle("View")
	box := m.geometry(80, 50).box
	y := rowY(box, rowIndex(t, m.Rows(), "Terminal"))
	m, _ = send(m, motion(box.x+3, y))
	if !m.SubOpen() {
		t.Fatal("setup: hovering the Terminal row opens its submenu")
	}
	sub := m.geometry(80, 50).sub
	for x := box.x + 4; x < sub.x+3; x++ {
		m, _ = send(m, motion(x, y))
		if !m.SubOpen() {
			t.Fatalf("the submenu closed with the pointer at column %d (%+v)", x, m.Hit(x, y))
		}
	}
	m, _ = send(m, motion(sub.x+3, rowY(sub, 1)))
	if m.SubSelected() != 1 || m.Selected() != rowIndex(t, m.Rows(), "Terminal") {
		t.Errorf("on the submenu row: child %d, parent %d", m.SubSelected(), m.Selected())
	}
}

func TestHoverOnTheDropdownBorderBesideTheSubmenuClearsOnlyTheRowHighlight(t *testing.T) {
	m := newBar(allOn(), 80, 50).openTitle("View")
	box := m.geometry(80, 50).box
	y := rowY(box, rowIndex(t, m.Rows(), "Terminal"))
	m, _ = send(m, motion(box.x+3, y))
	m, _ = send(m, motion(box.x+box.w-1, y))
	if m.Selected() != -1 || !m.SubOpen() {
		t.Errorf("on the border: highlight %d, submenu open %v", m.Selected(), m.SubOpen())
	}
	// A different row closes it.
	m, _ = send(m, motion(box.x+3, rowY(box, 0)))
	if m.SubOpen() {
		t.Error("the submenu stays open on another row")
	}
}

// TestReturningToTheParentRowClearsTheChildHighlight keeps two rows from looking
// highlighted at once.
func TestReturningToTheParentRowClearsTheChildHighlight(t *testing.T) {
	m := newBar(allOn(), 80, 50).openTitle("View")
	box := m.geometry(80, 50).box
	y := rowY(box, rowIndex(t, m.Rows(), "Terminal"))
	m, _ = send(m, motion(box.x+3, y))
	sub := m.geometry(80, 50).sub
	m, _ = send(m, motion(sub.x+3, rowY(sub, 1)))
	if m.SubSelected() != 1 {
		t.Fatalf("setup: child %d", m.SubSelected())
	}
	m, _ = send(m, motion(box.x+3, y))
	if m.SubSelected() != -1 || !m.SubOpen() || m.Selected() != rowIndex(t, m.Rows(), "Terminal") {
		t.Errorf("child %d, open %v, parent %d", m.SubSelected(), m.SubOpen(), m.Selected())
	}
}

// TestSubmenuDoesNotCoverItsParentLabelOnNarrowFrames covers frames where the
// submenu fits on neither side of the dropdown.
func TestSubmenuDoesNotCoverItsParentLabelOnNarrowFrames(t *testing.T) {
	for _, label := range []string{"Terminal", "Keybinding Style"} {
		for _, size := range [][2]int{{60, 40}, {60, 20}, {50, 24}} {
			w, h := size[0], size[1]
			m := newBar(allOn(), w, h).openTitle("View")
			i := rowIndex(t, m.Rows(), label)
			m, _ = send(m, keyMsg("down"))
			for m.Selected() != i {
				m, _ = send(m, keyMsg("down"))
			}
			m, _ = send(m, keyMsg("right"))
			g := m.geometry(w, h)
			if !g.hasSub {
				t.Errorf("%s at %dx%d: no submenu", label, w, h)
				continue
			}
			y := rowY(g.box, i)
			for x := g.box.x + 2; x < g.box.x+2+len(label); x++ {
				if got := m.Hit(x, y); got.Kind != TargetItem || got.Index != i {
					t.Errorf("%s at %dx%d: the label cell (%d,%d) is %+v, covered by the submenu", label, w, h, x, y, got)
					break
				}
			}
		}
	}
}
