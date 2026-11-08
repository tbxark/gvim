package editor

import (
	"github.com/tbxark/gvim/buffer"
)

// RowMapper returns the rune column at which each display row of a buffer line
// starts. The first entry is 0 and the entries rise. It returns nil, or one entry,
// for a line that is not wrapped, and always when line wrap is off.
type RowMapper func(line int) []int

// SetRowMapper sets the function that reports the wrapped rows of a line. Nil, the
// start state, moves by buffer line only.
func (e *Editor) SetRowMapper(f RowMapper) { e.rowMap = f }

// rowStarts returns the row starts of a wrapped line, or nil when the line is shown
// on one row.
func (e *Editor) rowStarts(line int) []int {
	if e.rowMap == nil {
		return nil
	}
	if s := e.rowMap(line); len(s) > 1 {
		return s
	}
	return nil
}

// RowWindowMapper reports the rows of a line that wraps to too many rows to list: the
// rune columns at which the rows around column col start, from the row before the one that
// holds col to the second row after it, as many of them as the line has, so the window
// starts at the line's first row only when no row is before it. tall is false, and starts
// nil, for a line whose rows RowMapper lists.
type RowWindowMapper func(line, col int) (starts []int, tall bool)

// SetRowWindowMapper sets the function that reports the rows around a column of a tall
// line. Nil, the start state, leaves such a line to RowMapper.
func (e *Editor) SetRowWindowMapper(f RowWindowMapper) { e.rowWin = f }

// rowStartsAt is rowStarts for the rows around column col: the same list for a line that
// RowMapper lists, and the window of rows of a tall line, which tall says.
func (e *Editor) rowStartsAt(line, col int) (starts []int, tall bool) {
	if e.rowWin != nil {
		if s, ok := e.rowWin(line, col); ok {
			return s, true
		}
	}
	return e.rowStarts(line), false
}

// visualVertical moves from p one display row up (dir -1) or down (+1). It reports
// false when neither the row of p nor the target row is a wrapped line, or when p
// is already on the first or last row of the text, and the buffer-line motion
// answers instead. The wanted column is the offset from the start of the row.
func (e *Editor) visualVertical(d *Document, from buffer.Pos, dir int) (buffer.Pos, bool) {
	b := d.Buffer
	starts, tall := e.rowStartsAt(from.Line, from.Col)
	row, line, target, targetTall := rowOf(starts, from.Col)+dir, from.Line, starts, tall
	switch {
	case row < 0:
		if line == 0 {
			return buffer.Pos{}, false
		}
		line--
		target, targetTall = e.rowStartsAt(line, b.LineLen(line))
		row = max(len(target), 1) - 1
	case row >= max(len(starts), 1):
		if line == b.Lines()-1 {
			return buffer.Pos{}, false
		}
		line++
		target, targetTall = e.rowStartsAt(line, 0)
		row = 0
	}
	if starts == nil && target == nil {
		return buffer.Pos{}, false
	}
	tab := e.tabSizeOf(d)
	if d.Want < 0 {
		first, _ := rowBounds(starts, rowOf(starts, from.Col), b.LineLen(from.Line))
		if tall {
			// Counted from the row start: the line is not walked from its own.
			d.Want = cellsFrom(b, from.Line, first, from.Col, 0, tab)
		} else {
			d.Want = cellColumn(b, from, tab) - cellColumn(b, buffer.Pos{Line: from.Line, Col: first}, tab)
		}
	}
	first, last := rowBounds(target, row, b.LineLen(line))
	col := colAtCellFrom(b, line, first, d.Want, last, tab, targetTall)
	return buffer.Pos{Line: line, Col: min(col, last)}, true
}

// visualLineStart is Home on a wrapped line: the start of the caret's row, or the
// start of the row before it when the caret is there already. ok is false when the
// line is not wrapped.
func (e *Editor) visualLineStart(p buffer.Pos) (buffer.Pos, bool) {
	starts, _ := e.rowStartsAt(p.Line, p.Col)
	if starts == nil {
		return p, false
	}
	r := rowOf(starts, p.Col)
	if starts[r] == p.Col && r > 0 {
		r--
	}
	return buffer.Pos{Line: p.Line, Col: starts[r]}, true
}

// visualLineEnd is End on a wrapped line: the last character of the caret's row, or
// of the next row when the caret is there already. The last row ends at the end of
// the line. ok is false when the line is not wrapped.
func (e *Editor) visualLineEnd(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	starts, _ := e.rowStartsAt(p.Line, p.Col)
	if starts == nil {
		return p, false
	}
	r := rowOf(starts, p.Col)
	_, end := rowBounds(starts, r, b.LineLen(p.Line))
	if p.Col == end && r+1 < len(starts) {
		_, end = rowBounds(starts, r+1, b.LineLen(p.Line))
	}
	return buffer.Pos{Line: p.Line, Col: end}, true
}

// pageInTallLine moves the caret one page, by display rows, when its line wraps to more
// rows than the viewport holds. A page of buffer lines would jump over the line, or to
// the end of the document, and the rows in between could never be reached. It reports
// whether it moved. A line that fits the viewport pages by lines.
func (e *Editor) pageInTallLine(d *Document, dir int, extend bool) bool {
	if starts, tall := e.rowStartsAt(d.Cursor.Line, d.Cursor.Col); !tall && len(starts) <= e.rows {
		return false
	}
	moved := false
	for range max(e.rows-min(pageOverlap, e.rows-1), 1) {
		to, ok := e.visualVertical(d, d.Cursor, dir)
		if !ok {
			break
		}
		d.Move(to, extend)
		moved = true
	}
	return moved
}
