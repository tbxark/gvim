package menubar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// wheelRows is the number of rows a wheel notch scrolls a dropdown.
const wheelRows = 2

// hover follows the pointer while a dropdown is open. The highlight is where the
// pointer is: a selectable row under it is highlighted. Any other place leaves no
// row highlighted, so a row does not look stuck after the pointer left it: a
// separator, a border, the bar outside the open label, or outside the menu. The
// menu stays open. A label that is not the open one switches to its dropdown with
// its first row highlighted, and a submenu row opens its submenu, as Fresh does
// (menu_hover_reaction). Fresh keeps the submenu open when the target is a border,
// a separator or no target at all, so those keep it too. A disabled row, another
// row or another label closes it.
func (m Model) hover(mo tea.Mouse) Model {
	t := m.Hit(mo.X, mo.Y)
	switch t.Kind {
	case TargetLabel, TargetMore:
		if title := m.titleOf(t); title != m.title {
			return m.openTitle(title)
		}
		m.selected = -1
		m.sub.selected = -1
		return m
	case TargetItem:
		row := m.rows[t.Index]
		if !row.selectable() {
			return m.clearHighlight()
		}
		if m.sub.open && m.sub.parent == t.Index {
			m.selected = t.Index
			m.sub.selected = -1
			return m
		}
		m.sub = subMenu{}
		m.selected = t.Index
		if row.Submenu && len(row.children) > 0 {
			m.sub = subMenu{open: true, parent: t.Index, selected: -1}
		}
		return m.reveal()
	case TargetSubItem:
		m.selected = m.sub.parent
		if m.rows[m.sub.parent].children[t.Index].selectable() {
			m.sub.selected = t.Index
		} else {
			m.sub.selected = -1
		}
	case TargetSubBorder:
		m.selected = m.sub.parent
		m.sub.selected = -1
	default:
		// A border, a separator, the bar outside the open label, or outside the menu:
		// no row of either list is under the pointer.
		m.selected = -1
		m.sub.selected = -1
	}
	return m
}

// clearHighlight removes the highlight of every row and closes the submenu that
// hung from the highlighted row. The dropdown stays open.
func (m Model) clearHighlight() Model {
	m.selected = -1
	m.sub = subMenu{}
	return m
}

// stepFrom is stepIn that also starts from no highlight: Down chooses the first
// selectable row and Up the last one.
func stepFrom(rows []Row, cur, dir int) int {
	if cur >= 0 || dir > 0 {
		return stepIn(rows, cur, dir)
	}
	if i := stepIn(rows, len(rows), dir); i < len(rows) {
		return i
	}
	return -1
}

// wheel scrolls the dropdown or the submenu under the pointer by one notch. A
// highlighted row that the scroll takes out of the window loses its highlight, so
// the window is not pulled back to it.
func (m Model) wheel(mo tea.Mouse) Model {
	d := 0
	switch mo.Button {
	case tea.MouseWheelUp:
		d = -wheelRows
	case tea.MouseWheelDown:
		d = wheelRows
	default:
		return m
	}
	g := m.geometry(m.w, m.h)
	switch m.Hit(mo.X, mo.Y).Kind {
	case TargetItem, TargetBorder:
		m.scroll = min(max(g.box.first+d, 0), max(len(g.box.rows)-g.box.visible(), 0))
		if m.selected < m.scroll || m.selected >= m.scroll+g.box.visible() {
			m = m.clearHighlight()
		}
	case TargetSubItem, TargetSubBorder:
		m.sub.scroll = min(max(g.sub.first+d, 0), max(len(g.sub.rows)-g.sub.visible(), 0))
		if m.sub.selected < m.sub.scroll || m.sub.selected >= m.sub.scroll+g.sub.visible() {
			m.sub.selected = -1
		}
	}
	return m
}

// paintMore marks a dropdown that has rows above or below its window: an up
// marker in the middle of the top border and a down marker in the bottom one.
func paintMore(g *grid.Grid, box dropBox, border grid.Style) {
	mid := box.x + box.w/2
	if box.first > 0 {
		g.Set(mid, box.top, "▲", border)
	}
	if box.first+box.visible() < len(box.rows) {
		g.Set(mid, box.bottom, "▼", border)
	}
}
