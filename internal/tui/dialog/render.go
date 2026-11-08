package dialog

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// The width of a dialog's content, in cells. The box adds two border cells and
// two padding cells on each side.
const (
	minContent = 40
	maxContent = 64
	boxChrome  = 4
	boxRows    = 4 // top border with the title, the blank row above the buttons, the button row, the bottom border
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
	for _, line := range wrapText(text, cw) {
		rows = append(rows, textRow(line, st))
	}
	return rows
}

// wrapText breaks s into lines no wider than width cells.
func wrapText(s string, width int) []string {
	return strings.Split(ansi.Wrap(s, max(width, 1), ""), "\n")
}

// fitWidth returns the content width for a dialog whose widest line is natural
// cells: at least minContent, at most maxContent, and no wider than the frame
// allows.
func fitWidth(frameWidth, natural int) int {
	cw := min(max(natural, minContent), maxContent)
	cw = min(cw, frameWidth-boxChrome-2*space.Margin(frameWidth))
	return max(cw, 1)
}

// natural returns the width the dialog wants: the title, the buttons, and the
// widest of texts.
func (m *modal) natural(texts ...string) int {
	n := max(ansi.StringWidth(m.title)+boxChrome, m.buttonsWidth())
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
	cw := fitWidth(g.Width(), natural)
	rows := body(cw)
	w, h := cw+boxChrome, len(rows)+boxRows
	x := max((g.Width()-w)/2, 0)
	y := max((g.Height()-h)/2, 0)
	m.box = rect{X: x, Y: y, W: w, H: h}
	m.caretOK = false
	m.hits = m.hits[:0]
	m.ov.PaintBox(g, m.box.X, m.box.Y, m.box.W, m.box.H, m.title)
	for i, r := range rows {
		r(g, x+2, y+1+i, cw)
	}
	m.drawButtons(g, x+2, y+len(rows)+2, cw)
}

// drawText paints a dialog whose body is plain text.
func (m *modal) drawText(g *grid.Grid, text string) {
	m.frame(g, m.natural(text), func(cw int) []row {
		return textRows(text, cw, m.ov.Box)
	})
}

// buttonWidth is the cells a button takes: its label and two spaces on each side.
func buttonWidth(b button) int {
	return len([]rune(b.label)) + 4
}

// buttonsWidth is the width of the button row.
func (m *modal) buttonsWidth() int {
	total := 0
	for i, b := range m.buttons {
		total += buttonWidth(b)
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
		bw := buttonWidth(b)
		drawButton(g, x, y, b, st)
		m.hits = append(m.hits, rect{X: x, Y: y, W: bw, H: 1})
		x += bw + gapButtons
	}
}

// drawButton paints one button. The hotkey letter is underlined, and only that
// letter. The label is padded with two spaces on each side.
func drawButton(g *grid.Grid, x, y int, b button, st grid.Style) {
	r := []rune(b.label)
	underlined := st
	underlined.Underline = true
	w := g.Width()
	x += g.Put(x, y, w, "  ", st)
	x += g.Put(x, y, w, string(r[:b.under]), st)
	x += g.Put(x, y, w, string(r[b.under]), underlined)
	x += g.Put(x, y, w, string(r[b.under+1:]), st)
	g.Put(x, y, w, "  ", st)
}
