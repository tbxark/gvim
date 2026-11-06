// Package editorpane paints the text area of the editor: the visible buffer
// lines with their gutter, soft-wrapped rows, selection, syntax colors,
// change marks, the vertical and horizontal scrollbars, and the display aids
// (carets, rulers, indent guides, visible whitespace, the current line,
// occurrences, fold markers and diagnostic underlines). It also maps a cell of
// the area back to a buffer position for the mouse.
//
// The package is pure. A Pane is a value built for one frame. Render returns a
// grid, and Caret, Hit, PosAt, ScrollTopFor, RevealTop and BodyRows answer from
// the same geometry as Render. Nothing is read or written outside the arguments,
// and there is no tea import. Coordinates are pane-local: column 0 is the pane's
// left edge (the first gutter column), and row 0 is the first body row. The root
// model adds the explorer columns and the body top before it calls in.
//
// Soft wrap follows the legacy rule (internal/tui/wrap.go, derived from the
// legacy screens). With Wrap on, a buffer line wider than the wrap width takes
// several rows, and the wrap width is the text area less two columns. With Wrap
// off, each line is one row, and the window moves by display columns (Left).
// The caret and the mouse both work in display rows, but the editor moves and
// scrolls by buffer lines.
package editorpane

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/icons"
)

// Document is the read-only view the pane paints. ui.Document has the same
// method set, so the root passes its active document through unchanged.
//
// A Document may also implement CaretLister and Folder. The pane checks for
// them with a type assertion, so a document without them paints one caret and
// no folds.
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
	// HideGutter removes the line-number gutter and its change bar, so the text
	// starts at the first column. The zero value shows the gutter.
	HideGutter bool
	// Highlights gives the search highlights of line i. The root sets it while a
	// find bar is open. Nil paints none.
	Highlights func(line int) []Highlight
	// Left is the first visible display column when Wrap is off. The pane moves
	// it so the caret stays on the window.
	Left int
	// Wrap breaks a line wider than the text area over several rows. Off, each
	// line is one row, cut at the right edge, and the body shows a horizontal
	// scrollbar on its last row when a line is wider than the text area.
	Wrap bool
	// Rulers are the columns that carry a ruler, 1-based as Fresh counts them:
	// column c is the text cell c-1.
	Rulers []int
	// IndentGuides draws a guide at each indent level in the leading blanks.
	IndentGuides bool
	// VisibleWhitespace shows a space as a middle dot and a tab as an arrow.
	VisibleWhitespace bool
	// CurrentLine tints the text row of the caret line.
	CurrentLine bool
	// Occurrences paints the whole-word matches of the word at the caret. Nothing
	// is painted while text is selected.
	Occurrences bool
	// Diagnostics gives the diagnostic spans of line i. Nil paints none.
	Diagnostics func(line int) []DiagnosticSpan
	// TabSize is the columns per tab stop, for tab expansion, indent guides, visible
	// whitespace and indentation folds. The zero value is defaultTabSize.
	TabSize int
	// HideTabArrows draws a tab as blanks under VisibleWhitespace, without the arrow.
	HideTabArrows bool
	// HideFoldMarkers draws no fold arrows. The zero value shows them.
	HideFoldMarkers bool
	// Icons picks the glyphs of the fold arrows. The zero value is the plain set.
	Icons icons.Mode
	// HideVerticalScrollbar leaves the scrollbar column blank: the thumb and the track
	// are not drawn, and the change marks still are. The column is kept, so the text
	// keeps its width.
	HideVerticalScrollbar bool
	// HideHorizontalScrollbar draws no horizontal scrollbar, so the text takes the row
	// the bar would take. The first column can still move to keep the caret on screen.
	HideHorizontalScrollbar bool
	// CaretShape is how the caret cell is drawn. The zero value is CaretBlock.
	CaretShape CaretShape
}

// CaretShape is the look of the caret cell. The root shapes the terminal caret the
// same way.
type CaretShape int

// The caret shapes.
const (
	CaretBlock     CaretShape = iota // the cell is drawn in reverse video
	CaretBar                         // the cell shows a thin bar at its left edge
	CaretUnderline                   // the cell is underlined
)

// DiagnosticSpan is the run of rune columns [Start, End) of a line with a
// diagnostic of Severity. The severities are the LSP numbers: SeverityError to
// SeverityHint. A cell with two diagnostics takes the more severe one.
type DiagnosticSpan struct {
	Start, End int
	Severity   int
}

// The LSP severities of a diagnostic.
const (
	SeverityError   = 1
	SeverityWarning = 2
	SeverityInfo    = 3
	SeverityHint    = 4
)

// Highlight is the run of rune columns [Start, End) of a line painted with the
// search colors. Current marks the match the caret is on.
type Highlight struct {
	Start, End int
	Current    bool
}

// HitKind names what is under a pane cell.
type HitKind int

// The kinds a Hit can report.
const (
	HitNone           HitKind = iota // outside the body, the horizontal bar, or no document
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
	caretBarGlyph   = "▏" // the caret of CaretBar: a thin bar at the left edge of the cell
	thumbHGlyph     = "━" // horizontal scrollbar thumb
	trackHGlyph     = "─" // horizontal scrollbar track
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
	doc    Document
	styles Styles
	w, h   int // h is the number of text rows; the horizontal bar is not one
	lines  int
	top    int
	gutter int
	wrap   bool
	wrapW  int
	tab    int // the columns per tab stop
	// left is the first visible display column, always zero when wrap is on.
	left int
	// hbar is true when the last body row is the horizontal scrollbar, at row
	// barY. contentW is the widest visible line, which sizes the bar.
	hbar     bool
	barY     int
	contentW int
	overflow bool
	caret    buffer.Pos
	sels     []buffer.Selection // the primary selection and the extra ones
	extra    []buffer.Pos       // the positions of the extra carets
	bar      map[int]bool       // lines that get the margin bar
	scroll   map[int]bool       // lines that get the last-column change mark
	// highlights gives the search highlights of a line. Nil paints none.
	highlights func(line int) []Highlight
	// folds maps each fold start line to whether it is collapsed. hidden marks
	// the lines inside a collapsed fold. Both are nil without folds.
	folds  map[int]bool
	hidden []bool
	// occWord is the word whose occurrences are painted, or "".
	occWord string
	curLine bool
	rulers  []int
	guides  bool
	visWS   bool
	diags   func(line int) []DiagnosticSpan
	// The hidden parts of the pane (see Pane), and the caret shape.
	noTabArrows bool
	noFolds     bool
	icons       icons.Mode
	noVBar      bool
	noHBar      bool
	caretShape  CaretShape
}

// newFrame builds the frame for a w by h pane. It reports false when there is
// nothing to paint: no document, or an empty pane.
func (p Pane) newFrame(w, h int) (frame, bool) {
	if p.Doc == nil || w <= 0 || h <= 0 {
		return frame{}, false
	}
	lines := max(p.Doc.LineCount(), 1)
	f := frame{
		doc:        p.Doc,
		styles:     p.Styles,
		w:          w,
		h:          h,
		lines:      lines,
		gutter:     GutterWidth(lines),
		wrap:       p.Wrap,
		tab:        p.tabSize(),
		caret:      p.Doc.Cursor(),
		highlights: p.Highlights,
		curLine:    p.CurrentLine,
		rulers:     p.Rulers,
		guides:     p.IndentGuides,
		visWS:      p.VisibleWhitespace,
		diags:      p.Diagnostics,

		noTabArrows: p.HideTabArrows,
		noFolds:     p.HideFoldMarkers,
		icons:       p.Icons,
		noVBar:      p.HideVerticalScrollbar,
		noHBar:      p.HideHorizontalScrollbar,
		caretShape:  p.CaretShape,
	}
	if p.HideGutter {
		f.gutter = 0
	}
	f.wrapW = (w - f.gutter) - 2 // the legacy wrap width: text area less two
	f.setFolds(p.Doc)
	f.top = f.visibleAtOrAbove(min(max(p.Top, 0), lines-1))
	f.setSelections(p.Doc)
	if p.Occurrences && len(f.sels) == 0 && f.caret.Line < lines {
		f.occWord = wordAt([]rune(p.Doc.Line(f.caret.Line)), f.caret.Col)
	}
	f.placeHorizontal(p.Left, h)
	// The vertical overflow counts the text rows left after the horizontal bar.
	f.overflow = f.visibleCount() > f.h
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
// document is taller than h, and the horizontal scrollbar takes the last body
// row when a line is wider than the text area with Wrap off. The caret cell is
// drawn in reverse video when it is in the body; Caret returns the same
// position for the terminal cursor.
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
	f.paintRulers(g)
	f.paintMarks(g, rows)
	f.paintHorizontalBar(g)
	f.paintSecondaryCarets(g)
	f.paintCaret(g)
	return g
}

// paintCaret draws the caret cell in the frame's caret shape, so the caret shows even
// when the terminal cursor is hidden. The caret never takes the scrollbar column.
func (f frame) paintCaret(g *grid.Grid) {
	x, y, ok := f.cellOf(f.caret)
	if !ok || x < f.gutter || x >= f.w-1 {
		return
	}
	c := g.At(x, y)
	st := c.Style
	text := c.Text
	if text == "" {
		text = " "
	}
	switch f.caretShape {
	case CaretBar:
		text = caretBarGlyph
	case CaretUnderline:
		st.Underline = true
	default:
		st.Reverse = true
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
	for ; i < f.lines && y < f.h; i = f.next(i) {
		ln := f.layoutLine(i, true)
		cur := f.curLine && i == f.caret.Line
		for k, sp := range ln.spans {
			if y >= f.h {
				break
			}
			if f.gutter > 0 {
				label := fmt.Sprintf("%*s │ ", f.gutter-gutterSeparator, "")
				if k == 0 {
					label = fmt.Sprintf("%*d │ ", f.gutter-gutterSeparator, i+1)
				}
				g.Put(0, y, f.w-1, label, f.styles.Gutter)
			}
			cells := ln.cells[sp.from:sp.to]
			if f.left > 0 {
				cells = dropColumns(cells, f.left)
			}
			end := f.paintCells(g, y, cells, cur)
			if cur && f.styles.CurrentLine.BG != nil {
				g.Fill(end, f.w-1, y, grid.Style{FG: f.styles.Text.FG, BG: f.styles.CurrentLine.BG})
			}
			if k == 0 {
				// The marker is drawn after the text, so it never covers a text cell.
				f.paintFoldMarker(g, i, y, end)
			}
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

// paintCells writes one display row of cells from the gutter edge and returns
// the column after the last cell written. Runs of the same attributes are
// written together, and a zero-width mark joins the run before it. The last
// column is never text. cur is true on the caret line.
func (f frame) paintCells(g *grid.Grid, y int, cells []cell, cur bool) int {
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
		x += g.Put(x, y, maxX, b.String(), f.style(cells[start], cur))
		start = end
	}
	return x
}

// style is the grid style of one cell. Its base is the syntax color. A search
// match takes its background, or its whole style when it is the current match.
// An occurrence takes its background, and the caret line takes its tint, both
// under a match. The selection background goes over all of these. A whitespace
// cell takes the whitespace color, and a diagnostic takes its severity color
// and an underline.
func (f frame) style(c cell, cur bool) grid.Style {
	st := f.styles.Text
	if int(c.kind) < len(f.styles.Syntax) {
		st = f.styles.Syntax[c.kind]
	}
	switch {
	case c.match == matchCurrent:
		st = f.styles.CurrentMatch
	case c.match == matchOther:
		st.BG = f.styles.Match.BG
	case c.match == matchOccurrence && f.styles.Occurrence.BG != nil:
		st.BG = f.styles.Occurrence.BG
	case cur && f.styles.CurrentLine.BG != nil:
		st.BG = f.styles.CurrentLine.BG
	}
	if c.ws && f.styles.Whitespace.FG != nil {
		st.FG = f.styles.Whitespace.FG
	}
	if c.diag != 0 {
		if fg := f.diagnosticFG(c.diag); fg != nil {
			st.FG = fg
		}
		st.Underline = true
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
		if f.bar[r.line] && f.gutter > 0 {
			g.Set(0, r.y, changeBar, f.styles.Gutter)
		}
		switch {
		case f.scroll[r.line]:
			g.Set(last, r.y, changeScroll, f.styles.Gutter)
		case f.overflow && f.noVBar:
			// The column stays blank: the scrollbar is hidden.
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
	return f.cellOf(f.caret)
}

// BodyRows is the number of text rows of a w by h pane, which is h less the row the
// horizontal scrollbar takes. With Wrap on there is no bar, so it is h. The root
// sizes the editor viewport from it.
func (p Pane) BodyRows(w, h int) int {
	if p.Wrap {
		return max(h, 0)
	}
	f, ok := p.newFrame(w, h)
	if !ok {
		return max(h, 0)
	}
	return f.h
}

// RevealTop returns the top line that puts the caret's display row inside a w by h
// pane, counting the wrapped rows of each line. A caret already on screen keeps
// the pane's top. A caret above the window takes its own line. A caret below takes
// the fewest lines that fit above it, so the scroll is as short as it can be.
func (p Pane) RevealTop(w, h int) int {
	f, ok := p.newFrame(w, h)
	if !ok {
		return p.Top
	}
	return f.revealTop()
}

// tabSize is the tab width of the pane: TabSize, or the default when it is zero.
func (p Pane) tabSize() int {
	if p.TabSize > 0 {
		return p.TabSize
	}
	return defaultTabSize
}

// revealTop is the top RevealTop returns for the frame.
func (f frame) revealTop() int {
	c := f.caret.Line
	if c < 0 || c >= f.lines || (f.hidden != nil && f.hidden[c]) {
		return f.top
	}
	if c < f.top {
		return c
	}
	if _, _, ok := f.cellOf(f.caret); ok {
		return f.top
	}
	// The caret is below the body. Count the rows from the caret's line up to the
	// caret's own row, then add the lines above it while they fit.
	ln := f.layoutLine(c, false)
	k, _ := caretInSpans(ln.geom, ln.spans, expandedIndex([]rune(f.doc.Line(c)), f.caret.Col, f.tab))
	rows, top := k+1, c
	for prev := f.prevLine(top); prev >= f.top; prev = f.prevLine(top) {
		n := len(f.layoutLine(prev, false).spans)
		if rows+n > f.h {
			break
		}
		rows += n
		top = prev
	}
	return top
}

// cellOf finds the cell of a document position by walking the rows from the
// top line. ok is false when the position is above the window, below the body,
// or on a line hidden by a fold.
func (f frame) cellOf(p buffer.Pos) (x, y int, ok bool) {
	for i := f.top; i < f.lines && y < f.h; i = f.next(i) {
		ln := f.layoutLine(i, false)
		if i == p.Line {
			raw := []rune(f.doc.Line(i))
			k, cells := caretInSpans(ln.geom, ln.spans, expandedIndex(raw, p.Col, f.tab))
			y += k
			return f.gutter + cells - f.left, y, y < f.h
		}
		y += len(ln.spans)
	}
	return 0, 0, false
}

// Hit maps a pane-local cell to what it shows. A cell outside the body, a cell
// on the horizontal scrollbar row, or a cell in a pane with no document, is
// HitNone. The last column is the scrollbar when the document overflows, and
// the text otherwise.
func (p Pane) Hit(w, h, x, y int) Hit {
	f, ok := p.newFrame(w, h)
	if !ok || x < 0 || y < 0 || x >= w || y >= h || y >= f.h {
		return Hit{Kind: HitNone}
	}
	switch {
	case x < f.gutter:
		return Hit{Kind: HitGutter, Pos: buffer.Pos{Line: f.rowAt(y).line}, Row: y}
	case f.overflow && !f.noVBar && x == w-1:
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
		cells += x - f.gutter + f.left
		if !r.last && cells >= r.endCells() {
			// A click past the end of a row that is not the last one lands on its
			// last character, not on the first character of the next row.
			cells = r.endCells() - 1
		}
	}
	return buffer.Pos{Line: r.line, Col: runeIndexAt([]rune(f.doc.Line(r.line)), cells, f.tab)}
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
