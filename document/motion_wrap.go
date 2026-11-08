package document

import (
	"github.com/tbxark/gvim/buffer"
)

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

// colAtCellFrom returns the column of the first cluster boundary at or after
// startCol whose display column is at least want cells past startCol, stopping at
// limit. The display column of startCol is counted from the start of the line, so a tab
// in the row runs to its stop, unless fromRow is set, for a row of a very long line, when it
// is counted from startCol, which is as far as a tab stop is known.
func colAtCellFrom(b *buffer.Buffer, line, startCol, want, limit, tab int, fromRow bool) int {
	base := 0
	if !fromRow {
		base = cellsFrom(b, line, 0, startCol, 0, tab)
	}
	cells := base
	w := newClusterWalker(b, line, startCol)
	for w.Col < limit && cells-base < want {
		if k := w.skipASCII(min(limit-w.Col, want-(cells-base))); k > 0 {
			cells += k
			continue
		}
		cluster, _, cw, ok := w.Next()
		if !ok {
			break
		}
		cells += clusterCells(cluster, cw, cells, tab)
	}
	return w.Col
}
