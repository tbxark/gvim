package editor

import (
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/internal/buffer"
)

// cellColumn returns the display column of p: the cells before it on its line,
// where a tab advances to the next multiple of tab and every other grapheme
// cluster takes its terminal width (two for CJK, none for a combining mark).
func cellColumn(b *buffer.Buffer, p buffer.Pos, tab int) int {
	cells, col := 0, 0
	for rest := b.Line(p.Line); rest != "" && col < p.Col; {
		cluster, tail, w, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		cells += clusterCells(cluster, w, cells, tab)
		col += len([]rune(cluster))
		rest = tail
	}
	return cells
}

// colAtCell returns the rune column on line of the first cluster boundary whose
// display column is at least target, or the line length when the line is
// shorter. Wide characters and tabs are never split.
func colAtCell(b *buffer.Buffer, line, target, tab int) int {
	cells, col := 0, 0
	for rest := b.Line(line); rest != "" && cells < target; {
		cluster, tail, w, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		cells += clusterCells(cluster, w, cells, tab)
		col += len([]rune(cluster))
		rest = tail
	}
	return col
}

// clusterCells is the width of cluster when it starts at display column cells.
// w is the width uniseg measured, which gives a tab none.
func clusterCells(cluster string, w, cells, tab int) int {
	if cluster == "\t" {
		return tab - cells%tab
	}
	return w
}
