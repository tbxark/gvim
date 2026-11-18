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
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

var asciiStrings [128]string

func init() {
	for i := range asciiStrings {
		asciiStrings[i] = string(byte(i))
	}
}

// Style is the set of attributes of one cell. A nil color is the terminal default.
type Style struct {
	FG, BG color.Color
	// UnderlineColor is the underline color (SGR 58), which the Kitty Unicode
	// placeholder protocol reads as the placement id. Nil leaves the terminal's
	// default underline color.
	UnderlineColor                                  color.Color
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

// Grid is a fixed-size cell buffer stored in row-major 1D order. Coordinates are
// zero-based: x is the column and y is the row. Writes outside the grid are clipped.
// A sub-grid created by Sub shares the backing cells slice with a stride and origin.
type Grid struct {
	w, h   int
	stride int
	ox, oy int
	cells  []Cell
	// keys is the per-cell theme-key capture: allocated only by CaptureKeys, so a
	// normal frame pays nothing beyond a nil check on each write.
	keys []cellKeys
}

// cellKeys is one cell's captured theme-key ids, FGKey then BGKey.
type cellKeys struct{ fg, bg uint16 }

// New returns a w by h grid of blank cells in the default style. A negative
// size is treated as zero.
func New(w, h int) *Grid {
	g := &Grid{}
	g.Resize(w, h)
	return g
}

// Resize resets the grid dimensions to w by h blanks in the default style,
// reusing the backing slice when its capacity suffices.
func (g *Grid) Resize(w, h int) {
	w, h = max(w, 0), max(h, 0)
	n := w * h
	if cap(g.cells) >= n {
		g.cells = g.cells[:n]
	} else {
		g.cells = make([]Cell, n)
	}
	for i := range g.cells {
		g.cells[i] = Cell{Text: " "}
	}
	g.w, g.h, g.stride, g.ox, g.oy = w, h, w, 0, 0
	g.keys = nil
}

// Clear fills every cell in the grid with blanks in style st.
func (g *Grid) Clear(st Style) {
	if g.w <= 0 || g.h <= 0 {
		return
	}
	blank := Cell{Text: " ", Style: st}
	if g.ox == 0 && g.oy == 0 && g.stride == g.w {
		for i := range g.cells {
			g.cells[i] = blank
		}
		if g.keys != nil {
			k := cellKeys{fg: st.FGKey, bg: st.BGKey}
			for i := range g.keys {
				g.keys[i] = k
			}
		}
		return
	}
	for y := range g.h {
		g.breakSpanBounds(y, 0, g.w)
		row := g.row(y)
		for x := range row {
			row[x] = blank
		}
		if g.keys != nil {
			krow := g.keyRow(y)
			k := cellKeys{fg: st.FGKey, bg: st.BGKey}
			for x := range krow {
				krow[x] = k
			}
		}
	}
}

// Sub returns a sub-grid view covering [x0, x0+w) × [y0, y0+h) within g without
// allocating cells. ok is false if any part of the rectangle lies outside g.
func (g *Grid) Sub(x0, y0, w, h int) (Grid, bool) {
	if x0 < 0 || y0 < 0 || w < 0 || h < 0 || x0+w > g.w || y0+h > g.h {
		return Grid{}, false
	}
	return Grid{
		w:      w,
		h:      h,
		stride: g.stride,
		ox:     g.ox + x0,
		oy:     g.oy + y0,
		cells:  g.cells,
		keys:   g.keys,
	}, true
}

func (g *Grid) row(y int) []Cell {
	off := (g.oy+y)*g.stride + g.ox
	return g.cells[off : off+g.w]
}

func (g *Grid) keyRow(y int) []cellKeys {
	off := (g.oy+y)*g.stride + g.ox
	return g.keys[off : off+g.w]
}

// Reset makes every cell blank in the default style again, so a grid can be painted
// again without allocating a new one. The key capture buffer, if any, is cleared too.
func (g *Grid) Reset() {
	g.Clear(Style{})
}

// CaptureKeys allocates the per-cell theme-key buffer and makes the grid record
// Style.FGKey/BGKey on every write. The theme inspector calls it on the one
// repaint it attributes; everything else paints without it.
func (g *Grid) CaptureKeys() {
	n := len(g.cells)
	if cap(g.keys) >= n {
		g.keys = g.keys[:n]
		clear(g.keys)
	} else {
		g.keys = make([]cellKeys, n)
	}
}

// KeysAt returns the theme-key ids recorded for the cell under the last style
// written there. (0, 0) means unknown, outside the grid, or capture off.
func (g *Grid) KeysAt(x, y int) (fg, bg uint16) {
	if g.keys == nil || x < 0 || y < 0 || x >= g.w || y >= g.h {
		return 0, 0
	}
	k := g.keyRow(y)[x]
	return k.fg, k.bg
}

// recordKeys copies st's theme-key ids into the capture buffer for the cell.
func (g *Grid) recordKeys(x, y int, st Style) {
	if g.keys == nil || x < 0 || y < 0 || x >= g.w || y >= g.h {
		return
	}
	g.keyRow(y)[x] = cellKeys{fg: st.FGKey, bg: st.BGKey}
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
	return g.row(y)[x]
}

// Set places one grapheme in the cell at (x, y), replacing what was there. It
// does not clip a wide grapheme; use Put for text that may be wider than one cell.
// Writing the grapheme a cell already holds only restyles it, and a wide grapheme
// keeps its continuation cell, which takes the style too.
func (g *Grid) Set(x, y int, text string, st Style) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return
	}
	row := g.row(y)
	if c := &row[x]; c.Text == text && text != "" {
		c.Style = st
		g.recordKeys(x, y, st)
		if x+1 < g.w && row[x+1].Text == "" {
			row[x+1].Style = st
			g.recordKeys(x+1, y, st)
		}
		return
	}
	g.breakPairAt(y, x)
	row[x] = Cell{Text: text, Style: st}
	g.recordKeys(x, y, st)
}

// Fill sets the cells in [x0, x1) of row y to blanks in style st.
func (g *Grid) Fill(x0, x1, y int, st Style) {
	if y < 0 || y >= g.h {
		return
	}
	x0, x1 = max(x0, 0), min(x1, g.w)
	if x0 >= x1 {
		return
	}
	g.breakSpanBounds(y, x0, x1)
	row := g.row(y)
	blank := Cell{Text: " ", Style: st}
	if g.keys == nil {
		for x := x0; x < x1; x++ {
			row[x] = blank
		}
		return
	}
	krow := g.keyRow(y)
	k := cellKeys{fg: st.FGKey, bg: st.BGKey}
	for x := x0; x < x1; x++ {
		row[x] = blank
		krow[x] = k
	}
}

// Put writes s starting at (x, y), clipped to the column maxX (exclusive). A
// grapheme that does not fit whole is dropped rather than split. It returns the
// number of cells the text advanced.
func (g *Grid) Put(x, y, maxX int, s string, st Style) int {
	if y < 0 || y >= g.h || s == "" {
		return 0
	}
	maxX = min(maxX, g.w)
	if x >= maxX {
		return 0
	}
	row := g.row(y)
	var krow []cellKeys
	if g.keys != nil {
		krow = g.keyRow(y)
	}
	k := cellKeys{fg: st.FGKey, bg: st.BGKey}
	start := x
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x80 && (i+1 == len(s) || s[i+1] < 0x80) {
			if b < 0x20 || b == 0x7f {
				i++
				continue
			}
			if x < 0 {
				x++
				i++
				continue
			}
			if x+1 > maxX {
				break
			}
			g.breakPairAt(y, x)
			row[x] = Cell{Text: asciiStrings[b], Style: st}
			if krow != nil {
				krow[x] = k
			}
			x++
			i++
			continue
		}
		gr, w := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		if gr == "" {
			break
		}
		i += len(gr)
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
				if row[p].Text == "" && p > 0 {
					p--
				}
				row[p].Text += gr
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
		g.breakPairAt(y, x)
		if w == 2 {
			// The continuation lands on x+1, which may be the lead of another pair.
			g.breakPairAt(y, x+1)
		}
		row[x] = Cell{Text: gr, Style: st}
		if krow != nil {
			krow[x] = k
		}
		if w == 2 {
			row[x+1] = Cell{Text: "", Style: st}
			if krow != nil {
				krow[x+1] = k
			}
		}
		x += w
	}
	return x - start
}

// breakPairAt repairs a wide grapheme in the full backing row that a write at
// sub-grid column x is about to split (including across sub-grid edges).
func (g *Grid) breakPairAt(y, x int) {
	rowStart := (g.oy + y) * g.stride
	full := g.cells[rowStart : rowStart+g.stride]
	ax := g.ox + x
	if full[ax].Text == "" && ax > 0 {
		full[ax-1].Text = " "
	}
	if ax+1 < g.stride && full[ax+1].Text == "" && full[ax].Text != "" {
		full[ax+1] = Cell{Text: " ", Style: full[ax+1].Style}
	}
}

// breakSpanBounds repairs wide graphemes straddling [x0, x1) across sub-grid edges.
func (g *Grid) breakSpanBounds(y, x0, x1 int) {
	rowStart := (g.oy + y) * g.stride
	full := g.cells[rowStart : rowStart+g.stride]
	ax0, ax1 := g.ox+x0, g.ox+x1
	if ax0 > 0 && ax0 < g.stride && full[ax0].Text == "" {
		full[ax0-1].Text = " "
	}
	if ax1 > 0 && ax1 < g.stride && full[ax1].Text == "" && full[ax1-1].Text != "" {
		full[ax1] = Cell{Text: " ", Style: full[ax1].Style}
	}
}

// Plain returns each row as text without styles. A wide grapheme appears once.
func (g *Grid) Plain() []string {
	rows := make([]string, g.h)
	var b strings.Builder
	b.Grow(g.w)
	for y := range g.h {
		b.Reset()
		for _, c := range g.row(y) {
			b.WriteString(c.Text)
		}
		rows[y] = b.String()
	}
	return rows
}

func (g *Grid) writeLine(b *strings.Builder, y int, sgrBuf []byte) []byte {
	var cur Style
	for _, c := range g.row(y) {
		if !sameStyle(c.Style, cur) {
			sgrBuf = appendSGR(sgrBuf[:0], c.Style)
			b.Write(sgrBuf)
			cur = c.Style
		}
		b.WriteString(c.Text)
	}
	if !sameStyle(cur, Style{}) {
		b.WriteString(ansi.ResetStyle)
	}
	return sgrBuf
}

// Lines returns each row with SGR escape sequences. The style is written only
// where it changes, and a row that ends styled is closed with a reset.
func (g *Grid) Lines() []string {
	rows := make([]string, g.h)
	var b strings.Builder
	b.Grow(g.w * 2)
	sgrBuf := make([]byte, 0, 64)
	for y := range g.h {
		b.Reset()
		sgrBuf = g.writeLine(&b, y, sgrBuf)
		rows[y] = b.String()
	}
	return rows
}

// String returns Lines joined with newlines.
func (g *Grid) String() string {
	if g.h == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(g.h * (g.w*2 + 8))
	sgrBuf := make([]byte, 0, 64)
	for y := range g.h {
		if y > 0 {
			b.WriteByte('\n')
		}
		sgrBuf = g.writeLine(&b, y, sgrBuf)
	}
	return b.String()
}

// sameStyle compares two styles without unnecessary color interface boxing or
// RGBA scaling when the concrete types match.
func sameStyle(a, b Style) bool {
	if a.Bold != b.Bold || a.Faint != b.Faint || a.Italic != b.Italic ||
		a.Underline != b.Underline || a.Strike != b.Strike || a.Reverse != b.Reverse {
		return false
	}
	return sameColor(a.FG, b.FG) && sameColor(a.BG, b.BG) && sameColor(a.UnderlineColor, b.UnderlineColor)
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch ac := a.(type) {
	case color.RGBA:
		if bc, ok := b.(color.RGBA); ok {
			return ac == bc
		}
	case color.NRGBA:
		if bc, ok := b.(color.NRGBA); ok {
			return ac == bc
		}
	case ansi.BasicColor:
		if bc, ok := b.(ansi.BasicColor); ok {
			return ac == bc
		}
	case ansi.IndexedColor:
		if bc, ok := b.(ansi.IndexedColor); ok {
			return ac == bc
		}
	case ansi.RGBColor:
		if bc, ok := b.(ansi.RGBColor); ok {
			return ac == bc
		}
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func appendSGR(buf []byte, st Style) []byte {
	if st.UnderlineColor != nil {
		return append(buf, sgr(st)...)
	}
	buf = append(buf, "\x1b[0"...)
	if st.Bold {
		buf = append(buf, ";1"...)
	}
	if st.Faint {
		buf = append(buf, ";2"...)
	}
	if st.Italic {
		buf = append(buf, ";3"...)
	}
	if st.Underline {
		buf = append(buf, ";4"...)
	}
	if st.Strike {
		buf = append(buf, ";9"...)
	}
	if st.Reverse {
		buf = append(buf, ";7"...)
	}
	if st.FG != nil {
		buf = appendColorSGR(buf, st.FG, true)
	}
	if st.BG != nil {
		buf = appendColorSGR(buf, st.BG, false)
	}
	return append(buf, 'm')
}

func appendColorSGR(buf []byte, c color.Color, fg bool) []byte {
	switch v := c.(type) {
	case ansi.BasicColor:
		code := int(v)
		if fg {
			if code < 8 {
				code += 30
			} else {
				code += 82 // 90 + (code - 8)
			}
		} else {
			if code < 8 {
				code += 40
			} else {
				code += 92 // 100 + (code - 8)
			}
		}
		buf = append(buf, ';')
		return strconv.AppendInt(buf, int64(code), 10)
	case ansi.IndexedColor:
		if fg {
			buf = append(buf, ";38;5;"...)
		} else {
			buf = append(buf, ";48;5;"...)
		}
		return strconv.AppendInt(buf, int64(v), 10)
	case color.RGBA:
		return appendRGBSGR(buf, v.R, v.G, v.B, fg)
	case color.NRGBA:
		return appendRGBSGR(buf, v.R, v.G, v.B, fg)
	case ansi.RGBColor:
		return appendRGBSGR(buf, v.R, v.G, v.B, fg)
	default:
		r, g, b, _ := c.RGBA()
		return appendRGBSGR(buf, uint8(r>>8), uint8(g>>8), uint8(b>>8), fg)
	}
}

func appendRGBSGR(buf []byte, r, g, b uint8, fg bool) []byte {
	if fg {
		buf = append(buf, ";38;2;"...)
	} else {
		buf = append(buf, ";48;2;"...)
	}
	buf = strconv.AppendInt(buf, int64(r), 10)
	buf = append(buf, ';')
	buf = strconv.AppendInt(buf, int64(g), 10)
	buf = append(buf, ';')
	return strconv.AppendInt(buf, int64(b), 10)
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
	if st.UnderlineColor != nil {
		s = s.UnderlineColor(convertColor(st.UnderlineColor))
	}
	return s.String()
}

// convertColor keeps a color that is already an ansi color, so an indexed
// underline color stays indexed, and otherwise hands the color to ansi.
func convertColor(c color.Color) ansi.Color {
	if ac, ok := c.(ansi.Color); ok {
		return ac
	}
	return c
}
