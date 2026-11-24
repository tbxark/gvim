package dialog

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// boxRows is the chrome the box adds to the body rows: the top border with the
// title, the blank row above the buttons, the button row and the bottom border.
// gapButtons is the gap between two buttons.
const (
	boxRows    = 4
	gapButtons = 2
)

// rect is a cell rectangle, the shared geometry type of space.Rect.
type rect = space.Rect

// point is a cell position.
type point struct{ x, y int }

// row paints one line of a dialog body at (x, y) across w cells.
type row func(g *grid.Grid, x, y, w int)

// textRow returns a row that paints s in style st.
func textRow(s string, st grid.Style) row {
	return func(g *grid.Grid, x, y, w int) {
		g.Put(x, y, x+w, s, st)
	}
}

// textRows returns one row per wrapped line of text, at width cw.
func textRows(text string, cw int, st grid.Style) []row {
	var rows []row
	for _, line := range space.Wrap(text, cw, 1<<10) {
		rows = append(rows, textRow(line, st))
	}
	return rows
}

// natural returns the width the dialog wants: the title, the buttons, and the
// widest of texts.
func (m *modal) natural(texts ...string) int {
	n := max(ansi.StringWidth(m.title)+space.CardChrome, m.buttonsWidth())
	for _, t := range texts {
		n = max(n, ansi.StringWidth(t))
	}
	return n
}

// frame paints the box for a dialog body. natural is the width the body wants.
// body builds the rows at the content width it is given. The body starts directly
// under the top border, and the buttons sit under it after one blank row, on the last
// row inside the border. Hit regions and the caret are recorded for the next event.
func (m *modal) frame(g *grid.Grid, natural int, body func(cw int) []row) {
	cw := space.CardWidth(g.Width(), natural)
	rows := body(cw)
	m.box = space.CardBox(g.Width(), g.Height(), cw, len(rows)+boxRows-2)
	m.caretOK = false
	m.hits = m.hits[:0]
	m.ov.PaintBox(g, m.box.X, m.box.Y, m.box.W, m.box.H, m.title)
	for i, r := range rows {
		r(g, m.box.X+2, m.box.Y+1+i, cw)
	}
	m.drawButtons(g, m.box.X+2, m.box.Y+len(rows)+2, cw)
}

// drawText paints a dialog whose body is plain text.
func (m *modal) drawText(g *grid.Grid, text string) {
	m.frame(g, m.natural(text), func(cw int) []row {
		return textRows(text, cw, m.ov.Box)
	})
}

// buttonsWidth is the width of the button row.
func (m *modal) buttonsWidth() int {
	total := 0
	for i, b := range m.buttons {
		total += m.ov.ButtonWidth(b.label)
		if i > 0 {
			total += gapButtons
		}
	}
	return total
}

// drawButtons paints the button row centered in the w cells at x0, on row y. It
// records one hit rectangle per button.
func (m *modal) drawButtons(g *grid.Grid, x0, y, w int) {
	x := x0 + max((w-m.buttonsWidth())/2, 0)
	focus, focused := m.buttonFocus()
	for i, b := range m.buttons {
		st := m.ov.Idle
		switch {
		case focused && i == focus:
			st = m.ov.Focus
		case i == m.hover:
			st = m.ov.Hover
		}
		bw := m.ov.ButtonWidth(b.label)
		m.ov.PaintButton(g, x, y, b.label, b.under, st)
		m.hits = append(m.hits, rect{X: x, Y: y, W: bw, H: 1})
		x += bw + gapButtons
	}
}
