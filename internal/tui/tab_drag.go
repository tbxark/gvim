package tui

import (
	tea "charm.land/bubbletea/v2"
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
	if mo.Y != l.stripY {
		m.dragTabZone(l, mo)
		return
	}
	m.drag.tab.zone = nil
	to := tabAt(l.strip.runs, mo.X)
	from := m.drag.tab.index
	if to < 0 || to == from || !passedMidpoint(l.strip.runs, to, from, mo.X) {
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

// passedMidpoint reports whether the pointer at column x has crossed the middle of
// tab to, coming from tab from. A neighbour that is wider than the dragged tab is
// still under the pointer after the swap, so the swap must not repeat on the next
// cell. The pointer outside the tab (left of the first, right of the last) always
// counts.
func passedMidpoint(runs []run, to, from, x int) bool {
	lo, hi := -1, -1
	for _, r := range runs {
		if (r.kind != hitTab && r.kind != hitTabClose) || r.index != to {
			continue
		}
		if lo < 0 || r.x < lo {
			lo = r.x
		}
		hi = max(hi, r.x+r.w)
	}
	if lo < 0 || x < lo || x >= hi {
		return true
	}
	mid := lo + (hi-lo)/2
	if to > from {
		return x >= mid
	}
	return x < mid
}

// tabAt is the index of the tab under column x of the strip: the tab whose label or
// close button covers it, the first tab for a column left of the tabs, and the last
// for a column right of them. It is -1 when the strip shows no tab.
func tabAt(runs []run, x int) int {
	first, last := -1, -1
	for _, r := range runs {
		if r.kind != hitTab && r.kind != hitTabClose {
			continue
		}
		if r.covers(x) {
			return r.index
		}
		if first < 0 || r.index < first {
			first = r.index
		}
		last = max(last, r.index)
	}
	if first < 0 {
		return -1
	}
	for _, r := range runs {
		if r.kind == hitTab && r.index == first && x < r.x {
			return first
		}
	}
	return last
}
