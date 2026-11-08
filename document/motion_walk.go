package document

import (
	"github.com/rivo/uniseg"
	"github.com/tbxark/gvim/buffer"
	"unicode/utf8"
)

// walkChunk is how many runes of a line a clusterWalker reads at once. A motion on a
// line of megabytes reads only the part of the line it walks over, and not the whole
// line as a string.
const walkChunk = 2048

// clusterWalker walks the grapheme clusters of one line from a rune column, reading the
// text in chunks. Printable ASCII, which is nearly all of a very long line, is passed
// over a run at a time, because each character of it is a cluster of one cell.
type clusterWalker struct {
	B    *buffer.Buffer
	Line int
	N    int    // the runes of the line
	Col  int    // the rune column of the next cluster
	Text string // the chunk, from col on
}

func newClusterWalker(b *buffer.Buffer, line, col int) *clusterWalker {
	return &clusterWalker{B: b, Line: line, N: b.LineLen(line), Col: col}
}

// fill makes w.text hold the line from w.col on, up to a chunk, when it holds none.
func (w *clusterWalker) fill() {
	if w.Text == "" && w.Col < w.N {
		w.Text = w.B.LineSlice(w.Line, w.Col, walkChunk)
	}
}

// skipASCII passes over up to limit printable ASCII characters that are each a cluster of
// one cell, and returns how many. A character followed by a byte that is not ASCII is not
// passed, because a combining mark after it would make them one cluster.
func (w *clusterWalker) skipASCII(limit int) int {
	w.fill()
	n := 0
	for n < len(w.Text) && n < limit && w.Text[n] >= 0x20 && w.Text[n] < 0x7f {
		n++
	}
	if n > 0 && n < len(w.Text) && n < limit && w.Text[n] >= 0x80 {
		n--
	}
	if n == len(w.Text) && w.Col+n < w.N {
		// The chunk ends in ASCII and the line goes on: the next chunk may start with a mark.
		n--
	}
	if n <= 0 {
		return 0
	}
	w.Text = w.Text[n:]
	w.Col += n
	return n
}

// next returns the next cluster, its length in runes and its terminal width, or ok false
// at the end of the line.
func (w *clusterWalker) Next() (cluster string, runes, width int, ok bool) {
	w.fill()
	if w.Text == "" {
		return "", 0, 0, false
	}
	cluster, tail, width, _ := uniseg.FirstGraphemeClusterInString(w.Text, -1)
	if tail == "" && w.Col+utf8.RuneCountInString(cluster) < w.N {
		// The chunk ends inside the cluster, or just before a mark that joins it. Read on
		// from the start of the cluster.
		w.Text = w.B.LineSlice(w.Line, w.Col, walkChunk+utf8.RuneCountInString(cluster))
		cluster, tail, width, _ = uniseg.FirstGraphemeClusterInString(w.Text, -1)
	}
	runes = utf8.RuneCountInString(cluster)
	w.Text = tail
	w.Col += runes
	return cluster, runes, width, true
}
