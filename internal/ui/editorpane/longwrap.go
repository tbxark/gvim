package editorpane

import (
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
)

// Wrap of very long lines. A line of tens of thousands of runes wrapped in one piece takes
// time in proportion to its length on every key typed in it, and its rows are numbered
// from its start, so the caret at the end is a million rows down. A tall line is laid out
// in segments instead: pieces of segmentRunes runes, which start at multiples of that
// length, backed up to the space before it when there is one in segmentLookback runes. Each
// segment is wrapped by itself, with the rows after the first one hanging as the line's
// indent says, and its first row counts as a continuation row. Where a segment ends, a row
// ends, so a row there can be shorter than the others, as a row before a long word is.
//
// The layout of a segment depends on the text of the segment, and its edges on the text
// next to them, so nothing is kept across frames: what a frame needs is the segments around
// the rows it shows, one or two. A row is named by the rune column where it starts, since
// its number from the line start is not known without laying out all that is before it. The
// view of a tall top line (Pane.TopRow) is such a column.

const (
	// segmentRunes is the length of the pieces of a tall line.
	segmentRunes = 4096
	// segmentLookback is how far before a multiple of segmentRunes a segment may end, to end
	// after a space.
	segmentLookback = 48
	// tallRows is how many rows a tall line counts for in the sums that tell whether the
	// lines fit the body: more than any body.
	tallRows = 1 << 24
)

// segInfo says where a segment lies in its line.
type segInfo struct {
	index      int  // the number of the segment
	first, end int  // the rune columns [first, end) of the line it holds
	cont       bool // it continues the line: its first row is a continuation row
	cut        bool // the line goes on after it
	indent     int  // the hanging indent of the rows after the first row of the line
}

// segment is a laid out piece of a tall line. ln holds the cells and rows of the piece, with
// columns relative to its first rune (ln.off). cols are the rune columns of the line at which
// its rows start, and then the column where it ends.
type segment struct {
	segInfo
	ln   line
	cols []int
}

// segKey names a segment in the frame's memo.
type segKey struct {
	line, seg int
	styled    bool
}

// runeLen is the number of runes of line i. A document in memory counts them in no time. For
// a document that cannot, the stored length of the line is the bound the window gives.
func (f frame) runeLen(i int) int {
	if ll, ok := f.doc.(lineLener); ok {
		return ll.LineLen(i)
	}
	return f.win.LineBytes(i)
}

// isTall reports whether line i is wrapped in segments: the pane wraps, the document is read
// through windows and the line is longer than LongLineRunes.
func (f frame) isTall(i int) bool {
	if !f.wrap || f.win == nil {
		return false
	}
	if tall, ok := f.fx.tall[i]; ok {
		return tall
	}
	tall := f.win.LineBytes(i) > LongLineRunes
	if f.fx.tall == nil {
		f.fx.tall = make(map[int]bool)
	}
	f.fx.tall[i] = tall
	return tall
}

// segBound is the rune column where segment j of line i starts, for a line of n runes: the
// end of segment j-1, and n for the segment after the last.
func (f frame) segBound(i, j, n int) int {
	if j <= 0 {
		return 0
	}
	pos := j * segmentRunes
	if pos >= n {
		return n
	}
	back := []rune(f.win.LineSlice(i, pos-segmentLookback, segmentLookback))
	for k := len(back) - 1; k >= 0; k-- {
		if back[k] == ' ' {
			return pos - len(back) + k + 1
		}
	}
	return pos
}

// segmentAt is the number of the segment of line i that holds rune column col, for a line of n
// runes. A column at the end of the line is in the last one.
func (f frame) segmentAt(i, col, n int) int {
	count := max((n+segmentRunes-1)/segmentRunes, 1)
	j := min(max(col, 0)/segmentRunes, count-1)
	switch {
	case j > 0 && col < f.segBound(i, j, n):
		j--
	case j+1 < count && col >= f.segBound(i, j+1, n):
		j++
	}
	return j
}

// tallIndent is the hanging indent of the rows of tall line i after the first: the blanks it
// starts with, as wrapHanging finds it, or none when the option is off or the rows would be
// left under minContinuation cells.
func (f frame) tallIndent(i int) int {
	if !f.fx.hang {
		return 0
	}
	indent := 0
	for _, r := range f.win.LineSlice(i, 0, f.wrapW) {
		if r != ' ' {
			break
		}
		indent++
	}
	if indent+minContinuation > f.wrapW {
		return 0
	}
	return indent
}

// segment lays out segment j of tall line i, or takes it from the frame, which keeps the
// segments it laid out. styled is as in layoutLine.
func (f frame) segment(i, j int, styled bool) *segment {
	key := segKey{line: i, seg: j, styled: styled}
	if s, ok := f.fx.segs[key]; ok {
		return s
	}
	n := f.runeLen(i)
	first, end := f.segBound(i, j, n), f.segBound(i, j+1, n)
	text := f.win.LineSlice(i, first, end-first)
	info := segInfo{index: j, first: first, end: end, cont: first > 0, cut: end < n, indent: f.tallIndent(i)}
	ln := f.layoutText(i, text, first, true, &info, styled)
	s := &segment{segInfo: info, ln: ln}
	// The rune column of each row start, found by walking the runes once.
	s.cols = make([]int, len(ln.spans)+1)
	table := expandedTable(ln.raw, f.tab)
	col := 0
	for k, sp := range ln.spans {
		for col < len(ln.raw) && table[col] < sp.from {
			col++
		}
		s.cols[k] = first + col
	}
	s.cols[len(ln.spans)] = end
	if f.fx.segs == nil {
		f.fx.segs = make(map[segKey]*segment)
	}
	f.fx.segs[key] = s
	return s
}

// rowIndex is the index of the row of s that holds rune column col. A column at the end of the
// segment is on its last row.
func (s *segment) rowIndex(col int) int {
	k, _ := slices.BinarySearch(s.cols[:len(s.cols)-1], col+1)
	return max(k-1, 0)
}

// rowDesc is one display row of a line, with what painting and mapping it needs. For a line
// that is not tall, ln is the layout of the line and k the row's index in it.
type rowDesc struct {
	ln          line
	k           int
	sp          span
	first, last bool // the first and the last row of the buffer line
	// start is the rune column where the row starts, when the line is tall.
	start int
}

// cont reports whether the row continues its line, so that it has the hanging indent.
func (r rowDesc) cont() bool { return !r.first }

// lineRows returns the display rows of line i, at most limit of them. The first one is the row
// that anchor names: for a line that is not tall, anchor is the number of rows to skip, for a
// tall one the rune column of a row, which may be any column of it. styled is as in layoutLine.
func (f frame) lineRows(i int, styled bool, anchor, limit int) []rowDesc {
	if f.isTall(i) {
		return f.tallRows(i, styled, anchor, limit)
	}
	ln := f.layoutLine(i, styled)
	var out []rowDesc
	for k := min(max(anchor, 0), len(ln.spans)-1); k < len(ln.spans) && len(out) < limit; k++ {
		out = append(out, rowDesc{ln: ln, k: k, sp: ln.spans[k], first: k == 0, last: k == len(ln.spans)-1})
	}
	return out
}

// tallRows returns the rows of tall line i from the row that holds rune column col, at most
// limit of them.
func (f frame) tallRows(i int, styled bool, col, limit int) []rowDesc {
	n := f.runeLen(i)
	var out []rowDesc
	for j := f.segmentAt(i, col, n); j*segmentRunes < n || j == 0; j++ {
		s := f.segment(i, j, styled)
		k := 0
		if len(out) == 0 {
			k = s.rowIndex(col)
		}
		for ; k < len(s.ln.spans) && len(out) < limit; k++ {
			out = append(out, rowDesc{
				ln: s.ln, k: k, sp: s.ln.spans[k], start: s.cols[k],
				first: s.index == 0 && k == 0,
				last:  s.end >= n && k == len(s.ln.spans)-1,
			})
		}
		if len(out) >= limit || s.end >= n {
			break
		}
	}
	return out
}

// tallRowStart is the rune column where the row of tall line i that holds col starts.
func (f frame) tallRowStart(i, col int) int {
	rows := f.tallRows(i, false, col, 1)
	return rows[0].start
}

// tallNextRow returns the start of the row after the one of tall line i that holds col.
func (f frame) tallNextRow(i, col int) (int, bool) {
	rows := f.tallRows(i, false, col, 2)
	if len(rows) < 2 {
		return 0, false
	}
	return rows[1].start, true
}

// tallPrevRow returns the start of the row before the one of tall line i that holds col.
func (f frame) tallPrevRow(i, col int) (int, bool) {
	n := f.runeLen(i)
	j := f.segmentAt(i, col, n)
	s := f.segment(i, j, false)
	if k := s.rowIndex(col); k > 0 {
		return s.cols[k-1], true
	}
	if j == 0 {
		return 0, false
	}
	prev := f.segment(i, j-1, false)
	return prev.cols[len(prev.cols)-2], true
}

// tallRowsBetween returns the number of rows from the row of tall line i that holds from to the
// row that holds col, counting at most limit rows, and whether col is that near. A col before
// from is not near.
func (f frame) tallRowsBetween(i, from, col, limit int) (int, bool) {
	if col < from {
		return 0, false
	}
	target := f.tallRowStart(i, col)
	rows := f.tallRows(i, false, from, limit+1)
	for k, r := range rows {
		if r.start == target {
			return k, true
		}
	}
	return limit, false
}

// tallBack returns the start of the row that is n rows before the one of tall line i that
// holds col, or the first row of the line when there are fewer before it.
func (f frame) tallBack(i, col, n int) int {
	col = f.tallRowStart(i, col)
	for ; n > 0; n-- {
		prev, ok := f.tallPrevRow(i, col)
		if !ok {
			break
		}
		col = prev
	}
	return col
}

// rowCount is the number of display rows of line i for the sums that tell whether lines fit
// the body: its rows, or tallRows for a tall line.
func (f frame) rowCount(i int) int {
	if f.isTall(i) {
		return tallRows
	}
	return f.rowsIn(i, f.layoutLine(i, false))
}

// tallCellOf finds the cell of position p on tall line i, when the view of the line starts at
// the row of rune column anchor and body row y0 is the one that row is on.
func (f frame) tallCellOf(i, anchor int, p buffer.Pos, y0 int) (x, y int, ok bool) {
	n, near := f.tallRowsBetween(i, anchor, p.Col, f.h-y0)
	if !near {
		return 0, 0, false
	}
	y = y0 + n
	s := f.segment(i, f.segmentAt(i, p.Col, f.runeLen(i)), false)
	rel := min(max(p.Col-s.first, 0), len(s.ln.raw))
	k, cells := caretInSpans(s.ln.geom, s.ln.spans, expandedIndex(s.ln.raw, rel, f.tab))
	if k > 0 || s.cont {
		cells += s.ln.indent
	}
	return f.gutter + cells, y, y >= 0 && y < f.h
}

// revealTall is revealView for a caret on a tall line c. The view of a tall line starts at a rune
// column, and moves by rows: a caret above the window, or above the rows that show, takes its
// own row first, and a caret below takes the last row.
func (f frame) revealTall() (top, row int) {
	c, col := f.caret.Line, f.caret.Col
	if c < f.top {
		return c, f.tallBack(c, col, f.h-1)
	}
	if _, _, ok := f.cellOf(f.caret); ok {
		return f.top, f.topRow
	}
	if c == f.top {
		if start := f.tallRowStart(c, col); start < f.tallRowStart(c, f.topRow) {
			return c, start
		}
		return c, f.tallBack(c, col, f.h-1)
	}
	// The caret is below the body. Near the start of its line, the lines above it that fit
	// come with it, as for any line.
	k, near := f.tallRowsBetween(c, 0, col, f.h)
	if !near {
		return c, f.tallBack(c, col, f.h-1)
	}
	rows, top := k+1, c
	for prev := f.prevLine(top); prev >= f.top; prev = f.prevLine(top) {
		n := f.rowCount(prev)
		if rows+n > f.h {
			break
		}
		rows += n
		top = prev
	}
	if top == c {
		return c, f.tallBack(c, col, f.h-1)
	}
	return top, 0
}

// marginTall is withMargin for a view that starts in a tall line, at the row of rune column
// anchor. A caret on that line keeps margin rows above and below its row, as far as the line
// has them. A caret on a later line is left where the view puts it.
func (f frame) marginTall(top, anchor, margin int) (int, int) {
	if top != f.caret.Line {
		return top, anchor
	}
	col := f.caret.Col
	above, near := f.tallRowsBetween(top, anchor, col, f.h+margin)
	if !near {
		return top, anchor
	}
	if above < margin {
		anchor = f.tallBack(top, anchor, margin-above)
		above, _ = f.tallRowsBetween(top, anchor, col, f.h+margin)
	}
	need := 1 + f.rowsBelowCaret(margin)
	for above > 0 && above+need > f.h {
		next, ok := f.tallNextRow(top, anchor)
		if !ok {
			break
		}
		anchor = next
		above--
	}
	return top, anchor
}
