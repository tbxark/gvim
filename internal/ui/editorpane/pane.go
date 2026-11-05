// Package editorpane paints the text area of the editor: the visible buffer
// lines with their gutter, soft-wrapped rows, selection, syntax colors,
// change marks and the vertical scrollbar. It also maps a cell of the area back
// to a buffer position for the mouse.
//
// The package is pure. A Pane is a value built for one frame. Render returns a
// grid, and Caret, Hit, PosAt and ScrollTopFor answer from the same geometry as
// Render. Nothing is read or written outside the arguments, and there is no
// tea import. Coordinates are pane-local: column 0 is the pane's left edge (the
// first gutter column), and row 0 is the first body row. The root model adds
// the explorer columns and the body top before it calls in.
//
// Soft wrap follows the legacy rule (internal/tui/wrap.go, derived from the
// legacy screens). A buffer line wider than the wrap width takes several rows, and the
// wrap width is the text area less two columns. The caret and the mouse both
// work in display rows, but the editor moves and scrolls by buffer lines.
package editorpane

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// Document is the read-only view the pane paints. ui.Document has the same
// method set, so the root passes its active document through unchanged.
type Document interface {
	Modified() bool
	LineCount() int
	Line(i int) string
	// SavedLines is the text at open or the last save, split into lines. The
	// pane diffs it against the current lines for the change marks.
	SavedLines() []string
	Cursor() buffer.Pos
	// Selection returns the anchor and the head. ok is false when nothing is
	// selected.
	Selection() (anchor, head buffer.Pos, ok bool)
	// LineSpans returns the syntax spans of line i, or nil when there is no
	// lexer. The caller caches the lexing per document version. The pane asks
	// only for the lines it paints.
	LineSpans(i int) []syntax.Span
}

// Pane is the editor text area for one frame.
type Pane struct {
	Doc    Document // nil paints blank body rows
	Top    int      // first visible buffer line; clamped to the document
	Styles Styles
}

// HitKind names what is under a pane cell.
type HitKind int

// The kinds a Hit can report.
const (
	HitNone           HitKind = iota // outside the body, or no document
	HitText                          // the text: a caret or selection position
	HitGutter                        // the line numbers: the start of the line
	HitScrollbarTrack                // the scrollbar, outside the thumb
	HitScrollbarThumb                // the scrollbar thumb
)

// Hit is the result of a mouse hit test. Pos is set for text and gutter hits,
// to the document position the press maps to. Row is the body row of the cell,
// which the scrollbar uses with ScrollTopFor.
type Hit struct {
	Kind HitKind
	Pos  buffer.Pos
	Row  int
}

// Gutter sizes. The number field is at least three digits wide, and it widens
// by one once the line count has three digits (from 100 lines), as the legacy
// gutter does (legacy at 200 lines: "  88 │" at 200 lines).
const (
	gutterMinDigits = 3
	gutterSeparator = 3 // " │ " after the number field
	tildeText       = "~"
	changeBar       = "│" // margin bar: new or grown line
	changeScroll    = "▌" // last column: line differs from the saved text
	selectedBlank   = '·' // selected blanks are drawn as middle dots (legacy)
	thumbGlyph      = "┃" // scrollbar thumb: secondary color (spec section 3)
	trackGlyph      = "│" // scrollbar track: separator color (spec section 3)
)

// GutterWidth is the width of the line-number gutter for a document with lines
// lines. It matches the gutter the layout reserves in the root.
func GutterWidth(lines int) int {
	digits := len(strconv.Itoa(max(lines, 0)))
	return max(gutterMinDigits, digits+1) + gutterSeparator
}

// frame is the per-call state of one Render, Caret or Hit: the document, the
// window and the derived geometry. It is built once and passed by value.
type frame struct {
	doc      Document
	styles   Styles
	w, h     int
	lines    int
	top      int
	gutter   int
	wrapW    int
	overflow bool
	caret    buffer.Pos
	sel      *buffer.Selection
	bar      map[int]bool // lines that get the margin bar
	scroll   map[int]bool // lines that get the last-column change mark
}

// newFrame builds the frame for a w by h pane. It reports false when there is
// nothing to paint: no document, or an empty pane.
func (p Pane) newFrame(w, h int) (frame, bool) {
	if p.Doc == nil || w <= 0 || h <= 0 {
		return frame{}, false
	}
	lines := max(p.Doc.LineCount(), 1)
	f := frame{
		doc:    p.Doc,
		styles: p.Styles,
		w:      w,
		h:      h,
		lines:  lines,
		top:    min(max(p.Top, 0), lines-1),
		gutter: GutterWidth(lines),
		caret:  p.Doc.Cursor(),
	}
	f.wrapW = (w - f.gutter) - 2 // the legacy wrap width: text area less two
	f.overflow = lines > h
	if a, hd, ok := p.Doc.Selection(); ok {
		f.sel = &buffer.Selection{Anchor: a, Head: hd}
	}
	if p.Doc.Modified() {
		f.bar, f.scroll = f.changes()
	}
	return f, true
}

// changes diffs the saved lines against the current lines.
func (f frame) changes() (bar, scroll map[int]bool) {
	cur := make([]string, f.lines)
	for i := range cur {
		cur[i] = f.doc.Line(i)
	}
	return changeMarks(f.doc.SavedLines(), cur)
}

// Render paints the pane into a new w by h grid. Body rows past the end of the
// document show "~" in the gutter. The scrollbar takes the last column when the
// document is taller than h. The caret cell is drawn in reverse video when it is
// in the body; Caret returns the same position for the terminal cursor.
func (p Pane) Render(w, h int) *grid.Grid {
	g := grid.New(w, h)
	for y := range g.Height() {
		g.Fill(0, g.Width(), y, p.Styles.Text)
	}
	f, ok := p.newFrame(w, h)
	if !ok {
		return g
	}
	rows := f.paintText(g)
	f.paintMarks(g, rows)
	f.paintCaret(g)
	return g
}

// paintCaret reverses the cell under the caret, so the caret shows even when
// the terminal cursor is hidden. The caret never takes the scrollbar column.
func (f frame) paintCaret(g *grid.Grid) {
	x, y, ok := f.caretPos()
	if !ok || x >= f.w-1 {
		return
	}
	c := g.At(x, y)
	st := c.Style
	st.Reverse = true
	text := c.Text
	if text == "" {
		text = " "
	}
	g.Set(x, y, text, st)
}

// rowInfo ties a painted body row to the buffer line it shows. The marks are
// per line, so every row of a wrapped line takes them.
type rowInfo struct {
	y, line int
}

// paintText writes the gutter and the text of every body row and returns the
// row list in order.
func (f frame) paintText(g *grid.Grid) []rowInfo {
	rows := make([]rowInfo, 0, f.h)
	y, i := 0, f.top
	for ; i < f.lines && y < f.h; i++ {
		ln := f.layoutLine(i, true)
		for k, sp := range ln.spans {
			if y >= f.h {
				break
			}
			label := fmt.Sprintf("%*s │ ", f.gutter-gutterSeparator, "")
			if k == 0 {
				label = fmt.Sprintf("%*d │ ", f.gutter-gutterSeparator, i+1)
			}
			g.Put(0, y, f.w-1, label, f.styles.Gutter)
			f.paintCells(g, y, ln.cells[sp.from:sp.to])
			rows = append(rows, rowInfo{y: y, line: i})
			y++
		}
	}
	for ; y < f.h; y, i = y+1, i+1 {
		g.Put(0, y, f.w-1, tildeText, f.styles.Gutter)
		rows = append(rows, rowInfo{y: y, line: i})
	}
	return rows
}

// paintCells writes one display row of cells from the gutter edge. Runs of the
// same attributes are written together, and a zero-width mark joins the run
// before it. The last column is never text.
func (f frame) paintCells(g *grid.Grid, y int, cells []cell) {
	x := f.gutter
	maxX := f.w - 1
	for start := 0; start < len(cells); {
		end := start + 1
		for end < len(cells) && (cells[end].sameAttr(cells[start]) || isZeroWidth(cells[end].r)) {
			end++
		}
		var b strings.Builder
		for _, c := range cells[start:end] {
			b.WriteRune(c.r)
		}
		x += g.Put(x, y, maxX, b.String(), f.style(cells[start]))
		start = end
	}
}

// style is the grid style of one cell: its syntax color, with the selection
// background when the cell is selected.
func (f frame) style(c cell) grid.Style {
	st := f.styles.Text
	if int(c.kind) < len(f.styles.Syntax) {
		st = f.styles.Syntax[c.kind]
	}
	if c.sel {
		st.BG = f.styles.Selection.BG
		if st.BG == nil {
			st.Reverse = true
		}
	}
	return st
}

// paintMarks writes the margin bar, the change mark and the scrollbar on the
// rows. The change mark has the last column when the line changed. Without a
// change mark, the scrollbar takes the column when the document overflows. The
// thumb is a "┃" in the thumb style and the track a "│" in the track style.
func (f frame) paintMarks(g *grid.Grid, rows []rowInfo) {
	last := f.w - 1
	thumbStart, thumbLen := f.thumb()
	for _, r := range rows {
		if f.bar[r.line] {
			g.Set(0, r.y, changeBar, f.styles.Gutter)
		}
		switch {
		case f.scroll[r.line]:
			g.Set(last, r.y, changeScroll, f.styles.Gutter)
		case f.overflow && r.y >= thumbStart && r.y < thumbStart+thumbLen:
			g.Set(last, r.y, thumbGlyph, f.styles.Thumb)
		case f.overflow:
			g.Set(last, r.y, trackGlyph, f.styles.Scrollbar)
		}
	}
}

// Caret returns the terminal cursor position of the document caret, in
// pane-local cells. ok is false when the caret line is above the window or
// below the body.
func (p Pane) Caret(w, h int) (x, y int, ok bool) {
	f, ok := p.newFrame(w, h)
	if !ok {
		return 0, 0, false
	}
	return f.caretPos()
}

// caretPos finds the caret row by walking the rows from the top line.
func (f frame) caretPos() (x, y int, ok bool) {
	for i := f.top; i < f.lines && y < f.h; i++ {
		ln := f.layoutLine(i, false)
		if i == f.caret.Line {
			raw := []rune(f.doc.Line(i))
			k, cells := caretInSpans(ln.geom, ln.spans, expandedIndex(raw, f.caret.Col))
			y += k
			return f.gutter + cells, y, y < f.h
		}
		y += len(ln.spans)
	}
	return 0, 0, false
}

// Hit maps a pane-local cell to what it shows. A cell outside the body, or in
// a pane with no document, is HitNone. The last column is the scrollbar when
// the document overflows, and the text otherwise.
func (p Pane) Hit(w, h, x, y int) Hit {
	f, ok := p.newFrame(w, h)
	if !ok || x < 0 || y < 0 || x >= w || y >= h {
		return Hit{Kind: HitNone}
	}
	switch {
	case x < f.gutter:
		return Hit{Kind: HitGutter, Pos: buffer.Pos{Line: f.rowAt(y).line}, Row: y}
	case f.overflow && x == w-1:
		start, length := f.thumb()
		if y >= start && y < start+length {
			return Hit{Kind: HitScrollbarThumb, Row: y}
		}
		return Hit{Kind: HitScrollbarTrack, Row: y}
	}
	return Hit{Kind: HitText, Pos: f.posAt(x, y), Row: y}
}

// PosAt maps a pane-local cell to a document position. The row and the column
// are clamped into the document, so a drag past the edge gives the nearest
// position. A column left of the text maps to the start of the row, and a
// column past the end of a line maps to its last character.
func (p Pane) PosAt(w, h, x, y int) buffer.Pos {
	f, ok := p.newFrame(w, h)
	if !ok {
		return buffer.Pos{}
	}
	return f.posAt(x, y)
}

// ScrollTopFor returns the first visible line that centers the scrollbar thumb
// on body row row. It is the target of a track press, and of a thumb drag when
// the caller passes the row where the thumb's centre should go.
func (p Pane) ScrollTopFor(h, row int) int {
	if p.Doc == nil || h <= 0 {
		return 0
	}
	lines := max(p.Doc.LineCount(), 1)
	_, length := thumbOf(lines, h, 0)
	pos := max(row-length/2, 0)
	top := (pos*lines + h/2) / h
	return min(top, max(lines-h, 0))
}

// Thumb returns the first body row and the length in rows of the scrollbar
// thumb for the current top line in a pane h rows tall. It is meaningful when
// the document is taller than the pane.
func (p Pane) Thumb(h int) (start, length int) {
	if p.Doc == nil || h <= 0 {
		return 0, 0
	}
	lines := max(p.Doc.LineCount(), 1)
	return thumbOf(lines, h, min(max(p.Top, 0), lines-1))
}

// thumb is the scrollbar thumb of the frame: its first body row and length.
func (f frame) thumb() (start, length int) {
	return thumbOf(f.lines, f.h, f.top)
}

// thumbOf sizes the thumb for lines document lines in a body h rows tall, with
// the first visible line top. The thumb is one row per lines/h of the document,
// and its top follows the first visible line through the body.
func thumbOf(lines, h, top int) (start, length int) {
	length = min(h, max(1, (h*h+lines/2)/lines))
	start = min(top, lines-1) * h / lines
	return start, length
}

// posAt maps the cell at (x, row) to a document position. See PosAt.
func (f frame) posAt(x, row int) buffer.Pos {
	r := f.rowAt(row)
	cells := r.startCells()
	if x >= f.gutter {
		cells += x - f.gutter
		if !r.last && cells >= r.endCells() {
			// A click past the end of a row that is not the last one lands on its
			// last character, not on the first character of the next row.
			cells = r.endCells() - 1
		}
	}
	return buffer.Pos{Line: r.line, Col: runeIndexAt([]rune(f.doc.Line(r.line)), cells)}
}

// Blit copies src into dst with its top-left cell at (x0, y0). Cells outside dst
// are dropped. The root uses it to place the pane in the frame.
func Blit(dst *grid.Grid, x0, y0 int, src *grid.Grid) {
	for y := range src.Height() {
		for x := range src.Width() {
			c := src.At(x, y)
			dst.Set(x0+x, y0+y, c.Text, c.Style)
		}
	}
}
