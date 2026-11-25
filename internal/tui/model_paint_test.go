package tui

import "github.com/tbxark/gvim/internal/tui/grid"

// paint draws the frame into a new grid. The editor pane fills the body to the
// right of the explorer. Each other band is painted from the layout runs, so the
// hit map and the paint cannot drift apart. The open overlays are drawn last.
func (m *Model) paint() *grid.Grid {
	g := grid.New(m.width, m.height)
	m.paintInto(g)
	return g
}
