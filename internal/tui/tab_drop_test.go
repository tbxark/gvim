package tui

import (
	"testing"
)

func TestDropEdgesFollowFreshsZones(t *testing.T) {
	r := rect{10, 5, 40, 12} // edges: 10 columns, 3 rows
	tests := []struct {
		name string
		x, y int
		want dropEdge
	}{
		{"left edge", 10, 9, dropLeft},
		{"just inside the left edge", 20, 9, dropCenter},
		{"right edge", 40, 9, dropRight},
		{"top edge", 25, 5, dropTop},
		{"bottom edge", 25, 16, dropBottom},
		{"centre", 25, 10, dropCenter},
		{"left wins over top in a corner", 10, 5, dropLeft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, area := dropEdges(r, tt.x, tt.y)
			if got != tt.want {
				t.Errorf("edge at (%d,%d) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
			if !area.contains(tt.x, tt.y) {
				t.Errorf("the tinted area %+v does not hold the pointer (%d,%d)", area, tt.x, tt.y)
			}
		})
	}
}

// dragTabTo presses tab i of the strip and drags to (x, y), without releasing.
func dragTabTo(t *testing.T, m *Model, i, x, y int) {
	t.Helper()
	src := tabRun(t, m, i)
	mouseDownAt(m, src.x+1, m.layout().stripY)
	mouseMove(m, x, y)
}

func TestDroppingATabOnAPaneEdgeSplitsIt(t *testing.T) {
	tests := []struct {
		name        string
		edge        func(p splitPane) (x, y int)
		wantDir     splitDir
		newPaneLast bool // the pane with the dragged tab is the second child
	}{
		{"right", func(p splitPane) (int, int) { return p.x + p.w - 2, p.y + p.h/2 }, sideBySide, true},
		{"left", func(p splitPane) (int, int) { return p.x + 1, p.y + p.h/2 }, sideBySide, false},
		{"bottom", func(p splitPane) (int, int) { return p.x + p.w/2, p.y + p.h - 1 }, stacked, true},
		{"top", func(p splitPane) (int, int) { return p.x + p.w/2, p.y }, stacked, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMultiFake(2, 30)
			m := newSplitModel(t, f, 80, 24)
			p := panesOf(t, m, 1)[0]
			x, y := tt.edge(p)
			dragTabTo(t, m, 1, x, y)
			if z := m.drag.tab.zone; z == nil || z.pane != 0 {
				t.Fatalf("zone = %+v, want one on pane 0", z)
			}
			mouseUpAt(m, x, y)
			root := m.splits.root
			if root.leaf() || root.dir != tt.wantDir {
				t.Fatalf("root leaf %v dir %v, want a split in direction %v", root.leaf(), root.dir, tt.wantDir)
			}
			withTab, other := root.a, root.b
			if tt.newPaneLast {
				withTab, other = root.b, root.a
			}
			if m.splits.focus != withTab {
				t.Error("the focus is not on the new pane")
			}
			if f.active != 1 {
				t.Errorf("active tab %d, want the dragged tab 1", f.active)
			}
			if other.tab != 0 {
				t.Errorf("the old pane shows tab %d, want 0", other.tab)
			}
			if m.drag.tab != nil {
				t.Error("the tab drag outlived the release")
			}
		})
	}
}

func TestDroppingATabInTheCentreShowsItInThatPane(t *testing.T) {
	f := newMultiFake(2, 30)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical") // two panes, both showing tab 0; the right one has the focus
	ps := panesOf(t, m, 2)
	left := ps[0]
	x, y := left.x+left.w/2, left.y+left.h/2
	dragTabTo(t, m, 1, x, y)
	mouseUpAt(m, x, y)
	if got := m.layout().focus; got != 0 {
		t.Errorf("focus %d, want the left pane 0", got)
	}
	if f.active != 1 {
		t.Errorf("active tab %d, want 1", f.active)
	}
	if n := len(m.layout().panes); n != 2 {
		t.Errorf("%d panes after a centre drop, want 2", n)
	}
}

func TestATabDragThatBarelyMovedDoesNotSplit(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	p := panesOf(t, m, 1)[0]
	src := tabRun(t, m, 1)
	y := m.layout().stripY
	mouseDownAt(m, src.x+1, y)
	mouseMove(m, src.x+1, y+1) // one row down, onto the pane's top edge
	mouseUpAt(m, src.x+1, y+1)
	if !m.splits.root.leaf() {
		t.Errorf("a wobble of one cell split the pane %+v", p)
	}
}

func TestDropZoneIsTintedWhileDragging(t *testing.T) {
	m := newSplitModelTheme(t, newMultiFake(2, 30), "dark")
	p := panesOf(t, m, 1)[0]
	x, y := p.x+p.w-2, p.y+p.h/2
	tint := m.styles.Overlay.Selected.BG
	dragTabTo(t, m, 1, x, y)
	g := m.paint()
	if got := g.At(x, y).Style.BG; got != tint {
		t.Errorf("the cell under the pointer has background %v, want the drop tint %v", got, tint)
	}
	if got := g.At(p.x+p.w/2, y).Style.BG; got == tint {
		t.Error("the tint reaches the middle of the pane, which is not in the right edge zone")
	}
	mouseUpAt(m, x, y)
	if got := m.paint().At(x, y).Style.BG; got == tint {
		t.Error("the tint stays after the drop")
	}
}
