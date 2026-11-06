package buffer

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Snapshot is an immutable view of a Buffer's text at one version. Edits made
// to the buffer afterwards do not show in it. A Snapshot is used by one
// goroutine at a time; the caller must Release it.
//
// A large snapshot is read on other goroutines than its buffer's, so it uses
// only data that is immutable or safe to share (see bigSnap) and never the
// UI-goroutine state of the buffer.
type Snapshot struct {
	lines [][]rune
	ver   uint64
	big   *bigSnap // set for a snapshot of a large buffer; lines is nil then
}

// Snapshot returns an immutable view of the current text. A large buffer
// answers in O(1): the snapshot keeps the immutable overlay state and a
// reference to the source, and reads the source only when it is read. A memory
// buffer copies its line headers (O(lines)); the rune data is shared because
// edits replace lines instead of changing them in place.
func (b *Buffer) Snapshot() *Snapshot {
	if b.big != nil {
		return &Snapshot{ver: b.ver, big: b.big.snapshot()}
	}
	return &Snapshot{lines: slices.Clone(b.lines), ver: b.ver}
}

// Release drops the snapshot's hold on its resources. A large snapshot lets go
// of its source, which stays open for it until then even when the buffer has
// moved on (Rebase) or closed. It is a no-op for a memory snapshot and safe to
// call more than once. A released snapshot must not be read.
func (s *Snapshot) Release() {
	if s.big != nil {
		s.big.release()
	}
}

// Err returns the first error a read of a large snapshot met (the source
// changed or failed). Lines it could not read came back empty. A memory
// snapshot never fails.
func (s *Snapshot) Err() error {
	if s.big != nil {
		return s.big.failed
	}
	return nil
}

// Version returns the buffer version the snapshot was taken at.
func (s *Snapshot) Version() uint64 { return s.ver }

// IndexComplete reports whether the snapshot covers the whole file. A large
// snapshot taken while the line index is still being built has only the lines
// indexed so far, so a search of it sees only that part. A memory snapshot is
// always complete.
func (s *Snapshot) IndexComplete() bool {
	return s.big == nil || s.big.r.g.complete()
}

// HugeLine reports whether the snapshot has a line longer than MaxLine, which
// Line and Chunk return cut short. It is conservative: it is also true while
// the first line is still being measured. A memory snapshot never has one.
func (s *Snapshot) HugeLine() bool {
	return s.big != nil && s.big.hugeBase
}

// Lines returns the number of lines.
func (s *Snapshot) Lines() int {
	if s.big != nil {
		return s.big.lines()
	}
	return len(s.lines)
}

// Line returns line i without its newline. Out-of-range indexes are clamped.
func (s *Snapshot) Line(i int) string {
	if s.big != nil {
		return s.big.line(i)
	}
	return string(s.lines[min(max(i, 0), len(s.lines)-1)])
}

// Chunk returns whole lines starting at from, joined by "\n", for streaming
// search. It takes lines while the text stays within maxBytes, but always at
// least one, so a longer first line comes back whole. first is from clamped
// into [0, Lines()] and n is the number of lines returned; n is 0 only when
// from is at or past the end.
func (s *Snapshot) Chunk(from, maxBytes int) (first, n int, text string) {
	if s.big != nil {
		return s.big.chunk(from, maxBytes)
	}
	first = min(max(from, 0), len(s.lines))
	size := 0
	for i := first; i < len(s.lines); i++ {
		add := runesLen(s.lines[i])
		if n > 0 {
			add++ // the joining newline
			if size+add > maxBytes {
				break
			}
		}
		size += add
		n++
	}
	return first, n, s.join(first, first+n, size)
}

// ChunkBefore returns whole lines ending just before line to, joined by "\n",
// with the same size rule as Chunk. first is the first returned line and n the
// count; n is 0 only when to <= 0. to is clamped to Lines().
func (s *Snapshot) ChunkBefore(to, maxBytes int) (first, n int, text string) {
	if s.big != nil {
		return s.big.chunkBefore(to, maxBytes)
	}
	to = min(to, len(s.lines))
	if to <= 0 {
		return 0, 0, ""
	}
	first = to
	size := 0
	for i := to - 1; i >= 0; i-- {
		add := runesLen(s.lines[i])
		if n > 0 {
			add++
			if size+add > maxBytes {
				break
			}
		}
		size += add
		n++
		first = i
	}
	return first, n, s.join(first, to, size)
}

// join returns lines [from, to) joined by "\n"; size is its byte length.
func (s *Snapshot) join(from, to, size int) string {
	var sb strings.Builder
	sb.Grow(size)
	for i := from; i < to; i++ {
		if i > from {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(s.lines[i]))
	}
	return sb.String()
}

// runesLen returns the UTF-8 length of rs as string(rs) encodes it.
func runesLen(rs []rune) int {
	var tmp [utf8.UTFMax]byte
	n := 0
	for _, r := range rs {
		n += utf8.EncodeRune(tmp[:], r)
	}
	return n
}
