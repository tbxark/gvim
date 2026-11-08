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
	// idxStride and idxChunk are the index geometry of huge lines the buffer uses (see
	// large_huge.go), which the snapshot's own indexes use too.
	idxStride int
	idxChunk  int64
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
	text, _ := g.lineText(i)
	return text
}

// lineText is line, and it reports whether the line is huge, which the text then
// has cut short. It reads the line once, so a chunk does not look a line up twice.
func (g *bigSnap) lineText(i int) (string, bool) {
	ref := refAt(g.st, min(max(i, 0), g.lines()-1))
	if ref.isLit {
		return string(ref.lit), false
	}
	data, huge, err := g.r.lineBytes(ref.base)
	if err != nil {
		g.fail(err)
		return "", false
	}
	return validString(data), huge
}

// chunk is Snapshot.Chunk. A huge line is a chunk of its own: it is not joined to the lines
// before it, and the chunk ends after it.
func (g *bigSnap) chunk(from, maxBytes int) (first, n int, text string) {
	total := g.lines()
	first = min(max(from, 0), total)
	var sb strings.Builder
	for i := first; i < total; i++ {
		line, long := g.lineText(i)
		if n > 0 && long {
			break
		}
		if n > 0 {
			if g.failed != nil || sb.Len()+1+len(line) > maxBytes {
				break
			}
			sb.WriteByte('\n')
		}
		sb.WriteString(line)
		n++
		if long {
			break
		}
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
		line, long := g.lineText(i)
		if n > 0 && long {
			break
		}
		if n > 0 {
			if g.failed != nil || size+1+len(line) > maxBytes {
				break
			}
			size++
		}
		size += len(line)
		rev = append(rev, line)
		n++
		if long {
			break
		}
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

// longLine reports whether line i is longer than MaxLine, and if so its length in runes. A
// huge line is read by column through its index (see hugeOf).
func (g *bigSnap) longLine(i int) (int, bool) {
	ref := refAt(g.st, min(max(i, 0), g.lines()-1))
	if ref.isLit {
		return 0, false
	}
	h := g.hugeOf(ref.base)
	if h == nil {
		return 0, false
	}
	return h.runes, true
}

// hugeOf returns the index of base line b when it is longer than MaxLine, or nil. The index is
// shared with the buffer through the source's hugeMemo, and a snapshot builds it from its own
// reader when none is there yet, since the buffer's state is not read from another goroutine.
func (g *bigSnap) hugeOf(b int) *hugeLine {
	if h, ok := g.r.huges.get(b); ok {
		return h
	}
	if b == 0 && g.r.g.provisional() {
		return nil
	}
	start, end, _, err := g.r.lineRange(b)
	if err != nil {
		g.fail(err)
		return nil
	}
	if end-start <= int64(g.r.maxLine) {
		return nil
	}
	h, err := indexHugeLine(&g.r, start, end-start, g.idxStride, g.idxChunk)
	if err != nil {
		g.fail(err)
		return nil
	}
	g.r.huges.put(b, h)
	return h
}

// lineRunes is Snapshot.LineRunes.
func (g *bigSnap) lineRunes(i, col, n int) string {
	ref := refAt(g.st, min(max(i, 0), g.lines()-1))
	if ref.isLit {
		return runesString(ref.lit, col, n)
	}
	if h := g.hugeOf(ref.base); h != nil {
		text, err := hugeSliceOf(&g.r, h, col, n)
		if err != nil {
			g.fail(err)
			return ""
		}
		return text
	}
	return runesString([]rune(g.line(i)), col, n)
}

// runesString returns the runes [col, col+n) of runes, clamped to them, as a string.
func runesString(runes []rune, col, n int) string {
	col = min(max(col, 0), len(runes))
	n = min(max(n, 0), len(runes)-col)
	return string(runes[col : col+n])
}
