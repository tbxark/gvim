package editor

import (
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/buffer"
)

// walkChunk is how many runes of a line a clusterWalker reads at once. A motion on a
// line of megabytes reads only the part of the line it walks over, and not the whole
// line as a string.
const walkChunk = 2048

// clusterWalker walks the grapheme clusters of one line from a rune column, reading the
// text in chunks. Printable ASCII, which is nearly all of a very long line, is passed
// over a run at a time, because each character of it is a cluster of one cell.
type clusterWalker struct {
	b    *buffer.Buffer
	line int
	n    int    // the runes of the line
	col  int    // the rune column of the next cluster
	text string // the chunk, from col on
}

func newClusterWalker(b *buffer.Buffer, line, col int) *clusterWalker {
	return &clusterWalker{b: b, line: line, n: b.LineLen(line), col: col}
}

// fill makes w.text hold the line from w.col on, up to a chunk, when it holds none.
func (w *clusterWalker) fill() {
	if w.text == "" && w.col < w.n {
		w.text = w.b.LineSlice(w.line, w.col, walkChunk)
	}
}

// skipASCII passes over up to limit printable ASCII characters that are each a cluster of
// one cell, and returns how many. A character followed by a byte that is not ASCII is not
// passed, because a combining mark after it would make them one cluster.
func (w *clusterWalker) skipASCII(limit int) int {
	w.fill()
	n := 0
	for n < len(w.text) && n < limit && w.text[n] >= 0x20 && w.text[n] < 0x7f {
		n++
	}
	if n > 0 && n < len(w.text) && n < limit && w.text[n] >= 0x80 {
		n--
	}
	if n == len(w.text) && w.col+n < w.n {
		// The chunk ends in ASCII and the line goes on: the next chunk may start with a mark.
		n--
	}
	if n <= 0 {
		return 0
	}
	w.text = w.text[n:]
	w.col += n
	return n
}

// next returns the next cluster, its length in runes and its terminal width, or ok false
// at the end of the line.
func (w *clusterWalker) next() (cluster string, runes, width int, ok bool) {
	w.fill()
	if w.text == "" {
		return "", 0, 0, false
	}
	cluster, tail, width, _ := uniseg.FirstGraphemeClusterInString(w.text, -1)
	if tail == "" && w.col+utf8.RuneCountInString(cluster) < w.n {
		// The chunk ends inside the cluster, or just before a mark that joins it. Read on
		// from the start of the cluster.
		w.text = w.b.LineSlice(w.line, w.col, walkChunk+utf8.RuneCountInString(cluster))
		cluster, tail, width, _ = uniseg.FirstGraphemeClusterInString(w.text, -1)
	}
	runes = utf8.RuneCountInString(cluster)
	w.text = tail
	w.col += runes
	return cluster, runes, width, true
}
