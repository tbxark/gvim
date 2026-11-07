package buffer

import (
	"errors"
	"sort"
	"unicode/utf8"
)

// ErrLargeBuffer is returned by ByteOffset and PosAtByteOffset for a large
// buffer, whose byte offsets would need the length of every line.
var ErrLargeBuffer = errors.New("buffer: byte offsets are not available on large buffers")

// ByteCol returns the byte column of p inside its line: the UTF-8 length of
// the line's text before p. p is clamped. The text is the line as Line returns
// it, so in a large buffer a byte of the source that is not valid UTF-8 counts
// as U+FFFD, three bytes, and a line cut at the line cap is the cut line.
func (b *Buffer) ByteCol(p Pos) int {
	line := b.lineRunes(p.Line)
	return runesLen(line[:min(max(p.Col, 0), len(line))])
}

// PosAtByteCol returns the position on line whose byte column is byteCol. A
// byteCol inside a UTF-8 sequence rounds down to the start of the rune, and
// both arguments are clamped. The line is the text Line returns (see ByteCol).
func (b *Buffer) PosAtByteCol(line, byteCol int) Pos {
	line = b.clampLine(line)
	rs := b.lineRunes(line)
	var tmp [utf8.UTFMax]byte
	n := 0
	for i, r := range rs {
		n += utf8.EncodeRune(tmp[:], r)
		if n > byteCol {
			return Pos{line, i}
		}
	}
	return Pos{line, len(rs)}
}

// ByteOffset returns the offset of p in Text, where lines are joined by a
// single "\n". p is clamped. Line start offsets are cached as prefix sums and
// an edit on line l only forgets the sums after l, so a run of queries between
// edits, or edits far below the queried lines, cost little. A large buffer
// returns ErrLargeBuffer.
func (b *Buffer) ByteOffset(p Pos) (int, error) {
	if b.big != nil {
		return 0, ErrLargeBuffer
	}
	p = b.Clamp(p)
	return b.lineStart(p.Line) + runesLen(b.lines[p.Line][:p.Col]), nil
}

// PosAtByteOffset is the inverse of ByteOffset. An offset before the text is
// the start, one past the end of the text is the end, and one inside a UTF-8
// sequence rounds down to the start of the rune. A large buffer returns
// ErrLargeBuffer.
func (b *Buffer) PosAtByteOffset(off int) (Pos, error) {
	if b.big != nil {
		return Pos{}, ErrLargeBuffer
	}
	off = max(off, 0)
	b.lineStart(0)
	n := len(b.lines)
	line := sort.Search(b.startsValid, func(i int) bool { return b.starts[i] > off }) - 1
	// The line is the last known one when off may lie beyond it: learn more
	// lines until the next one starts after off.
	for line == b.startsValid-1 && b.startsValid < n {
		if b.lineStart(b.startsValid) > off {
			break
		}
		line = b.startsValid - 1
	}
	return b.PosAtByteCol(line, off-b.starts[line]), nil
}

// lineStart returns the offset in Text where line l starts. l must be a line
// of a memory buffer. Offsets are learned from the last valid prefix sum on.
func (b *Buffer) lineStart(l int) int {
	if b.startsValid == 0 {
		b.starts = append(b.starts[:0], 0)
		b.startsValid = 1
	}
	if l >= b.startsValid {
		b.starts = b.starts[:b.startsValid]
		for i := b.startsValid; i <= l; i++ {
			b.starts = append(b.starts, b.starts[i-1]+runesLen(b.lines[i-1])+1)
		}
		b.startsValid = l + 1
	}
	return b.starts[l]
}

// forgetStarts drops the cached line start offsets after line l, which an edit
// is about to change. The start of l itself does not depend on the edit.
func (b *Buffer) forgetStarts(l int) {
	b.startsValid = min(b.startsValid, l+1)
}

// ByteColWalker converts positions to byte columns like Buffer.ByteCol, for a run of
// positions that mostly ascend, such as the matches of a search. It remembers where the
// last conversion stopped, so converting the positions of one line costs the length of
// that line once, where ByteCol costs it for each position. A position before the
// remembered one on its line starts again from the line start. The buffer must not be
// edited while the walker is in use.
type ByteColWalker struct {
	b     *Buffer
	line  int
	runes []rune
	col   int // rune column the sums stand at
	bytes int // byte column of col
}

// NewByteColWalker returns a walker over b.
func (b *Buffer) NewByteColWalker() *ByteColWalker {
	return &ByteColWalker{b: b, line: -1}
}

// ByteCol returns what Buffer.ByteCol returns for p.
func (w *ByteColWalker) ByteCol(p Pos) int {
	if l := w.b.clampLine(p.Line); l != w.line {
		w.line, w.runes, w.col, w.bytes = l, w.b.lineRunes(l), 0, 0
	}
	col := min(max(p.Col, 0), len(w.runes))
	if col < w.col {
		w.col, w.bytes = 0, 0
	}
	w.bytes += runesLen(w.runes[w.col:col])
	w.col = col
	return w.bytes
}
