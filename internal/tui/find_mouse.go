package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/find"
)

// topFindBar returns the find bar when it is the top overlay, or nil.
func (m *Model) topFindBar() *find.Bar {
	if n := len(m.overlays); n > 0 {
		if o, ok := m.overlays[n-1].(*find.Bar); ok {
			return o
		}
	}
	return nil
}

// mouseOf returns the pointer of a mouse message, and false for any other message.
func mouseOf(msg tea.Msg) (tea.Mouse, bool) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return tea.Mouse(msg), true
	case tea.MouseMotionMsg:
		return tea.Mouse(msg), true
	case tea.MouseReleaseMsg:
		return tea.Mouse(msg), true
	case tea.MouseWheelMsg:
		return tea.Mouse(msg), true
	}
	return tea.Mouse{}, false
}

// findMouse handles a mouse message while the find bar is on top. The bar is not
// modal for the mouse, as Fresh's bottom prompt is not: a press on its rows toggles an
// option or moves the keys, and the root takes the message when it is above the bar, so
// a click in the text moves the caret and the wheel scrolls. It reports whether the
// bar took the message.
func (m *Model) findMouse(msg tea.Msg) bool {
	bar := m.topFindBar()
	mo, ok := mouseOf(msg)
	if bar == nil || !ok {
		return false
	}
	if !bar.Covers(mo.Y, m.height) {
		return false
	}
	if c, ok := msg.(tea.MouseClickMsg); ok && c.Button == tea.MouseLeft {
		bar.Click(mo.X, mo.Y, m.width, m.height)
	}
	return true
}

// rebaseFind tells the open find bar that a press in the text moved the caret, so Esc
// does not put the caret back where the bar opened.
func (m *Model) rebaseFind() {
	if bar := m.topFindBar(); bar != nil {
		bar.Rebase()
	}
}
