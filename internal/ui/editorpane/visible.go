package editorpane

import (
	"sort"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// RowSpan is one text row the pane paints: the buffer line, the part of it the row
// shows, and whether the row continues a soft-wrapped line.
type RowSpan struct {
	Line int
	// From and To are byte columns of the line text, [From, To). A rune belongs to the
	// row where its first cell is drawn, so the rows of one wrapped line split the line
	// without gaps or overlap. With Wrap off the span is the part of the line inside the
	// horizontal window. A long line of a large document is laid out as a window of runes
	// (see window in large.go); its columns are still bytes from the start of the line.
	From, To int
	Wrapped  bool
}

// VisibleRows lists the text rows of a w by h pane from the top row down, at most limit
// of them. It walks the document as paintText does, with the same fold-hidden lines
// skipped and the same wrapped rows, so row k of the result is body row k of Render. Body
// rows past the end of the document ("~" rows) are not listed.
func (p Pane) VisibleRows(w, h, limit int) []RowSpan {
	f, ok := p.newFrame(w, h)
	if !ok {
		return nil
	}
	textW := f.w - 1 - f.gutter
	var out []RowSpan
	y := 0
	for i := f.top; i < f.lines && y < f.h && len(out) < limit; i = f.next(i) {
		ln := f.layoutLine(i, false)
		raw, _ := f.lineRaw(i)
		starts := runeStarts(raw, f.tab)
		bytes := byteStarts(raw)
		base := 0
		if ln.long && ln.off > 0 {
			// The window starts at a rune column; the row's columns are bytes of the line.
			base = len(f.win.LineSlice(i, 0, ln.off))
		}
		col := func(exp int) int {
			j := sort.SearchInts(starts, exp)
			return base + bytes[j]
		}
		for k, sp := range ln.spans {
			if y >= f.h || len(out) >= limit {
				break
			}
			from, to := sp.from, sp.to
			if !f.wrap {
				from, to = visibleCells(ln.geom, f.dropFor(ln), textW)
			}
			out = append(out, RowSpan{Line: i, From: col(from), To: col(to), Wrapped: k > 0})
			y++
		}
	}
	return out
}

// View returns the number of text rows of a w by h pane (BodyRows) and the first visible
// display column (HScroll) from one layout of the pane.
func (p Pane) View(w, h int) (rows, left int) {
	f, ok := p.newFrame(w, h)
	if !ok {
		return max(h, 0), 0
	}
	return f.h, f.left
}

// visibleCells returns the expanded cells [from, to) of geom that a row shows when the
// first drop display columns are scrolled off and the text area is width columns wide.
// A cell that straddles the left cut is dropped with the columns before it, and a cell
// that does not fit on the right is not drawn.
func visibleCells(geom []rune, drop, width int) (from, to int) {
	for from < len(geom) && drop > 0 {
		drop -= ansi.StringWidth(string(geom[from]))
		from++
	}
	to = from
	for to < len(geom) {
		cw := ansi.StringWidth(string(geom[to]))
		if cw > width {
			break
		}
		width -= cw
		to++
	}
	return from, to
}

// runeStarts returns, for each rune of raw, the index in the expanded runes of the line
// where the rune starts, and one more entry for the end of the line. A tab covers one
// index per blank and a control character the four of its <XX> escape, as expandCells
// lays them out.
func runeStarts(raw []rune, tab int) []int {
	starts := make([]int, len(raw)+1)
	idx, cells := 0, 0
	for j, r := range raw {
		starts[j] = idx
		n := cellAdvance(cells, r, tab)
		cells += n
		if r == '\t' || controlRune(r) {
			idx += n
		} else {
			idx++
		}
	}
	starts[len(raw)] = idx
	return starts
}

// byteStarts returns the byte offset of each rune of raw, and the byte length at the end.
func byteStarts(raw []rune) []int {
	out := make([]int, len(raw)+1)
	for j, r := range raw {
		out[j+1] = out[j] + utf8.RuneLen(r)
	}
	return out
}
