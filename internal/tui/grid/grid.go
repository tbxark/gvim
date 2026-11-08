// Package grid is the cell surface gvim paints a frame into. It is a fixed
// buffer of styled cells. Text is placed by terminal column, so wide graphemes
// take two cells and zero-width marks attach to the cell before them. Rendering
// writes one SGR-styled line per row, in the same escape form lipgloss emits.
//
// The package knows nothing about the editor or the theme. Callers pass colors
// as image/color values; the style package does the conversion from theme keys.
package grid

import (
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Style is the set of attributes of one cell. A nil color is the terminal default.
type Style struct {
	FG, BG                                          color.Color
	Bold, Faint, Italic, Underline, Strike, Reverse bool
	// FGKey and BGKey are the theme-key ids Style's colours came from, for the
	// theme inspector's per-cell provenance. Zero means unknown (a style built
	// without a theme role, or a modified style whose colour no key names). They
	// are only recorded when the grid's key capture is on, so normal painting
	// writes no attribution.
	FGKey, BGKey uint16
}

// Cell is one terminal cell. A wide grapheme occupies its own cell followed by
// a continuation cell whose Text is empty.
type Cell struct {
	Text  string
	Style Style
}

// Grid is a fixed-size cell buffer. Coordinates are zero-based: x is the column
// and y is the row. Writes outside the grid are clipped.
type Grid struct {
	w, h  int
	cells [][]Cell
	// keys is the per-cell theme-key capture: allocated only by CaptureKeys, so a
	// normal frame pays nothing beyond a nil check on each write.
	keys [][]cellKeys
}

// cellKeys is one cell's captured theme-key ids, FGKey then BGKey.
type cellKeys struct{ fg, bg uint16 }

// New returns a w by h grid of blank cells in the default style. A negative
// size is treated as zero.
func New(w, h int) *Grid {
	w, h = max(w, 0), max(h, 0)
	g := &Grid{w: w, h: h, cells: make([][]Cell, h)}
	for y := range g.cells {
		g.cells[y] = make([]Cell, w)
		for x := range g.cells[y] {
			g.cells[y][x] = Cell{Text: " "}
		}
	}
	return g
}

// CaptureKeys allocates the per-cell theme-key buffer and makes the grid record
// Style.FGKey/BGKey on every write. The theme inspector calls it on the one
// repaint it attributes; everything else paints without it.
func (g *Grid) CaptureKeys() {
	g.keys = make([][]cellKeys, g.h)
	for y := range g.keys {
		g.keys[y] = make([]cellKeys, g.w)
	}
}

// ReleaseKeys stops recording and frees the capture buffer.
func (g *Grid) ReleaseKeys() { g.keys = nil }

// CapturingKeys reports whether the grid records theme-key ids.
func (g *Grid) CapturingKeys() bool { return g.keys != nil }

// KeysAt returns the theme-key ids recorded for the cell under the last style
// written there. (0, 0) means unknown, outside the grid, or capture off.
func (g *Grid) KeysAt(x, y int) (fg, bg uint16) {
	if x < 0 || y < 0 || y >= len(g.keys) || x >= len(g.keys[y]) {
		return 0, 0
	}
	k := g.keys[y][x]
	return k.fg, k.bg
}

// recordKeys copies st's theme-key ids into the capture buffer for the cell.
func (g *Grid) recordKeys(x, y int, st Style) {
	if g.keys == nil || x < 0 || y < 0 || y >= g.h || x >= g.w {
		return
	}
	g.keys[y][x] = cellKeys{fg: st.FGKey, bg: st.BGKey}
}

// Width is the number of columns.
func (g *Grid) Width() int { return g.w }

// Height is the number of rows.
func (g *Grid) Height() int { return g.h }

// At returns the cell at (x, y). Outside the grid it returns a zero Cell.
func (g *Grid) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return Cell{}
	}
	return g.cells[y][x]
}

// Set places one grapheme in the cell at (x, y), replacing what was there. It
// does not clip a wide grapheme; use Put for text that may be wider than one cell.
// Writing the grapheme a cell already holds only restyles it, and a wide grapheme
// keeps its continuation cell, which takes the style too.
func (g *Grid) Set(x, y int, text string, st Style) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return
	}
	if c := &g.cells[y][x]; c.Text == text && text != "" {
		c.Style = st
		g.recordKeys(x, y, st)
		if x+1 < g.w && g.cells[y][x+1].Text == "" {
			g.cells[y][x+1].Style = st
			g.recordKeys(x+1, y, st)
		}
		return
	}
	g.breakPairs(x, y)
	g.cells[y][x] = Cell{Text: text, Style: st}
	g.recordKeys(x, y, st)
}

// Fill sets the cells in [x0, x1) of row y to blanks in style st.
func (g *Grid) Fill(x0, x1, y int, st Style) {
	if y < 0 || y >= g.h {
		return
	}
	x0, x1 = max(x0, 0), min(x1, g.w)
	for x := x0; x < x1; x++ {
		g.breakPairs(x, y)
		g.cells[y][x] = Cell{Text: " ", Style: st}
		g.recordKeys(x, y, st)
	}
}

// Put writes s starting at (x, y), clipped to the column maxX (exclusive). A
// grapheme that does not fit whole is dropped rather than split. It returns the
// number of cells the text advanced.
func (g *Grid) Put(x, y, maxX int, s string, st Style) int {
	if y < 0 || y >= g.h {
		return 0
	}
	maxX = min(maxX, g.w)
	start := x
	for rest := s; rest != ""; {
		gr, w := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if gr == "" {
			break
		}
		rest = rest[len(gr):]
		if w == 0 {
			// A control character has no width and no glyph. It is dropped, because
			// written into a cell it would reach the terminal as a control (a newline
			// or a carriage return in a file name moves the cursor under the frame).
			if r, _ := utf8.DecodeRuneInString(gr); unicode.IsControl(r) {
				continue
			}
			// Zero-width graphemes, such as combining marks, attach to the lead
			// cell of the grapheme before them.
			if p := x - 1; x > start && p >= 0 && p < g.w {
				if g.cells[y][p].Text == "" && p > 0 {
					p--
				}
				g.cells[y][p].Text += gr
			}
			continue
		}
		if x < 0 {
			// Text that starts left of the grid is clipped, not dropped.
			x += w
			continue
		}
		if x+w > maxX {
			break
		}
		g.breakPairs(x, y)
		g.cells[y][x] = Cell{Text: gr, Style: st}
		g.recordKeys(x, y, st)
		if w == 2 {
			g.cells[y][x+1] = Cell{Text: "", Style: st}
			g.recordKeys(x+1, y, st)
		}
		x += w
	}
	return x - start
}

// breakPairs repairs a wide grapheme that a write at (x, y) is about to split.
// Writing over the continuation cell blanks the lead cell, and writing over a
// lead cell blanks its continuation.
func (g *Grid) breakPairs(x, y int) {
	row := g.cells[y]
	if row[x].Text == "" && x > 0 {
		row[x-1].Text = " "
	}
	if x+1 < g.w && row[x+1].Text == "" && row[x].Text != "" {
		row[x+1] = Cell{Text: " ", Style: row[x+1].Style}
	}
}

// Plain returns each row as text without styles. A wide grapheme appears once.
func (g *Grid) Plain() []string {
	rows := make([]string, g.h)
	var b strings.Builder
	for y, row := range g.cells {
		b.Reset()
		for _, c := range row {
			b.WriteString(c.Text)
		}
		rows[y] = b.String()
	}
	return rows
}

// Lines returns each row with SGR escape sequences. The style is written only
// where it changes, and a row that ends styled is closed with a reset.
func (g *Grid) Lines() []string {
	rows := make([]string, g.h)
	var b strings.Builder
	for y, row := range g.cells {
		b.Reset()
		var cur Style
		for _, c := range row {
			if !sameStyle(c.Style, cur) {
				b.WriteString(sgr(c.Style))
				cur = c.Style
			}
			b.WriteString(c.Text)
		}
		if !sameStyle(cur, Style{}) {
			b.WriteString(ansi.ResetStyle)
		}
		rows[y] = b.String()
	}
	return rows
}

// String returns Lines joined with newlines.
func (g *Grid) String() string {
	return strings.Join(g.Lines(), "\n")
}

// sameStyle compares two styles. Colors are compared by their RGBA value, so a
// nil color and a color that is not nil never match.
func sameStyle(a, b Style) bool {
	if a.Bold != b.Bold || a.Faint != b.Faint || a.Italic != b.Italic ||
		a.Underline != b.Underline || a.Strike != b.Strike || a.Reverse != b.Reverse {
		return false
	}
	return sameColor(a.FG, b.FG) && sameColor(a.BG, b.BG)
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// sgr returns the escape sequence that sets st from a clean state. It starts
// with a reset, so the sequence does not depend on the style before it.
func sgr(st Style) string {
	s := ansi.Style{}.Reset()
	if st.Bold {
		s = s.Bold()
	}
	if st.Faint {
		s = s.Faint()
	}
	if st.Italic {
		s = s.Italic(true)
	}
	if st.Underline {
		s = s.Underline(true)
	}
	if st.Strike {
		s = s.Strikethrough(true)
	}
	if st.Reverse {
		s = s.Reverse(true)
	}
	if st.FG != nil {
		s = s.ForegroundColor(st.FG)
	}
	if st.BG != nil {
		s = s.BackgroundColor(st.BG)
	}
	return s.String()
}
