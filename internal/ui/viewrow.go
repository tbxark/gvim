package ui

// rowTop is the rows of the top line that the focused pane has scrolled off, which the
// editor does not know: it counts buffer lines. A wrapped line taller than the body is
// scrolled through by rows, and the view then starts below the first row of its top line. The
// number is the pane's to read: for a line too long to count its rows it is a rune column.
// The record holds for the tab and the top line it was made for, so a scroll or a jump
// that moves the top line (the editor moves it as it likes) starts at the first row again.
type rowTop struct{ tab, top, row int }

// topRow returns the rows of the top line, top, that the active document has scrolled off.
func (m *Model) topRow(top int) int {
	if r := m.rowTop; r.row > 0 && r.tab == m.activeTab() && r.top == top {
		return r.row
	}
	return 0
}

// setTopRow records that the active document's view starts row rows into its top line.
func (m *Model) setTopRow(row int) {
	m.rowTop = rowTop{tab: m.activeTab(), top: m.viewTop(), row: row}
}

// scrollFocused scrolls the focused pane by delta display rows with the wheel. The
// caret stays where it is.
func (m *Model) scrollFocused(delta int) {
	doc := m.ed.Active()
	l := m.layout()
	if doc == nil || l.gated {
		m.ed.ScrollBy(delta)
		return
	}
	fp := l.focusedPane()
	top, row := m.paneGeometry(fp, doc).ScrollView(fp.w, fp.h, delta)
	m.ed.ScrollTo(top)
	m.setTopRow(row)
}
