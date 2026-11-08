package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// dropEdge is the part of a pane a dragged tab is over, as Fresh's tab drop zones: an
// edge makes a new pane on that side that shows the tab, and the centre shows the tab in
// that pane.
type dropEdge int

const (
	dropNone dropEdge = iota
	dropLeft
	dropRight
	dropTop
	dropBottom
	dropCenter
)

// dropThreshold is how many cells the pointer must travel from the press before a tab
// drag has drop zones, so a click that wobbles does not split a pane.
const dropThreshold = 3

// tabZone is where a dragged tab would land: pane is the index of the pane in reading
// order, and area the part of it to tint.
type tabZone struct {
	pane int
	edge dropEdge
	area rect
}

// dropEdges returns the edge of r that cell (x, y) is on and the part of r it tints.
// Each edge takes a quarter of the extent, at least 3 columns or 2 rows, as Fresh does.
// Edges come before the centre, and the left and right before the top and bottom.
func dropEdges(r rect, x, y int) (dropEdge, rect) {
	tx, ty := max(r.w/4, 3), max(r.h/4, 2)
	rx, ry := x-r.x, y-r.y
	switch {
	case rx < tx:
		return dropLeft, rect{r.x, r.y, min(tx, r.w), r.h}
	case rx >= r.w-tx:
		return dropRight, rect{r.x + max(r.w-tx, 0), r.y, min(tx, r.w), r.h}
	case ry < ty:
		return dropTop, rect{r.x, r.y, r.w, min(ty, r.h)}
	case ry >= r.h-ty:
		return dropBottom, rect{r.x, r.y + max(r.h-ty, 0), r.w, min(ty, r.h)}
	}
	return dropCenter, r
}

// dragTabZone finds the drop zone under the pointer while a tab is dragged off the
// strip. The pointer on the strip, or within dropThreshold of the press, has none.
func (m *Model) dragTabZone(l layout, mo tea.Mouse) {
	td := m.drag.tab
	td.zone = nil
	if abs(mo.X-td.startX) <= dropThreshold && abs(mo.Y-td.startY) <= dropThreshold {
		return
	}
	for i, p := range l.panes {
		if !p.contains(mo.X, mo.Y) {
			continue
		}
		edge, area := dropEdges(p.rect, mo.X, mo.Y)
		if edge == dropCenter && p.tab == td.index {
			return // the tab is already shown in that pane
		}
		td.zone = &tabZone{pane: i, edge: edge, area: area}
		return
	}
}

func abs(n int) int { return max(n, -n) }

// dropTab drops the dragged tab where the release found its zone. An edge splits the pane
// and the new pane, on that side, shows the tab and takes the focus. The centre shows the
// tab in the pane.
func (m *Model) dropTab(td *tabDrag) {
	z := td.zone
	if z == nil || m.ed.Active() == nil {
		return
	}
	m.noteTab()
	defer m.noteTab()
	// The press that began the drag made the tab the active one, so the pane it was
	// pressed in shows it. A split then keeps the tab that pane showed before.
	prev := m.splits.prevTab
	leaves := m.splits.root.leaves()
	pressedIn := z.pane >= 0 && z.pane < len(leaves) && leaves[z.pane] == m.splits.focus
	m.focusPane(z.pane)
	if z.edge != dropCenter {
		dir := sideBySide
		if z.edge == dropTop || z.edge == dropBottom {
			dir = stacked
		}
		if msg := m.trySplit(dir); msg != "" {
			m.notice = msg
			return
		}
		if z.edge == dropLeft || z.edge == dropTop {
			n := m.splits.root.parentOf(m.splits.focus)
			n.a, n.b = n.b, n.a
		}
		if parent := m.splits.root.parentOf(m.splits.focus); pressedIn && parent != nil && prev >= 0 && prev != td.index && prev < len(m.ed.Tabs()) {
			old := parent.a
			if old == m.splits.focus {
				old = parent.b
			}
			old.tab, old.top = prev, 0
		}
	}
	m.focusEditor()
	m.activateTab(td.index)
	m.syncViewport()
}

// parentOf returns the inner node of the tree n whose child is leaf, or nil.
func (n *splitNode) parentOf(leaf *splitNode) *splitNode {
	if n.leaf() {
		return nil
	}
	if n.a == leaf || n.b == leaf {
		return n
	}
	if p := n.a.parentOf(leaf); p != nil {
		return p
	}
	return n.b.parentOf(leaf)
}

// paintDropZone tints the part of the pane the dragged tab would land on, keeping the
// text under it.
func (m *Model) paintDropZone(g *grid.Grid) {
	if m.drag.tab == nil || m.drag.tab.zone == nil {
		return
	}
	a := m.drag.tab.zone.area
	bg := m.styles.Overlay.Selected.BG
	for y := a.y; y < a.y+a.h; y++ {
		for x := a.x; x < a.x+a.w; x++ {
			c := g.At(x, y)
			if c.Text == "" {
				continue
			}
			c.Style.BG = bg
			g.Set(x, y, c.Text, c.Style)
		}
	}
}
