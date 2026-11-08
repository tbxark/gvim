package editorpane

import (
	"fmt"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
)

// defaultTabSize is the tab width in cells when Pane.TabSize is zero.
const defaultTabSize = 4

// cell is one painted rune of an expanded line, with the attributes it takes.
// A tab becomes one cell per blank it covers, and a control character becomes
// the four cells of its <XX> escape.
type cell struct {
	r     rune
	kind  syntax.Kind
	sel   bool      // inside the selection
	match matchKind // inside a search match or an occurrence
	ws    bool      // a visible whitespace mark or a line-end mark: whitespace color
	diag  int       // the severity of the most severe diagnostic here, or 0
	blank blankKind // what blank of the text the cell is, for the whitespace marks and guides
	mark  markKind  // an indent guide or a bracket color
	lvl   uint8     // the rainbow level of the mark
	pair  bool      // a bracket of the pair at the caret
	glue  bool      // joined to the next cell: a cell of a control escape but the last
	dec   uint16    // the look an overlay gives the cell: an index into overlayFX.looks plus one, or 0
	rep   uint16    // the text an overlay draws instead of the rune: repHidden, an index into overlayFX.reps plus two, or 0
}

// blankKind says whether a cell is a blank of the text: a space, the first cell of a
// tab, or a later cell of a tab.
type blankKind uint8

const (
	blankNone blankKind = iota
	blankSpace
	blankTab
	blankTabPad
)

// markKind is the decoration that sets the foreground of a cell.
type markKind uint8

const (
	markNone    markKind = iota
	markGuide            // an indent guide
	markBracket          // a bracket colored by its depth
)

// sameAttr reports whether two cells paint with the same attributes.
func (c cell) sameAttr(o cell) bool {
	return c.kind == o.kind && c.sel == o.sel && c.match == o.match && c.ws == o.ws && c.diag == o.diag &&
		c.mark == o.mark && c.lvl == o.lvl && c.pair == o.pair && c.dec == o.dec && c.rep == o.rep && c.rep < 2
}

// line is one buffer line laid out for painting. geom holds the expanded runes
// (tabs as blanks, control characters as escapes) that wrap and caret positions
// index. cells is the same sequence with paint attributes, followed by the cells
// that sit past the text (line-end marks, guides through a blank line), and it is
// set only when the line is laid out for painting. spans are the display rows of the
// line. indent is the width of the hanging indent of the rows after the first, and
// pad the cells that fill it.
//
// A long line of a large document is laid out as a window of its runes, from column
// off on (see window in large.go). Everything the line holds is then relative to the
// window, and long is set.
type line struct {
	geom   []rune
	cells  []cell
	spans  []span
	indent int
	pad    []cell
	off    int
	long   bool
	// raw is the runes of the line, or of its window.
	raw []rune
	// cont is set for a segment of a tall line that continues it, whose first row has the
	// hanging indent. cutEnd says that the window ends before its line does.
	cont, cutEnd bool
	// tail is the look of the row past the end of the text, when an overlay gives it one.
	tail    grid.Style
	hasTail bool
	// Page view mapping: srcToDisp maps a source rune column to the composed rune
	// column, and dispToSrc the reverse. They are nil outside the composed page view.
	srcToDisp []int
	dispToSrc []int
}

// layoutKey is what the layout of a line without paint attributes depends on besides
// the text of the line: the wrap geometry and the tab stops.
type layoutKey struct {
	line, wrapW, tab int
	wrap, hang       bool
}

// LayoutMemo is implemented by a Document that keeps values until its text changes, as
// *editor.Document does. The pane keeps the layout of a line there, so a key press that
// asks about the caret, the rows and the hit map in several frames lays out a long
// line once. MemoValue returns the value kept for key, or stores and returns what
// compute makes.
type LayoutMemo interface {
	MemoValue(key any, compute func() any) any
}

// layoutLine lays out buffer line i. styled also resolves the syntax kinds and
// the selection per cell, which needs the line's spans, so the hit and caret
// paths pass false and skip the lexer. A line laid out without attributes is kept
// for the frame, and for the text of the document when it has a LayoutMemo, because the
// caret, the rows and the hit map ask for the same lines again and again.
func (f frame) layoutLine(i int, styled bool) line {
	if styled || f.isLong(i) {
		return f.layout(i, styled)
	}
	if ln, ok := f.fx.plain[i]; ok {
		return ln
	}
	var ln line
	if lm, ok := f.doc.(LayoutMemo); ok && !isLarge(f.doc) {
		key := layoutKey{line: i, wrapW: f.wrapW, tab: f.tab, wrap: f.wrap, hang: f.fx.hang}
		ln = lm.MemoValue(key, func() any { return f.layout(i, false) }).(line)
	} else {
		ln = f.layout(i, false)
	}
	if f.fx.plain == nil {
		f.fx.plain = make(map[int]line)
	}
	f.fx.plain[i] = ln
	return ln
}

// layout is layoutLine without the memo.
func (f frame) layout(i int, styled bool) line {
	if f.pageOn && f.pageMarkdown {
		return f.layoutCompose(i, styled)
	}
	var text string
	var off int
	long := f.isLong(i)
	if long {
		w := f.window(i)
		text, off = w.text, w.off
	} else {
		text = f.doc.Line(i)
	}
	return f.layoutText(i, text, off, long, nil, styled)
}

// layoutText lays out text, the runes of line i from column off on. A window of a long line
// is long. A segment of a tall line, which has its own rows, is described by seg.
func (f frame) layoutText(i int, text string, off int, long bool, seg *segInfo, styled bool) line {
	raw := []rune(text)
	plain := expandCells(raw, nil, nil, f.tab)
	geom := make([]rune, len(plain))
	glue := make([]bool, len(plain))
	for j, c := range plain {
		geom[j], glue[j] = c.r, c.glue
	}
	ln := line{geom: geom, raw: raw, spans: []span{{0, len(geom)}}, off: off, long: long}
	switch {
	case seg != nil:
		ln.spans = wrapRowsFrom(geom, f.wrapW, seg.indent, glue, seg.cont)
		ln.indent, ln.cont = seg.indent, seg.cont
	case f.wrap:
		ln.spans, ln.indent = wrapHanging(geom, f.wrapW, f.fx.hang, glue)
	}
	cut := long && len(raw) >= longWindow
	if seg != nil {
		cut = seg.cut
	}
	ln.cutEnd = cut
	if styled {
		// The spans of the syntax are byte offsets into the whole line, so a window or a
		// segment of it has none of those. The document may lex its text from the text around it.
		var spans []syntax.Span
		if long {
			spans = f.segmentSpans(i, off, text)
		} else {
			spans = f.doc.LineSpans(i)
		}
		kinds := kindsPerRune(text, spans)
		// The helpers below read the window's offset from the frame.
		f.off, f.cut = off, cut
		ln.cells = expandCells(raw, kinds, f.selectedIn(i), f.tab)
		f.markOccurrences(i, raw, ln.cells)
		f.markMatches(i, raw, ln.cells)
		f.markDiagnostics(i, raw, ln.cells)
		f.markOverlays(i, raw, &ln)
		f.decorate(i, raw, kinds, &ln)
	}
	return ln
}

// SpanWindower is implemented by a Document that lexes a piece of a very long line. The pane
// asks for the syntax spans of the text of a segment of a tall line, which is runes [col, ...)
// of line i. The spans are byte offsets into text. A document without it paints such a line
// without syntax colors.
type SpanWindower interface {
	LineSpansIn(i, col int, text string) []syntax.Span
}

// segmentSpans returns the syntax spans of the text of a segment of line i, which starts at
// rune column col.
func (f frame) segmentSpans(i, col int, text string) []syntax.Span {
	if sw, ok := f.doc.(SpanWindower); ok {
		return sw.LineSpansIn(i, col, text)
	}
	return nil
}

// selectedIn returns the test for a rune of line i being in the selection, or
// nil when there is no selection.
func (f frame) selectedIn(i int) func(col int) bool {
	if len(f.sels) == 0 {
		return nil
	}
	// Only the selections that reach line i can hold a rune of it. With thousands of
	// carets that is a few, and each rune is tested against those only.
	var here []buffer.Selection
	for _, s := range f.sels {
		if from, to := s.Ordered(); from.Line <= i && i <= to.Line {
			here = append(here, s)
		}
	}
	if len(here) == 0 {
		return nil
	}
	return func(col int) bool {
		p := buffer.Pos{Line: i, Col: col + f.off}
		for _, s := range here {
			from, to := s.Ordered()
			if !p.Less(from) && p.Less(to) {
				return true
			}
		}
		return false
	}
}

// expandCells expands raw runes the way the legacy painter does. A tab becomes
// the blanks up to the next tab stop of tab cells, and a control character
// becomes its <XX> escape. kinds and sel may be nil.
func expandCells(raw []rune, kinds []syntax.Kind, sel func(col int) bool, tab int) []cell {
	out := make([]cell, 0, len(raw))
	col := 0
	for j, r := range raw {
		k := syntax.KindPlain
		if j < len(kinds) {
			k = kinds[j]
		}
		selected := sel != nil && sel(j)
		switch {
		case r == '\t':
			n := cellAdvance(col, r, tab)
			for q := range n {
				b := blankTabPad
				if q == 0 {
					b = blankTab
				}
				out = append(out, cell{r: ' ', kind: k, sel: selected, blank: b})
			}
			col += n
		case controlRune(r):
			esc := controlEscape(r)
			for q, e := range esc {
				out = append(out, cell{r: e, kind: k, sel: selected, glue: q < len(esc)-1})
			}
			col += controlWidth
		default:
			c := cell{r: r, kind: k, sel: selected}
			if r == ' ' {
				c.blank = blankSpace
			}
			out = append(out, c)
			col += cellAdvance(col, r, tab)
		}
	}
	return out
}

// cellAdvance is how many cells rune r takes when it starts at display column
// col. A tab runs to the next stop of tab cells, a control character takes the
// width of its <XX> escape, and any other rune takes its display width.
func cellAdvance(col int, r rune, tab int) int {
	switch {
	case r == '\t':
		return tab - col%tab
	case controlRune(r):
		return controlWidth
	case r < 0x7f: // printable ASCII, the common case, with no string to build
		return 1
	}
	return ansi.StringWidth(string(r))
}

// controlWidth is the width of the <XX> escape of a control character.
const controlWidth = 4

// controlRune reports whether r is drawn as an <XX> escape: the C0 controls except
// the tab and the line feed, and DEL. Fresh lets ESC through for ANSI text, but gvim
// has no interpreter for escapes in the document, so ESC is escaped as well.
func controlRune(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f
}

// controlEscape is the text a control character is drawn as, such as "<00>".
func controlEscape(r rune) string {
	return fmt.Sprintf("<%02X>", r)
}

// isZeroWidth reports whether r takes no cell of its own, such as a combining
// mark. It attaches to the cell before it.
func isZeroWidth(r rune) bool {
	return ansi.StringWidth(string(r)) == 0
}

// expandedIndex maps rune index col of raw to its index in the expanded runes.
// A tab is one index per blank it covers, and a control character four.
func expandedIndex(raw []rune, col, tab int) int {
	out, cells := 0, 0
	for i, r := range raw {
		if i >= col {
			break
		}
		n := cellAdvance(cells, r, tab)
		cells += n
		switch {
		case r == '\t' || controlRune(r):
			out += n
		default:
			out++
		}
	}
	return out
}

// expandedTable returns expandedIndex(raw, col, tab) for every col of 0..len(raw), in one
// pass over raw, for a caller that asks about many columns of one line.
func expandedTable(raw []rune, tab int) []int {
	out := make([]int, len(raw)+1)
	n, cells := 0, 0
	for i, r := range raw {
		out[i] = n
		adv := cellAdvance(cells, r, tab)
		cells += adv
		if r == '\t' || controlRune(r) {
			n += adv
		} else {
			n++
		}
	}
	out[len(raw)] = n
	return out
}

// lookup is expandedIndex(raw, col, tab) read from a table made by expandedTable.
func lookup(table []int, col int) int {
	return table[min(max(col, 0), len(table)-1)]
}

// runeIndexAt returns the rune index of the character that covers display
// column col of raw. A column past the end gives the rune count.
func runeIndexAt(raw []rune, col, tab int) int {
	width := 0
	for i, r := range raw {
		w := cellAdvance(width, r, tab)
		if width+w > col {
			return i
		}
		width += w
	}
	return len(raw)
}

// kindsPerRune gives the syntax kind of each rune of text, from its spans. Spans
// use byte offsets, so each rune is matched by its starting byte. A rune no span
// covers is plain.
func kindsPerRune(text string, spans []syntax.Span) []syntax.Kind {
	kinds := make([]syntax.Kind, 0, utf8.RuneCountInString(text))
	si := 0
	for off := range text {
		for si < len(spans) && spans[si].End <= off {
			si++
		}
		k := syntax.KindPlain
		if si < len(spans) && spans[si].Start <= off {
			k = spans[si].Kind
		}
		kinds = append(kinds, k)
	}
	return kinds
}

// displayRow is the display row a body row maps to: its buffer line, the line's
// geometry, and the span of the row within it.
type displayRow struct {
	line  int
	ln    line
	span  span
	first bool // the row is the first row of its line
	last  bool // the row is the last row of its line
	valid bool // the row is inside the document
	// filler says the row is a filler row of the line, and not one of its text rows.
	// span is then the line's first span, and last is true.
	filler filler
}

// startCells is the display column of the row's first cell, measured from the
// start of the line, or of its segment when the line is tall.
func (r displayRow) startCells() int {
	return ansi.StringWidth(string(r.ln.geom[:r.span.from]))
}

// endCells is the display column just past the row's last cell.
func (r displayRow) endCells() int {
	return ansi.StringWidth(string(r.ln.geom[:r.span.to]))
}

// displayOf is the display row of rd, a row of line i.
func displayOf(i int, rd rowDesc, valid bool) displayRow {
	return displayRow{line: i, ln: rd.ln, span: rd.sp, first: rd.first, last: rd.last, valid: valid}
}

// lastRowOf is the last display row of line i.
func (f frame) lastRowOf(i int) rowDesc {
	if f.isTall(i) {
		rows := f.tallRows(i, false, f.runeLen(i), 1)
		return rows[0]
	}
	ln := f.layoutLine(i, false)
	k := len(ln.spans) - 1
	return rowDesc{ln: ln, k: k, sp: ln.spans[k], first: k == 0, last: true}
}

// rowAt maps body row row (0 is the top body row) to the display row shown there.
// Rows count from the top line through the wrapped lines. A row above the body
// maps to a line above the window, and a row past the document maps to the last
// row of the last line with valid false.
func (f frame) rowAt(row int) displayRow {
	if f.fill != nil {
		return f.rowAtFilled(row)
	}
	last := f.lines - 1
	if row < 0 && !f.isTall(f.top) && f.topRow+row >= 0 {
		// A row of the top line that is scrolled off above the body.
		rows := f.lineRows(f.top, false, 0, f.topRow+row+1)
		return displayOf(f.top, rows[f.topRow+row], true)
	}
	if row < 0 {
		i := min(max(f.top+row, 0), last)
		return displayOf(i, f.lastRowOf(i), true)
	}
	k := row
	for i := f.top; i < f.lines; i = f.next(i) {
		anchor := 0
		if i == f.top {
			// The rows of the top line that are scrolled off, or the column its view starts at.
			anchor = f.topRow
		}
		rows := f.lineRows(i, false, anchor, k+1)
		if k < len(rows) {
			return displayOf(i, rows[k], true)
		}
		k -= len(rows)
	}
	return displayOf(last, f.lastRowOf(last), false)
}
