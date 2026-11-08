package editor

import (
	"github.com/tbxark/gvim/internal/buffer"
)

// cellColumn returns the display column of p: the cells before it on its line,
// where a tab advances to the next multiple of tab and every other grapheme
// cluster takes its terminal width (two for CJK, none for a combining mark).
func cellColumn(b *buffer.Buffer, p buffer.Pos, tab int) int {
	return cellsFrom(b, p.Line, 0, p.Col, 0, tab)
}

// cellsFrom returns the cells of the runes [from, to) of line, when the text starts at
// display column start, so that a tab runs to the stop it would reach there.
func cellsFrom(b *buffer.Buffer, line, from, to, start, tab int) int {
	cells := start
	w := newClusterWalker(b, line, from)
	for w.col < to {
		if k := w.skipASCII(to - w.col); k > 0 {
			cells += k
			continue
		}
		cluster, _, cw, ok := w.next()
		if !ok {
			break
		}
		cells += clusterCells(cluster, cw, cells, tab)
	}
	return cells - start
}

// colAtCell returns the rune column on line of the first cluster boundary whose
// display column is at least target, or the line length when the line is
// shorter. Wide characters and tabs are never split.
func colAtCell(b *buffer.Buffer, line, target, tab int) int {
	return colAtCellFrom(b, line, 0, target, b.LineLen(line), tab, false)
}

// clusterCells is the width of cluster when it starts at display column cells.
// w is the width uniseg measured, which gives a tab none.
func clusterCells(cluster string, w, cells, tab int) int {
	if cluster == "\t" {
		return tab - cells%tab
	}
	return w
}
