package dock

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// hoverState is the Ctrl+hover link highlight: the token under the pointer, its
// rune span on one row, and whether the row is a live screen row or an absolute
// combined row of the scrollback view.
type hoverState struct {
	on     bool
	scroll bool
	row    int
	start  int
	end    int
	token  string
}

// ClearHover drops the link hover highlight. The root clears it when the pointer
// leaves the dock's rows, and the panel clears it on any other mouse event, a
// view change and losing the focus.
func (p *Panel) ClearHover() { p.hover = hoverState{} }

// updateHover computes the Ctrl+hover link at body cell x,row of the visible
// output, with no IO: the path is resolved only when the link is clicked.
func (p *Panel) updateHover(mo tea.Mouse, row int) {
	if p.altScreen() {
		p.ClearHover()
		return
	}
	abs := row
	if p.scroll.on {
		abs = p.scroll.top + row
	}
	text, ok := p.visibleRowText(row)
	if !ok {
		p.ClearHover()
		return
	}
	token, start, end, ok := linkTokenAt([]rune(text), mo.X)
	if !ok {
		p.ClearHover()
		return
	}
	if p.hover.on && p.hover.scroll == p.scroll.on && p.hover.row == abs && p.hover.token == token {
		return
	}
	p.hover = hoverState{
		on: true, scroll: p.scroll.on, row: abs,
		start: start, end: end, token: token,
	}
	if p.host.Notice != nil {
		p.host.Notice("Ctrl+Click to open " + token)
	}
}

// paintHoverRow underlines the hover link's cells on one painted row. row is a
// live screen row or an absolute combined row, as scroll says.
func (p *Panel) paintHoverRow(g *grid.Grid, y, row, w int, scroll bool) {
	if !p.hover.on || p.hover.scroll != scroll || p.hover.row != row {
		return
	}
	for c := max(p.hover.start, 0); c < min(p.hover.end, w); c++ {
		cell := g.At(c, y)
		st := cell.Style
		st.Underline = true
		g.Set(c, y, cell.Text, st)
	}
}
