package buffer

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

// gridView is the line index of a source: the source is cut into fixed-size
// chunks, and nl[c] is the number of '\n' bytes in chunks [0, c). Chunk
// boundaries may fall anywhere (mid-line, mid-CRLF, mid-rune); only '\n' bytes
// are counted, which is safe for UTF-8 and CRLF.
//
// The text starts skip bytes into the source (a BOM) and is size bytes long.
// nl has nChunks+1 cells; cells [0, scanned] are valid. The owner writes cell
// k only for k > scanned, and readers copy this struct, so a reader never sees
// a cell that is written later.
type gridView struct {
	chunk, skip, size int64
	nl                []int64
	scanned           int
}

// nChunks is the number of chunks that cover the text.
func (g gridView) nChunks() int { return len(g.nl) - 1 }

// complete reports whether every chunk has been counted.
func (g gridView) complete() bool { return g.scanned == g.nChunks() }

// baseLines is the number of source lines known: all of them once the index is
// complete, else only the terminated ones (a line is known when its '\n' has
// been seen). A buffer shows at least one line; see provisional.
func (g gridView) baseLines() int {
	n := g.nl[g.scanned]
	if g.complete() {
		n++
	}
	return int(n)
}

// provisional reports that no line is known yet but the text is not empty: the
// first line is longer than what has been scanned.
func (g gridView) provisional() bool {
	return g.nl[g.scanned] == 0 && !g.complete()
}

// newlineOffset returns the absolute source offset of the k-th '\n' (k counts
// from 1 and must not exceed nl[scanned]).
func (r *baseReader) newlineOffset(k int64) (int64, error) {
	g := r.g
	c := sort.Search(g.scanned+1, func(i int) bool { return g.nl[i] >= k }) - 1
	p, err := r.page(c)
	if err != nil {
		return 0, err
	}
	// The page was counted when the index was built; a different count means
	// the bytes changed under us.
	if int64(len(p.nlPos)) != g.nl[c+1]-g.nl[c] {
		return 0, ErrSourceChanged
	}
	return g.skip + int64(c)*g.chunk + int64(p.nlPos[k-g.nl[c]-1]), nil
}

// byteAt returns the source byte at absolute offset off.
func (r *baseReader) byteAt(off int64) (byte, error) {
	rel := off - r.g.skip
	c := int(rel / r.g.chunk)
	p, err := r.page(c)
	if err != nil {
		return 0, err
	}
	i := rel - int64(c)*r.g.chunk
	if i >= int64(len(p.data)) {
		return 0, ErrSourceChanged
	}
	return p.data[i], nil
}

// lineRange locates base line b, which must be below max(1, baseLines). start
// and end delimit its text in the source, without the terminator, and term is
// the terminator it has: eolLF, eolCRLF, or eolDefault for the last line of a
// complete source, which has none. The line that is still unterminated while
// the index is incomplete (only the first can be) ends MaxLine+UTFMax bytes
// in, so the line cut can find a rune start.
func (r *baseReader) lineRange(b int) (start, end int64, term uint8, err error) {
	if sp, ok := r.ranges.get(b); ok {
		return sp.start, sp.end, sp.term, nil
	}
	g := r.g
	start = g.skip
	if b > 0 {
		if start, err = r.newlineOffset(int64(b)); err != nil {
			return 0, 0, 0, err
		}
		start++
	}
	switch {
	case int64(b) < g.nl[g.scanned]:
		nl, err := r.newlineOffset(int64(b) + 1)
		if err != nil {
			return 0, 0, 0, err
		}
		end, term = nl, eolLF
		if nl > start {
			last, err := r.byteAt(nl - 1)
			if err != nil {
				return 0, 0, 0, err
			}
			if last == '\r' {
				end, term = nl-1, eolCRLF
			}
		}
		r.ranges.put(b, lineSpan{start, end, term})
	case g.complete():
		end = g.skip + g.size
	default:
		end = min(g.skip+g.size, start+r.maxLine+utf8.UTFMax)
	}
	return start, end, term, nil
}

// lineBytes returns the text bytes of base line b: terminator removed, and
// cut to MaxLine bytes at a rune start when longer (huge reports that). The
// result may alias a cached page and must not be modified.
func (r *baseReader) lineBytes(b int) (data []byte, huge bool, err error) {
	r.stats.lineCalls.Add(1)
	start, end, _, err := r.lineRange(b)
	if err != nil {
		return nil, false, err
	}
	n := end - start
	if n == 0 {
		return nil, false, nil
	}
	huge = n > r.maxLine
	if huge {
		end = start + min(n, r.maxLine+utf8.UTFMax)
	}
	data, err = r.readSpan(start, end)
	if err != nil {
		return nil, false, err
	}
	if huge {
		cut := int(r.maxLine)
		for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(data[cut]); i++ {
			cut--
		}
		data = data[:cut]
	}
	return data, huge, nil
}

// linePrefix returns the first limit bytes of base line b, or all of it when it is
// shorter or limit reaches the line cap. A prefix that lies in a cached page comes
// from it. Otherwise only the prefix is read, straight from the source and not
// cached, so showing a window of a long line costs the window and not the line.
// The result may end inside a rune.
func (r *baseReader) linePrefix(b int, limit int64) ([]byte, error) {
	if limit >= r.maxLine {
		data, _, err := r.lineBytes(b)
		return data, err
	}
	r.stats.lineCalls.Add(1)
	start, end, _, err := r.lineRange(b)
	if err != nil {
		return nil, err
	}
	end = min(end, start+limit)
	if end <= start {
		return nil, nil
	}
	rel := start - r.g.skip
	c := int(rel / r.g.chunk)
	if p, ok := r.pages.get(c); ok && end-r.g.skip <= int64(c+1)*r.g.chunk {
		i := rel - int64(c)*r.g.chunk
		if end-start > int64(len(p.data))-i {
			return nil, ErrSourceChanged
		}
		return p.data[i : i+end-start], nil
	}
	if r.frozen != nil {
		return nil, r.frozen
	}
	buf := make([]byte, end-start)
	if err := readFull(r.src.src, r.stats, buf, start); err != nil {
		return nil, err
	}
	return buf, nil
}

// readSpan returns source bytes [start, end). A span inside one chunk comes
// from the page cache. A span across chunks is read directly and not cached.
func (r *baseReader) readSpan(start, end int64) ([]byte, error) {
	rel := start - r.g.skip
	c := int(rel / r.g.chunk)
	if end-r.g.skip <= int64(c+1)*r.g.chunk {
		p, err := r.page(c)
		if err != nil {
			return nil, err
		}
		i := rel - int64(c)*r.g.chunk
		if end-start > int64(len(p.data))-i {
			return nil, ErrSourceChanged
		}
		return p.data[i : i+end-start], nil
	}
	if r.frozen != nil {
		return nil, r.frozen
	}
	buf := make([]byte, end-start)
	if err := readFull(r.src.src, r.stats, buf, start); err != nil {
		return nil, err
	}
	return buf, nil
}

// page returns the page of chunk c, reading it from the source on a miss. A
// frozen reader (its source failed or changed) serves cached pages only.
func (r *baseReader) page(c int) (*page, error) {
	if p, ok := r.pages.get(c); ok {
		return p, nil
	}
	if r.frozen != nil {
		return nil, r.frozen
	}
	g := r.g
	off := int64(c) * g.chunk
	buf := make([]byte, min(g.chunk, g.size-off))
	if err := readFull(r.src.src, r.stats, buf, g.skip+off); err != nil {
		return nil, err
	}
	p := &page{data: buf}
	if c < g.scanned {
		p.nlPos = make([]int32, 0, g.nl[c+1]-g.nl[c])
	}
	for at := 0; ; {
		i := bytes.IndexByte(buf[at:], '\n')
		if i < 0 {
			break
		}
		at += i
		p.nlPos = append(p.nlPos, int32(at))
		at++
	}
	return r.pages.put(c, p), nil
}
