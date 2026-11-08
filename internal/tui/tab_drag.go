package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/tabstrip"
)

// tabMover is the optional editor port that moves a tab. The app adapter implements it.
// Without it a drag of a tab says that moving tabs is not supported.
type tabMover interface {
	// MoveTabTo moves the tab at index from to index to, the others shifting by one.
	MoveTabTo(from, to int) error
}

// tabDrag is the drag of a tab along the strip: the tab follows the pointer, as in
// Fresh, and drops where the pointer is released. index is where the dragged tab is
// now.
type tabDrag struct {
	index          int
	startX, startY int      // where the press was
	noted          bool     // the status row already said that tabs cannot be moved
	zone           *tabZone // where the tab would land if dropped on a pane, or nil
}

// dragTab moves the dragged tab to the tab under the pointer. The pointer left of the
// first tab means the first, and right of the last tab (over the "+" or the blank)
// the last. While the pointer is off the strip row nothing moves, and over a pane it picks
// the drop zone (see tab_drop.go).
func (m *Model) dragTab(mo tea.Mouse) {
	l := m.layout()
	if mo.Y != l.StripY {
		m.dragTabZone(l, mo)
		return
	}
	m.drag.tab.zone = nil
	to := tabstrip.TabAt(l.Strip.Runs, mo.X)
	from := m.drag.tab.index
	if to < 0 || to == from || !tabstrip.PassedMidpoint(l.Strip.Runs, to, from, mo.X) {
		return
	}
	mv, ok := m.ed.(tabMover)
	if !ok {
		if !m.drag.tab.noted {
			m.notice = "Moving tabs is not supported in this port"
			m.drag.tab.noted = true
		}
		return
	}
	if err := mv.MoveTabTo(from, to); err != nil {
		m.notice = err.Error()
		return
	}
	m.tabMoved(from, to)
	m.drag.tab.index = to
}
