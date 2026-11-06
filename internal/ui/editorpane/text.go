package editorpane

import (
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/syntax"
)

// defaultTabSize is the tab width in cells when Pane.TabSize is zero.
const defaultTabSize = 4

// cell is one painted rune of an expanded line, with the attributes it takes.
// A tab becomes one cell per blank it covers, and a carriage return has none.
type cell struct {
	r     rune
	kind  syntax.Kind
	sel   bool      // inside the selection
	match matchKind // inside a search match or an occurrence
	ws    bool      // a visible whitespace mark or an indent guide: whitespace color
	diag  int       // the severity of the most severe diagnostic here, or 0
}

// sameAttr reports whether two cells paint with the same attributes.
func (c cell) sameAttr(o cell) bool {
	return c.kind == o.kind && c.sel == o.sel && c.match == o.match && c.ws == o.ws && c.diag == o.diag
}

// line is one buffer line laid out for painting. geom holds the expanded runes
// (tabs as blanks, no carriage returns) that wrap and caret positions index.
// cells is the same sequence with paint attributes, and it is set only when the
// line is laid out for painting. spans are the display rows of the line.
type line struct {
	geom  []rune
	cells []cell
	spans []span
}

// layoutLine lays out buffer line i. styled also resolves the syntax kinds and
// the selection per cell, which needs the line's spans, so the hit and caret
// paths pass false and skip the lexer.
func (f frame) layoutLine(i int, styled bool) line {
	text := f.doc.Line(i)
	raw := []rune(text)
	plain := expandCells(raw, nil, nil, f.tab)
	geom := make([]rune, len(plain))
	for j, c := range plain {
		geom[j] = c.r
	}
	ln := line{geom: geom, spans: []span{{0, len(geom)}}}
	if f.wrap {
		ln.spans = wrapSpans(geom, f.wrapW)
	}
	if styled {
		kinds := kindsPerRune(text, f.doc.LineSpans(i))
		ln.cells = expandCells(raw, kinds, f.selectedIn(i), f.tab)
		f.markOccurrences(raw, ln.cells)
		f.markMatches(i, raw, ln.cells)
		f.markDiagnostics(i, raw, ln.cells)
		f.markWhitespace(text, ln.cells)
	}
	return ln
}

// selectedIn returns the test for a rune of line i being in the selection, or
// nil when there is no selection.
func (f frame) selectedIn(i int) func(col int) bool {
	if len(f.sels) == 0 {
		return nil
	}
	return func(col int) bool {
		p := buffer.Pos{Line: i, Col: col}
		for _, s := range f.sels {
			from, to := s.Ordered()
			if !p.Less(from) && p.Less(to) {
				return true
			}
		}
		return false
	}
}

// expandCells expands raw runes the way the legacy painter does. A tab becomes
// the blanks up to the next tab stop of tab cells, and a carriage return is
// dropped. A blank inside the selection is drawn as a middle dot. kinds and sel
// may be nil.
func expandCells(raw []rune, kinds []syntax.Kind, sel func(col int) bool, tab int) []cell {
	out := make([]cell, 0, len(raw))
	col := 0
	for j, r := range raw {
		k := syntax.KindPlain
		if j < len(kinds) {
			k = kinds[j]
		}
		selected := sel != nil && sel(j)
		switch r {
		case '\t':
			n := cellAdvance(col, r, tab)
			for range n {
				out = append(out, cell{r: ' ', kind: k, sel: selected})
			}
			col += n
		case '\r':
		default:
			if selected && r == ' ' {
				r = selectedBlank
			}
			out = append(out, cell{r: r, kind: k, sel: selected})
			col += cellAdvance(col, r, tab)
		}
	}
	return out
}

// cellAdvance is how many cells rune r takes when it starts at display column
// col. A tab runs to the next stop of tab cells, a carriage return takes none,
// and any other rune takes its display width.
func cellAdvance(col int, r rune, tab int) int {
	switch r {
	case '\t':
		return tab - col%tab
	case '\r':
		return 0
	}
	return ansi.StringWidth(string(r))
}

// isZeroWidth reports whether r takes no cell of its own, such as a combining
// mark. It attaches to the cell before it.
func isZeroWidth(r rune) bool {
	return ansi.StringWidth(string(r)) == 0
}

// expandedIndex maps rune index col of raw to its index in the expanded runes.
// A tab is one index per blank it covers, and a carriage return has none.
func expandedIndex(raw []rune, col, tab int) int {
	out, cells := 0, 0
	for i, r := range raw {
		if i >= col {
			break
		}
		n := cellAdvance(cells, r, tab)
		cells += n
		switch r {
		case '\r':
		case '\t':
			out += n
		default:
			out++
		}
	}
	return out
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
