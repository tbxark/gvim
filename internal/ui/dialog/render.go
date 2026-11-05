package dialog

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// The colors are crush's Charmtone Pantera, as the spec names them.
var (
	primary      = color.RGBA{0x6B, 0x50, 0xFF, 0xFF} // Charple, the border and the title fill start
	secondary    = color.RGBA{0xFF, 0x60, 0xFF, 0xFF} // Dolly, the title fill end and the focused button
	onPrimary    = color.RGBA{0xFF, 0xFA, 0xF1, 0xFF} // Butter
	fgBase       = color.RGBA{0xEC, 0xEB, 0xF0, 0xFF} // Sash
	fgSubtle     = color.RGBA{0xBF, 0xBC, 0xC8, 0xFF} // Smoke
	fgMoreSubtle = color.RGBA{0x85, 0x83, 0x92, 0xFF} // Squid
	fgMostSubtle = color.RGBA{0x60, 0x5F, 0x6B, 0xFF} // Oyster, the hovered button
	bgBase       = color.RGBA{0x20, 0x1F, 0x26, 0xFF} // Pepper, the box
	bgLessVis    = color.RGBA{0x3A, 0x39, 0x43, 0xFF} // Char, the field and the blurred button
	bgMostVis    = color.RGBA{0x4D, 0x4C, 0x57, 0xFF} // Iron, the blurred list selection
	errorColor   = color.RGBA{0xEB, 0x42, 0x68, 0xFF} // Sriracha
)

// The cell styles of the dialog parts.
var (
	stText     = grid.Style{FG: fgBase, BG: bgBase}
	stMuted    = grid.Style{FG: fgMoreSubtle, BG: bgBase}
	stErr      = grid.Style{FG: errorColor, BG: bgBase}
	stTitle    = grid.Style{FG: fgBase, BG: bgBase, Bold: true}
	stBorder   = grid.Style{FG: primary, BG: bgBase}
	stField    = grid.Style{FG: fgBase, BG: bgLessVis}
	stBtnIdle  = grid.Style{FG: fgBase, BG: bgLessVis}
	stBtnFocus = grid.Style{FG: onPrimary, BG: secondary, Bold: true}
	stBtnHover = grid.Style{FG: fgBase, BG: fgMostSubtle}
	stRowFocus = grid.Style{FG: onPrimary, BG: primary}
	stRowIdle  = grid.Style{FG: fgSubtle, BG: bgMostVis}
	stRowPlain = grid.Style{FG: fgBase, BG: bgBase}
)

// The width of a dialog's content, in cells. The box adds two border cells and
// two padding cells on each side.
const (
	minContent = 40
	maxContent = 64
	boxChrome  = 4
	boxRows    = 7 // top border, title, blank, blank, buttons, blank, bottom border
	gapButtons = 2
)

// rect is a cell rectangle.
type rect struct{ x, y, w, h int }

// contains reports whether (x, y) is inside r.
func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

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
	cw = min(cw, frameWidth-boxChrome)
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
// body builds the rows at the content width it is given. The buttons come last,
// under the body. Hit regions and the caret are recorded for the next event.
func (m *modal) frame(g *grid.Grid, natural int, body func(cw int) []row) {
	cw := fitWidth(g.Width(), natural)
	rows := body(cw)
	w, h := cw+boxChrome, len(rows)+boxRows
	x := max((g.Width()-w)/2, 0)
	y := max((g.Height()-h)/2, 0)
	m.box = rect{x, y, w, h}
	m.caretOK = false
	m.hits = m.hits[:0]
	paintBox(g, m.box, m.title)
	for i, r := range rows {
		r(g, x+2, y+3+i, cw)
	}
	m.drawButtons(g, x+2, y+len(rows)+4, cw)
}

// drawText paints a dialog whose body is plain text.
func (m *modal) drawText(g *grid.Grid, text string) {
	m.frame(g, m.natural(text), func(cw int) []row {
		return textRows(text, cw, stText)
	})
}

// paintBox fills r with the box background, draws the rounded primary border,
// and writes the title on the first row inside, followed by a fill of ╱ that
// runs from primary to secondary.
func paintBox(g *grid.Grid, r rect, title string) {
	for y := r.y; y < r.y+r.h; y++ {
		g.Fill(r.x, r.x+r.w, y, stText)
	}
	b := lipgloss.RoundedBorder()
	right, bottom := r.x+r.w-1, r.y+r.h-1
	for x := r.x + 1; x < right; x++ {
		g.Set(x, r.y, b.Top, stBorder)
		g.Set(x, bottom, b.Bottom, stBorder)
	}
	for y := r.y + 1; y < bottom; y++ {
		g.Set(r.x, y, b.Left, stBorder)
		g.Set(right, y, b.Right, stBorder)
	}
	g.Set(r.x, r.y, b.TopLeft, stBorder)
	g.Set(right, r.y, b.TopRight, stBorder)
	g.Set(r.x, bottom, b.BottomLeft, stBorder)
	g.Set(right, bottom, b.BottomRight, stBorder)

	ty := r.y + 1
	end := right - 1
	n := g.Put(r.x+2, ty, end, " "+title+" ", stTitle)
	start := r.x + 2 + n
	span := max(end-start-1, 1)
	for x := start; x < end; x++ {
		t := float64(x-start) / float64(span)
		g.Set(x, ty, "╱", grid.Style{FG: lerp(primary, secondary, t), BG: bgBase})
	}
}

// lerp mixes two colors. t is 0 at a and 1 at b.
func lerp(a, b color.RGBA, t float64) color.Color {
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5)
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 0xFF}
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
		st := stBtnIdle
		switch {
		case focused && i == focus:
			st = stBtnFocus
		case i == m.hover:
			st = stBtnHover
		}
		bw := buttonWidth(b)
		drawButton(g, x, y, b, st)
		m.hits = append(m.hits, rect{x, y, bw, 1})
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
