package dialog

import (
	"github.com/tbxark/gvim/internal/ui/grid"
)

// listSize is the number of recent entries shown at once.
const listSize = 6

// recentList is the list of recent folders in Open Folder. The selection moves
// with Up and Down and stays inside the visible window. It is only drawn when
// the dialog has a field above it.
type recentList struct {
	items []string
	sel   int
	top   int    // the first visible entry
	rects []rect // one per visible row of the last draw, top to bottom
}

// newRecentList returns a list of items with the first item selected.
func newRecentList(items []string) *recentList {
	return &recentList{items: items}
}

// move shifts the selection by d entries, clamped to the list.
func (l *recentList) move(d int) {
	if len(l.items) == 0 {
		return
	}
	l.sel = min(max(l.sel+d, 0), len(l.items)-1)
	l.keepVisible()
}

// keepVisible scrolls the window so that the selection is in it.
func (l *recentList) keepVisible() {
	l.top = min(l.top, l.sel)
	if l.sel >= l.top+listSize {
		l.top = l.sel - listSize + 1
	}
	l.top = max(l.top, 0)
}

// selected returns the selected item, or "" for an empty list.
func (l *recentList) selected() string {
	if len(l.items) == 0 {
		return ""
	}
	return l.items[l.sel]
}

// rowAt returns the visible row at (x, y) in the last draw, or -1.
func (l *recentList) rowAt(x, y int) int {
	for k, r := range l.rects {
		if r.contains(x, y) {
			return k
		}
	}
	return -1
}

// listRows returns the rows of the recent list: a heading, then the visible
// entries, or a note when there are none. It runs during the frame, before the
// rows are painted, so it resets the row rectangles of the last draw here.
func (m *modal) listRows() []row {
	l := m.list
	l.keepVisible()
	l.rects = l.rects[:0]
	rows := []row{textRow("Recent folders", stMuted)}
	if len(l.items) == 0 {
		return append(rows, textRow("No recent folders", stMuted))
	}
	for k := range min(listSize, len(l.items)-l.top) {
		idx := l.top + k
		rows = append(rows, func(g *grid.Grid, x, y, w int) {
			st := stRowPlain
			if idx == l.sel {
				st = stRowIdle
				if m.listFocused() {
					st = stRowFocus
				}
				g.Fill(x, x+w, y, st)
			}
			g.Put(x+1, y, x+w, l.items[idx], st)
			l.rects = append(l.rects, rect{x, y, w, 1})
		})
	}
	return rows
}

// fieldRow returns a row that paints the text field and records its place and
// the caret.
func (m *modal) fieldRow() row {
	f := m.field
	return func(g *grid.Grid, x, y, w int) {
		m.fieldAt = rect{x, y, w, 1}
		cx, ok := f.draw(g, x, y, w, stField)
		m.caret = point{cx, y}
		m.caretOK = ok
	}
}
