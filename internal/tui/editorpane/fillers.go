package editorpane

import (
	"sort"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// Fillers are blank rows that the pane puts between lines of its document, so that a
// pane showing a different text next to it keeps matching lines on the same row. The
// compare view uses them. They are display only: no line of the document owns a filler
// row, the caret never stands on one, and the document does not see them.
//
// A run of fillers belongs to the line it follows (After) or, at the top of the text, to
// the start of line 0 (Above). A line and the fillers after it form its block, so the
// block of line i is its text row followed by After[i] filler rows, and block 0 starts
// with the Above rows. Row numbers in this file count the rows of the whole text from 0,
// fillers included; the compare view calls them aligned rows.
//
// A pane with Fillers never wraps: a wrapped line would push the two sides out of step.
type Fillers struct {
	Above int         // filler rows above line 0
	After map[int]int // filler rows after line i; a missing line has none
	Style grid.Style  // the look of the filler rows: a patch over the text style

	keys []int // the lines with fillers after them, ascending
	cum  []int // cum[k] is the filler rows after keys[0..k]
}

// NewFillers returns the fillers with the given runs. A run of zero or fewer rows is
// dropped, and the map is copied.
func NewFillers(above int, after map[int]int, style grid.Style) *Fillers {
	fl := &Fillers{Above: max(above, 0), After: map[int]int{}, Style: style}
	for line, n := range after {
		if n > 0 && line >= 0 {
			fl.After[line] = n
			fl.keys = append(fl.keys, line)
		}
	}
	sort.Ints(fl.keys)
	total := 0
	for _, line := range fl.keys {
		total += fl.After[line]
		fl.cum = append(fl.cum, total)
	}
	return fl
}

// above is the filler rows above the text row of line.
func (fl *Fillers) above(line int) int {
	if fl == nil || line != 0 {
		return 0
	}
	return fl.Above
}

// after is the filler rows after the text row of line.
func (fl *Fillers) after(line int) int {
	if fl == nil {
		return 0
	}
	return fl.After[line]
}

// beforeBlock is the filler rows after the lines before line, not counting Above.
func (fl *Fillers) beforeBlock(line int) int {
	k := sort.SearchInts(fl.keys, line) // the keys under line
	if k == 0 {
		return 0
	}
	return fl.cum[k-1]
}

// BlockStart is the row where the block of line starts. The block of line 0 starts at
// row 0, above rows included.
func (fl *Fillers) BlockStart(line int) int {
	if line <= 0 {
		return 0
	}
	if fl == nil {
		return line
	}
	return fl.Above + line + fl.beforeBlock(line)
}

// TextRow is the row where the text of line is: its block start, and the rows above it
// for line 0.
func (fl *Fillers) TextRow(line int) int {
	return fl.BlockStart(line) + fl.above(line)
}

// blockRows is the number of rows of the block of line.
func (fl *Fillers) blockRows(line int) int {
	return fl.above(line) + 1 + fl.after(line)
}

// Total is the number of rows of a text of lines lines, with every filler row.
func (fl *Fillers) Total(lines int) int {
	if fl == nil {
		return lines
	}
	if lines <= 0 {
		return fl.Above
	}
	return fl.BlockStart(lines-1) + fl.blockRows(lines-1)
}

// LineAt is the line whose block holds row, and the row's index inside that block, for a
// text of lines lines. A row past the end maps to the last row of the last block, and a
// negative one to the first row of the first.
func (fl *Fillers) LineAt(lines, row int) (line, inBlock int) {
	if lines <= 0 {
		return 0, 0
	}
	row = min(max(row, 0), fl.Total(lines)-1)
	// The last line whose block starts at or above row.
	line = sort.Search(lines, func(i int) bool { return fl.BlockStart(i) > row }) - 1
	line = max(line, 0)
	return line, row - fl.BlockStart(line)
}

// fillAbove is the filler rows above the text row of line i in this frame.
func (f frame) fillAbove(i int) int { return f.fill.above(i) }

// fillAfter is the filler rows after the text row of line i in this frame.
func (f frame) fillAfter(i int) int { return f.fill.after(i) }

// rowsIn is the display rows of the block of line i: its filler rows and its text rows.
func (f frame) rowsIn(i int, ln line) int {
	return f.fillAbove(i) + f.aboveCount(i) + len(ln.spans) + f.belowCount(i) + f.fillAfter(i)
}

// fillRows is the number of filler rows of the frame's text.
func (f frame) fillRows() int {
	if f.fill == nil {
		return 0
	}
	return f.fill.Total(f.lines) - f.lines
}

// fillShown is how many of the filler rows [from, from+n) of the block of line i are in the
// view: all of them, except for the top line, whose first topRow rows are scrolled off.
func (f frame) fillShown(i, from, n int) int {
	if i == f.top {
		return max(from+n-max(f.topRow, from), 0)
	}
	return n
}

// filler tells what a display row is when it is not a text row.
type filler uint8

const (
	notFiller   filler = iota // a text row
	fillerAbove               // a filler row above line 0
	fillerAfter               // a filler row after a line
)

// fillStyle is the look of a filler cell.
func (f frame) fillStyle() grid.Style {
	return PatchStyle(f.styles.Text, f.fill.Style)
}

// paintFillers paints the filler rows [from, from+n) of the block of line i from row y
// on, for as long as the body has room, and returns the next row. A row that is
// scrolled off above the body (the top line's first topRow rows) is left out. Each row
// is listed in rows. Without fillers it does nothing.
func (f frame) paintFillers(g *grid.Grid, rows []rowInfo, i, from, n, y int) ([]rowInfo, int) {
	for k := 0; k < n && y < f.h; k++ {
		if i == f.top && from+k < f.topRow {
			continue
		}
		if f.gutter > 0 {
			g.Fill(0, f.gutter, y, f.styles.Gutter)
		}
		g.Fill(f.gutter, f.w-1, y, f.fillStyle())
		rows = append(rows, rowInfo{y: y, line: i, filler: true})
		y++
	}
	return rows, y
}

// fillerPos is the position a press on a filler row stands for: the start of the text
// for a row above line 0, and the end of the line the row follows otherwise.
func (f frame) fillerPos(r displayRow) buffer.Pos {
	if r.filler == fillerAbove {
		return buffer.Pos{Line: r.line}
	}
	raw, off := f.lineRaw(r.line)
	return buffer.Pos{Line: r.line, Col: off + len(raw)}
}

// scrollFilled is ScrollView for a pane with fillers: a step is a display row, filler
// rows included, so both sides of a comparison move by the same rows. The view stops with
// the last row of the text at the top, as the wrapped view does.
func (f frame) scrollFilled(delta int) (top, row int) {
	top, row = f.top, f.topRow
	rows := func(i int) int { return f.rowsIn(i, f.layoutLine(i, false)) }
	for ; delta > 0; delta-- {
		if row+1 < rows(top) {
			row++
			continue
		}
		next := f.next(top)
		if next >= f.lines {
			break
		}
		top, row = next, 0
	}
	for ; delta < 0; delta++ {
		if row > 0 {
			row--
			continue
		}
		prev := f.prevLine(top)
		if prev < 0 {
			break
		}
		top, row = prev, rows(prev)-1
	}
	return top, row
}

// trackMark is the look of the mark on scrollbar track row y, if a track mark covers any of the
// text rows that the track row stands for. The first mark in the list wins.
func (f frame) trackMark(y int) (grid.Style, bool) {
	if len(f.marks) == 0 || f.h <= 0 {
		return grid.Style{}, false
	}
	total := f.display + f.fillRows()
	lo, hi := y*total/f.h, max((y+1)*total/f.h-1, y*total/f.h)
	for _, m := range f.marks {
		if m.To >= lo && m.From <= hi {
			return PatchStyle(f.styles.Scrollbar, grid.Style{FG: m.Style.FG}), true
		}
	}
	return grid.Style{}, false
}

// rowAtFilled is rowAt for a pane with fillers. The rows count from the first row of the top
// block, of which topRow are above the body, and a block is its filler rows and its text row.
func (f frame) rowAtFilled(row int) displayRow {
	last := f.lines - 1
	row += f.topRow
	if row < 0 {
		i := min(max(f.top+row, 0), last)
		return displayOf(i, f.lastRowOf(i), true)
	}
	k := row
	for i := f.top; i < f.lines; i = f.next(i) {
		ln := f.layoutLine(i, false)
		n := f.rowsIn(i, ln)
		if k >= n {
			k -= n
			continue
		}
		above := f.fillAbove(i)
		switch {
		case k < above:
			return displayRow{line: i, ln: ln, span: ln.spans[0], last: true, valid: true, filler: fillerAbove}
		case k-above >= len(ln.spans):
			return displayRow{line: i, ln: ln, span: ln.spans[0], last: true, valid: true, filler: fillerAfter}
		}
		k -= above
		return displayOf(i, rowDesc{ln: ln, k: k, sp: ln.spans[k], first: k == 0, last: k == len(ln.spans)-1}, true)
	}
	return displayOf(last, f.lastRowOf(last), false)
}
