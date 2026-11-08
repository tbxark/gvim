package buffer

import (
	"bytes"
	"context"
	"errors"
)

// IndexScan counts the '\n' bytes of a large buffer's source chunk by chunk.
// It reads the source on its own reference and touches no buffer state, so
// Next may run on another goroutine while the buffer is used; the caller
// applies each batch with ApplyIndex on the buffer's goroutine. One IndexScan
// is used by one goroutine at a time and must be closed.
type IndexScan struct {
	owner  *bigStore // identity only; never dereferenced here
	ref    *srcRef
	stats  *statCounters
	chunk  int64
	skip   int64
	size   int64
	maxLn  int64
	n      int // chunks in the source
	next   int // next chunk to count
	lineLn int64
	buf    []byte
	closed bool
}

// IndexBatch is the newline counts of consecutive chunks, to be passed to
// ApplyIndex. A zero batch is valid and does nothing.
type IndexBatch struct {
	// First is the index of the first chunk counted.
	First int
	// Counts holds the number of '\n' bytes in each chunk from First on.
	Counts []int32
	// Huge reports that a line longer than MaxLine ended or was seen inside
	// these chunks.
	Huge bool
	// Done reports that the scan reached the end of the source.
	Done bool

	owner *bigStore
}

// beginScan starts a scan at the first chunk not yet counted. The caller must
// have checked that the buffer is open.
func (s *bigStore) beginScan() *IndexScan {
	s.src.retain()
	return &IndexScan{
		owner: s, ref: s.src, stats: s.stats,
		chunk: s.g.chunk, skip: s.g.skip, size: s.g.size, maxLn: s.maxLine,
		n: s.g.nChunks(), next: s.g.scanned,
	}
}

// BeginIndexScan starts counting the chunks that are not indexed yet. It
// returns nil for a memory buffer, a complete index, or a buffer whose source
// changed or was closed. A buffer frozen by a read error is thawed first, so
// the scan resumes where the last applied batch ended.
func (b *Buffer) BeginIndexScan() *IndexScan {
	s := b.big
	if s == nil {
		return nil
	}
	if s.err != nil && !errors.Is(s.err, ErrSourceChanged) && !s.closed {
		s.err, s.frozen = nil, nil
	}
	if s.err != nil || s.g.complete() {
		return nil
	}
	return s.beginScan()
}

// Next counts chunks until at least maxBytes bytes were read (always one
// chunk) or the source ends. It carries the length of the running line across
// chunk edges to find lines longer than MaxLine. On error it returns the
// chunks counted before the failure, which are valid, and the scan can be
// retried from a new BeginIndexScan after applying them.
func (sc *IndexScan) Next(ctx context.Context, maxBytes int64) (IndexBatch, error) {
	batch := IndexBatch{First: sc.next, owner: sc.owner}
	var read int64
	for sc.next < sc.n && (read == 0 || read < maxBytes) {
		if err := ctx.Err(); err != nil {
			return batch, err
		}
		off := int64(sc.next) * sc.chunk
		n := min(sc.chunk, sc.size-off)
		if int64(cap(sc.buf)) < n {
			sc.buf = make([]byte, n)
		}
		data := sc.buf[:n]
		if err := readFull(sc.ref.src, sc.stats, data, sc.skip+off); err != nil {
			return batch, err
		}
		count := bytes.Count(data, []byte{'\n'})
		batch.Huge = sc.account(data, count) || batch.Huge
		batch.Counts = append(batch.Counts, int32(count))
		sc.next++
		read += n
	}
	batch.Done = sc.next == sc.n
	return batch, nil
}

// account folds one chunk into the running line length and reports whether a
// line longer than MaxLine was found in it.
func (sc *IndexScan) account(data []byte, count int) (huge bool) {
	if count == 0 {
		sc.lineLn += int64(len(data))
		return sc.lineLn > sc.maxLn
	}
	first := bytes.IndexByte(data, '\n')
	huge = sc.lineLn+int64(first) > sc.maxLn
	if int64(len(data)) > sc.maxLn {
		// Only a chunk longer than MaxLine can hold a whole long line.
		for rest := data[first+1:]; ; {
			i := bytes.IndexByte(rest, '\n')
			if i < 0 {
				break
			}
			huge = huge || int64(i) > sc.maxLn
			rest = rest[i+1:]
		}
	}
	sc.lineLn = int64(len(data) - 1 - bytes.LastIndexByte(data, '\n'))
	return huge || sc.lineLn > sc.maxLn
}

// Close releases the scan's reference to the source. It is safe to call more
// than once.
func (sc *IndexScan) Close() {
	if sc == nil || sc.closed {
		return
	}
	sc.closed = true
	sc.ref.release()
}

// ApplyIndex adds the counts of a batch to the line index. It ignores a batch
// from another buffer, a batch that does not start at the first chunk not yet
// counted, and everything once the buffer is closed. Applying a batch never
// changes the content of existing lines or the version; Lines() only grows.
func (b *Buffer) ApplyIndex(batch IndexBatch) {
	if b.big != nil {
		b.big.apply(batch)
	}
}

func (s *bigStore) apply(batch IndexBatch) {
	if batch.owner != s || s.closed || s.g.complete() || batch.First != s.g.scanned {
		return
	}
	wasProvisional := s.g.provisional()
	for _, c := range batch.Counts {
		if s.g.complete() {
			break
		}
		s.g.nl[s.g.scanned+1] = s.g.nl[s.g.scanned] + int64(c)
		s.g.scanned++
	}
	s.huge = s.huge || batch.Huge
	if wasProvisional && !s.g.provisional() {
		// The provisional first line was cut at MaxLine and may have included
		// the terminator; now that it is found, decode it again.
		s.dec.remove(0)
	}
}

// Index reports how much of the line index exists. estLineLen is the average
// line length assumed for the estimate while fewer than 1000 lines are known.
// A memory buffer is always complete and exact.
func (b *Buffer) Index(estLineLen int) IndexInfo {
	if b.big == nil {
		n := len(b.lines)
		return IndexInfo{Lines: n, ApproxLines: n, Complete: true}
	}
	s := b.big
	g := s.g
	lines := s.lines()
	info := IndexInfo{
		Size:        g.size,
		Indexed:     min(int64(g.scanned)*g.chunk, g.size),
		Lines:       lines,
		ApproxLines: lines,
		Complete:    g.complete(),
		HugeLine:    s.hugeSeen(),
	}
	if info.Complete {
		return info
	}
	known := g.nl[g.scanned]
	avg := int64(estLineLen)
	if avg <= 0 {
		avg = 80
	}
	if known >= 1000 {
		avg = info.Indexed / known
	}
	avg = max(avg, 1)
	info.ApproxLines = max(lines, int(known+(g.size-info.Indexed)/avg))
	return info
}
