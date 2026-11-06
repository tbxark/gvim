package editor

import (
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/internal/buffer"
)

// Soft wrap. The editor does not know the pane width, so the UI tells it where a
// wrapped line breaks (SetRowMapper). With line wrap on, Up, Down, Home and End
// then move by display row, as Fresh does.

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

// rowOf returns the index of the row of starts that holds column col. A column at
// the start of a row belongs to that row. Nil starts is one row.
func rowOf(starts []int, col int) int {
	r := 0
	for i, s := range starts {
		if s <= col {
			r = i
		}
	}
	return r
}

// rowBounds returns the first column of row r and the last column the caret may
// take on it. The caret stays on a row that is not the last, so it may not take
// the start of the next row. The last row ends at the end of the line.
func rowBounds(starts []int, r, lineLen int) (first, last int) {
	if starts == nil {
		return 0, lineLen
	}
	if r+1 < len(starts) {
		return starts[r], starts[r+1] - 1
	}
	return starts[r], lineLen
}

// visualVertical moves from p one display row up (dir -1) or down (+1). It reports
// false when neither the row of p nor the target row is a wrapped line, or when p
// is already on the first or last row of the text, and the buffer-line motion
// answers instead. The wanted column is the offset from the start of the row.
func (e *Editor) visualVertical(d *Document, from buffer.Pos, dir int) (buffer.Pos, bool) {
	b := d.Buffer
	starts := e.rowStarts(from.Line)
	row, line, target := rowOf(starts, from.Col)+dir, from.Line, starts
	switch {
	case row < 0:
		if line == 0 {
			return buffer.Pos{}, false
		}
		line--
		target = e.rowStarts(line)
		row = max(len(target), 1) - 1
	case row >= max(len(starts), 1):
		if line == b.Lines()-1 {
			return buffer.Pos{}, false
		}
		line++
		target = e.rowStarts(line)
		row = 0
	}
	if starts == nil && target == nil {
		return buffer.Pos{}, false
	}
	tab := e.tabSizeOf(d)
	if d.want < 0 {
		first, _ := rowBounds(starts, rowOf(starts, from.Col), b.LineLen(from.Line))
		d.want = cellColumn(b, from, tab) - cellColumn(b, buffer.Pos{Line: from.Line, Col: first}, tab)
	}
	first, last := rowBounds(target, row, b.LineLen(line))
	col := colAtCellFrom(b, line, first, d.want, last, tab)
	return buffer.Pos{Line: line, Col: min(col, last)}, true
}

// colAtCellFrom returns the column of the first cluster boundary at or after
// startCol whose display column is at least want cells past startCol, stopping at
// limit.
func colAtCellFrom(b *buffer.Buffer, line, startCol, want, limit, tab int) int {
	base := cellColumn(b, buffer.Pos{Line: line, Col: startCol}, tab)
	cells, col := base, startCol
	rest := string([]rune(b.Line(line))[startCol:])
	for rest != "" && col < limit && cells-base < want {
		cluster, tail, w, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		cells += clusterCells(cluster, w, cells, tab)
		col += len([]rune(cluster))
		rest = tail
	}
	return col
}

// visualLineStart is Home on a wrapped line: the start of the caret's row, or the
// start of the row before it when the caret is there already. ok is false when the
// line is not wrapped.
func (e *Editor) visualLineStart(p buffer.Pos) (buffer.Pos, bool) {
	starts := e.rowStarts(p.Line)
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
	starts := e.rowStarts(p.Line)
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
