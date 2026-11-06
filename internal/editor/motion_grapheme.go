package editor

import (
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/internal/buffer"
)

// Cursor columns are rune indexes, but the caret moves and deletes by grapheme
// cluster (UAX #29), so a combining mark or a ZWJ emoji sequence is one step.

// clusterSimple reports whether r always forms a cluster of its own with the
// runes next to it. Everything below U+0300 does, which lets plain text skip the
// segmenter.
func clusterSimple(r rune) bool { return r < 0x300 }

// prevBoundary returns the column of the cluster boundary before col on line.
// col must be above 0.
func prevBoundary(b *buffer.Buffer, line, col int) int {
	before, _ := b.RuneAt(buffer.Pos{Line: line, Col: col - 1})
	at, ok := b.RuneAt(buffer.Pos{Line: line, Col: col})
	if clusterSimple(before) && (!ok || clusterSimple(at)) {
		return col - 1
	}
	n := 0
	for rest := b.Line(line); rest != ""; {
		cluster, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		size := len([]rune(cluster))
		if n+size >= col {
			return n
		}
		n, rest = n+size, tail
	}
	return n
}

// nextBoundary returns the column of the cluster boundary after col on line.
// col must be below the line length.
func nextBoundary(b *buffer.Buffer, line, col int) int {
	at, _ := b.RuneAt(buffer.Pos{Line: line, Col: col})
	after, ok := b.RuneAt(buffer.Pos{Line: line, Col: col + 1})
	if clusterSimple(at) && (!ok || clusterSimple(after)) {
		return col + 1
	}
	n := 0
	for rest := b.Line(line); rest != ""; {
		cluster, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		n += len([]rune(cluster))
		if n > col {
			return n
		}
		rest = tail
	}
	return col + 1
}

// snapToCluster moves p back to the start of the cluster that holds it, so a
// column set by a click or a vertical move never splits a cluster.
func snapToCluster(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	if p.Col > 0 && p.Col < b.LineLen(p.Line) {
		p.Col = prevBoundary(b, p.Line, p.Col+1)
	}
	return p
}
