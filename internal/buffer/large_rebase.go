package buffer

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
)

// ErrRebaseMismatch is returned by Rebase when the file that was written does
// not match what the buffer expects: a stale version, a different size, or
// newline counts that differ from the bytes on disk.
var ErrRebaseMismatch = errors.New("buffer: written file does not match rebase counts")

// snapshot returns the large snapshot state of s's current text.
func (s *bigStore) snapshot() *bigSnap {
	g := &bigSnap{r: s.baseReader, st: s.st, hugeBase: s.hugeSeen(), idxStride: s.stride(), idxChunk: s.chunk()}
	if s.src.retain() {
		g.held = true
	} else {
		g.fail(errClosed)
	}
	return g
}

// Rebase makes the buffer read from src, a file just written from a snapshot
// of this buffer, instead of the old source, without scanning it. res is what
// WriteTo returned, ver the version the snapshot was taken at and prefixLen
// the length of the WriteOptions.Prefix that was written.
//
// It requires that the buffer is still at ver and src has the size WriteTo
// wrote, and it reads the first, middle and last chunk of src to check them
// against the counts. On success the overlay and the undo history are empty,
// the buffer is marked saved, and the old source is released (snapshots that
// still read it keep it open until they are released). On error nothing is
// changed, ErrRebaseMismatch is reported for a file that does not match, and
// the caller still owns src; on success the buffer owns it.
func (b *Buffer) Rebase(src Source, res WriteResult, ver uint64, prefixLen int64) error {
	s := b.big
	switch {
	case s == nil:
		return errNotLarge
	case s.closed:
		return errClosed
	case b.depth > 0:
		return fmt.Errorf("%w: inside Group", ErrRebaseMismatch)
	case b.ver != ver:
		return fmt.Errorf("%w: buffer is at version %d, not %d", ErrRebaseMismatch, b.ver, ver)
	}
	nl, err := s.verifyRebase(src, res, prefixLen)
	if err != nil {
		return err
	}
	old := s.baseReader
	size := res.Bytes - prefixLen
	s.baseReader = baseReader{
		src: newSrcRef(src),
		g: gridView{
			chunk: res.Chunk, skip: prefixLen, size: size,
			nl: nl, scanned: len(nl) - 1,
		},
		pages:   newPageCache(res.Chunk, old.pages.want, s.budget, s.stats),
		ranges:  newRangeMemo(),
		huges:   newHugeMemo(),
		stats:   s.stats,
		maxLine: old.maxLine,
	}
	s.st = bigState{}
	s.dec.clear()
	s.err, s.refusal, s.grew = nil, nil, false
	s.huge = res.huge
	b.undo, b.redo, b.open = nil, nil, false
	b.saved = b.ver
	old.pages.retire()
	old.src.release()
	return nil
}

// verifyRebase checks res against src and returns the cumulative newline
// counts of the file's chunks.
func (s *bigStore) verifyRebase(src Source, res WriteResult, prefixLen int64) ([]int64, error) {
	size := res.Bytes - prefixLen
	switch {
	case res.Chunk <= 0 || prefixLen < 0 || size < 0:
		return nil, fmt.Errorf("%w: bad write result", ErrRebaseMismatch)
	case src.Size() != res.Bytes:
		return nil, fmt.Errorf("%w: file has %d bytes, wrote %d", ErrRebaseMismatch, src.Size(), res.Bytes)
	}
	n := int((size + res.Chunk - 1) / res.Chunk)
	if len(res.Counts) != n {
		return nil, fmt.Errorf("%w: %d counts for %d chunks", ErrRebaseMismatch, len(res.Counts), n)
	}
	nl := make([]int64, n+1)
	for c, k := range res.Counts {
		if k < 0 || int64(k) > res.Chunk {
			return nil, fmt.Errorf("%w: bad count in chunk %d", ErrRebaseMismatch, c)
		}
		nl[c+1] = nl[c] + int64(k)
	}
	// Sample the first, middle and last chunk of the file itself.
	picks := []int{0, n / 2, n - 1}
	if n == 0 {
		picks = nil
	}
	var buf []byte
	for _, c := range slices.Compact(picks) {
		off := int64(c) * res.Chunk
		l := min(res.Chunk, size-off)
		if int64(cap(buf)) < l {
			buf = make([]byte, l)
		}
		buf = buf[:l]
		if err := readFull(src, s.stats, buf, prefixLen+off); err != nil {
			return nil, err
		}
		if int64(bytes.Count(buf, []byte{'\n'})) != nl[c+1]-nl[c] {
			return nil, fmt.Errorf("%w: chunk %d", ErrRebaseMismatch, c)
		}
	}
	return nl, nil
}
