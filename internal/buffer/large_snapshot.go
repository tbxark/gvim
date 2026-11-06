package buffer

import "strings"

// bigSnap is the state behind a Snapshot of a large buffer. It is a value
// copy of what a reader needs: the immutable overlay state, a copy of the line
// index frozen at its current length, and the shared page cache and source
// reference. It holds nothing of the buffer's UI-goroutine state, so a
// snapshot can be read on another goroutine while the buffer is edited and
// indexed. One snapshot is used by one goroutine at a time.
type bigSnap struct {
	r  baseReader
	st bigState
	// hugeBase: the buffer had seen a line longer than MaxLine when the
	// snapshot was taken.
	hugeBase bool
	// held: the snapshot owns a reference to the source.
	held bool
	// failed is the first error a read of this snapshot met.
	failed error
}

// release gives up the snapshot's source reference and empties the page cache
// when the buffer no longer uses it.
func (g *bigSnap) release() {
	if !g.held {
		return
	}
	g.held = false
	g.r.pages.clearIfRetired()
	g.r.src.release()
}

// fail records the first read error and stops further source reads; lines
// that are cached keep reading.
func (g *bigSnap) fail(err error) {
	if g.failed == nil {
		g.failed = err
		g.r.frozen = err
	}
}

// lines is the number of document lines: at least one.
func (g *bigSnap) lines() int {
	return max(1, g.st.root.size()+g.r.g.baseLines()-g.st.tailFirst)
}

// line returns document line i (clamped) as text. A line that cannot be read
// comes back empty and is recorded in failed.
func (g *bigSnap) line(i int) string {
	ref := refAt(g.st, min(max(i, 0), g.lines()-1))
	if ref.isLit {
		return string(ref.lit)
	}
	data, _, err := g.r.lineBytes(ref.base)
	if err != nil {
		g.fail(err)
		return ""
	}
	return validString(data)
}

// chunk is Snapshot.Chunk.
func (g *bigSnap) chunk(from, maxBytes int) (first, n int, text string) {
	total := g.lines()
	first = min(max(from, 0), total)
	var sb strings.Builder
	for i := first; i < total; i++ {
		line := g.line(i)
		if n > 0 {
			if g.failed != nil || sb.Len()+1+len(line) > maxBytes {
				break
			}
			sb.WriteByte('\n')
		}
		sb.WriteString(line)
		n++
	}
	return first, n, sb.String()
}

// chunkBefore is Snapshot.ChunkBefore.
func (g *bigSnap) chunkBefore(to, maxBytes int) (first, n int, text string) {
	to = min(to, g.lines())
	if to <= 0 {
		return 0, 0, ""
	}
	var rev []string // lines from to-1 downward
	size := 0
	for i := to - 1; i >= 0; i-- {
		line := g.line(i)
		if n > 0 {
			if g.failed != nil || size+1+len(line) > maxBytes {
				break
			}
			size++
		}
		size += len(line)
		rev = append(rev, line)
		n++
	}
	first = to - n
	var sb strings.Builder
	sb.Grow(size)
	for k := len(rev) - 1; k >= 0; k-- {
		sb.WriteString(rev[k])
		if k > 0 {
			sb.WriteByte('\n')
		}
	}
	return first, n, sb.String()
}
