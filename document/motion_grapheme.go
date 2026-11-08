package document

import (
	"github.com/tbxark/gvim/buffer"
)

// clusterSimple reports whether r always forms a cluster of its own with the
// runes next to it. Everything below U+0300 does, which lets plain text skip the
// segmenter.
func clusterSimple(r rune) bool { return r < 0x300 }

// clusterWholeLine is the length from which a line is not walked from its start to find a
// cluster boundary: a walk of a line of megabytes for every key pressed on it would be the cost
// of the line. clusterLookback is how far before the column the walk of a longer line begins,
// and clusterBackup how many runes it may be backed up from there, over runes that continue a
// cluster, to a place where one can begin.
const (
	clusterWholeLine = 4096
	clusterLookback  = 256
	clusterBackup    = 64
)

// clusterStart is the column from which the clusters of line are walked to find the boundary
// next to col: the start of the line, or, in a very long line, a place a little before col where
// a cluster begins. A boundary that is a lookback away from the start is the same as if the whole
// line were walked, except inside a run of paired regional indicators, which no text has so long.
func clusterStart(b *buffer.Buffer, line, col int) int {
	if b.LineLen(line) <= clusterWholeLine {
		return 0
	}
	start := max(col-clusterLookback, 0)
	for k := 0; start > 0 && k < clusterBackup; k++ {
		if r, ok := b.RuneAt(buffer.Pos{Line: line, Col: start}); ok && clusterSimple(r) {
			break
		}
		start--
	}
	return start
}

// prevBoundary returns the column of the cluster boundary before col on line.
// col must be above 0.
func prevBoundary(b *buffer.Buffer, line, col int) int {
	before, _ := b.RuneAt(buffer.Pos{Line: line, Col: col - 1})
	at, ok := b.RuneAt(buffer.Pos{Line: line, Col: col})
	if clusterSimple(before) && (!ok || clusterSimple(at)) {
		return col - 1
	}
	w := newClusterWalker(b, line, clusterStart(b, line, col))
	n := w.Col
	for {
		_, size, _, ok := w.Next()
		if !ok {
			return n
		}
		if n+size >= col {
			return n
		}
		n += size
	}
}

// nextBoundary returns the column of the cluster boundary after col on line.
// col must be below the line length.
func nextBoundary(b *buffer.Buffer, line, col int) int {
	at, _ := b.RuneAt(buffer.Pos{Line: line, Col: col})
	after, ok := b.RuneAt(buffer.Pos{Line: line, Col: col + 1})
	if clusterSimple(at) && (!ok || clusterSimple(after)) {
		return col + 1
	}
	w := newClusterWalker(b, line, clusterStart(b, line, col))
	n := w.Col
	for {
		_, size, _, ok := w.Next()
		if !ok {
			return col + 1
		}
		n += size
		if n > col {
			return n
		}
	}
}

// snapToCluster moves p back to the start of the cluster that holds it, so a
// column set by a click or a vertical move never splits a cluster.
func snapToCluster(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	if p.Col > 0 && p.Col < b.LineLen(p.Line) {
		p.Col = prevBoundary(b, p.Line, p.Col+1)
	}
	return p
}
