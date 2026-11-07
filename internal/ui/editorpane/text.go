package editorpane

import (
	"fmt"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
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
	// tail is the look of the row past the end of the text, when an overlay gives it one.
	tail    grid.Style
	hasTail bool
}

// layoutLine lays out buffer line i. styled also resolves the syntax kinds and
// the selection per cell, which needs the line's spans, so the hit and caret
// paths pass false and skip the lexer.
func (f frame) layoutLine(i int, styled bool) line {
	var text string
	var off int
	long := f.isLong(i)
	if long {
		w := f.window(i)
		text, off = w.text, w.off
	} else {
		text = f.doc.Line(i)
	}
	raw := []rune(text)
	plain := expandCells(raw, nil, nil, f.tab)
	geom := make([]rune, len(plain))
	glue := make([]bool, len(plain))
	for j, c := range plain {
		geom[j], glue[j] = c.r, c.glue
	}
	ln := line{geom: geom, spans: []span{{0, len(geom)}}, off: off, long: long}
	if f.wrap {
		ln.spans, ln.indent = wrapHanging(geom, f.wrapW, f.fx.hang, glue)
	}
	if styled {
		// The spans of the syntax are byte offsets into the whole line, so a window has
		// none.
		var spans []syntax.Span
		if !long {
			spans = f.doc.LineSpans(i)
		}
		kinds := kindsPerRune(text, spans)
		// The helpers below read the window's offset from the frame.
		f.off, f.cut = off, long && len(raw) >= longWindow
		ln.cells = expandCells(raw, kinds, f.selectedIn(i), f.tab)
		f.markOccurrences(i, raw, ln.cells)
		f.markMatches(i, raw, ln.cells)
		f.markDiagnostics(i, raw, ln.cells)
		f.markOverlays(i, raw, &ln)
		f.decorate(i, raw, kinds, &ln)
	}
	return ln
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
	last  bool // the row is the last row of its line
	valid bool // the row is inside the document
}

// startCells is the display column of the row's first cell, measured from the
// start of the line.
func (r displayRow) startCells() int {
	return ansi.StringWidth(string(r.ln.geom[:r.span.from]))
}

// endCells is the display column just past the row's last cell.
func (r displayRow) endCells() int {
	return ansi.StringWidth(string(r.ln.geom[:r.span.to]))
}

// rowAt maps body row row (0 is the top body row) to the display row shown there.
// Rows count from the top line through the wrapped lines. A row above the body
// maps to a line above the window, and a row past the document maps to the last
// row of the last line with valid false.
func (f frame) rowAt(row int) displayRow {
	last := f.lines - 1
	if row < 0 {
		i := min(max(f.top+row, 0), last)
		ln := f.layoutLine(i, false)
		return displayRow{line: i, ln: ln, span: ln.spans[len(ln.spans)-1], last: true, valid: true}
	}
	k := row
	for i := f.top; i < f.lines; i = f.next(i) {
		ln := f.layoutLine(i, false)
		if k < len(ln.spans) {
			return displayRow{line: i, ln: ln, span: ln.spans[k], last: k == len(ln.spans)-1, valid: true}
		}
		k -= len(ln.spans)
	}
	ln := f.layoutLine(last, false)
	return displayRow{line: last, ln: ln, span: ln.spans[len(ln.spans)-1], last: true, valid: false}
}
