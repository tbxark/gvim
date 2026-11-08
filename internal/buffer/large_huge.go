package buffer

import (
	"unicode/utf8"
)

// Huge lines. A line of the source that is longer than MaxLine is not decoded: a line of
// 113 MB would be 450 MB of runes. It is read through an index instead: its length in runes,
// and the byte offset of every hugeStride-th rune, found once by reading the line in chunks.
// A window of it, such as the part on screen, a range for the clipboard or the character at
// the caret, is then read from the source at the mark before it and decoded from there. A
// huge line can be read and moved over to its end. An edit reads the whole line as runes once
// (see wholeLine), and the line is then an ordinary literal line of the overlay; the edit is
// refused when those runes would not fit the overlay budget.

const (
	// hugeStride is how many runes lie between two marks of a huge line.
	hugeStride = 4096
	// hugeChunk is how many bytes of a huge line are read at once while it is indexed.
	hugeChunk = 4 << 20
)

// stride is the runes between the marks, and chunk the bytes read at once. A test makes them
// small, to have a line of a few hundred bytes cross many of both.
func (s *bigStore) stride() int {
	if s.strideOver > 0 {
		return s.strideOver
	}
	return hugeStride
}

func (s *bigStore) chunk() int64 {
	if s.chunkOver > 0 {
		return int64(s.chunkOver)
	}
	return hugeChunk
}

// hugeLine is the index of one huge base line.
type hugeLine struct {
	// start is the offset of the line in the source, and size its length in bytes.
	start, size int64
	// runes is the number of runes of the line, an invalid byte counting as one.
	runes int
	// marks[k] is the byte offset in the line of rune k*stride.
	marks  []int64
	stride int
}

// hugeBase returns the index of base line b when it is longer than MaxLine and its end is
// known, or nil. The first line of a source whose index is not complete may go on, and has no
// index. Building the index reads the line once. A failed read is recorded and gives nil.
func (s *bigStore) hugeBase(b int) *hugeLine {
	if h, ok := s.huges.get(b); ok {
		return h
	}
	if b == 0 && s.g.provisional() {
		return nil
	}
	start, end, _, err := s.lineRange(b)
	if err != nil {
		s.fail(err)
		return nil
	}
	if end-start <= s.maxLine {
		return nil
	}
	h, err := s.indexHuge(start, end-start)
	if err != nil {
		s.fail(err)
		return nil
	}
	s.huges.put(b, h)
	return h
}

// hugeOf is hugeBase for document line i, or nil when the line is not a huge base line.
func (s *bigStore) hugeOf(i int) *hugeLine {
	r := s.ref(s.clampLine(i))
	if r.isLit {
		return nil
	}
	return s.hugeBase(r.base)
}

// indexHuge reads the size bytes of the source from start, and indexes them.
func (s *bigStore) indexHuge(start, size int64) (*hugeLine, error) {
	return indexHugeLine(&s.baseReader, start, size, s.stride(), s.chunk())
}

// indexHugeLine reads the size bytes of the source from start, through r, in pieces of chunk
// bytes, and indexes them every stride runes. It reads through a baseReader, so that a snapshot
// can index a line of its own: the buffer's index is not read from another goroutine.
func indexHugeLine(r *baseReader, start, size int64, stride int, chunk int64) (*hugeLine, error) {
	h := &hugeLine{start: start, size: size, marks: []int64{0}, stride: stride}
	var carry []byte
	for off := int64(0); off < size; {
		n := min(chunk, size-off)
		buf := make([]byte, int64(len(carry))+n)
		copy(buf, carry)
		if err := readFull(r.src.src, r.stats, buf[len(carry):], start+off); err != nil {
			return nil, err
		}
		base := off - int64(len(carry))
		off += n
		data := buf
		carry = carry[:0]
		if off < size {
			// A rune that is cut by the end of the chunk goes on with the next one.
			for i := 1; i < utf8.UTFMax && i <= len(data); i++ {
				if !utf8.RuneStart(data[len(data)-i]) {
					continue
				}
				if cut := len(data) - i; !utf8.FullRune(data[cut:]) {
					carry = append(carry, data[cut:]...)
					data = data[:cut]
				}
				break
			}
		}
		h.add(data, base)
	}
	return h, nil
}

// add indexes data, which starts base bytes into the line.
func (h *hugeLine) add(data []byte, base int64) {
	if isASCII(data) {
		// A byte is a rune: the marks fall at known offsets.
		for m := len(h.marks) * h.stride; m < h.runes+len(data); m = len(h.marks) * h.stride {
			h.marks = append(h.marks, base+int64(m-h.runes))
		}
		h.runes += len(data)
		return
	}
	for at := 0; at < len(data); {
		if h.runes%h.stride == 0 && h.runes/h.stride == len(h.marks) {
			h.marks = append(h.marks, base+int64(at))
		}
		_, n := utf8.DecodeRune(data[at:])
		at += n
		h.runes++
	}
}

// wholeLine reads document line i, a huge base line, in full as runes, for an edit that
// rewrites it. The runes are charged to the overlay budget first: the line is refused with
// ErrOverlayFull when they would not fit with what is there already, and with ErrLineOverBudget
// when they would not fit the budget on their own. An invalid byte is refused with
// ErrInvalidUTF8, as for any other line. The provisional first line has no known end, so it is
// refused with ErrLineTooLong.
func (s *bigStore) wholeLine(i int) (lineInfo, error) {
	h := s.hugeOf(i)
	if h == nil {
		return lineInfo{}, ErrLineTooLong
	}
	if int64(h.runes)*runeCost+lineCost > s.maxOverlay {
		return lineInfo{}, ErrLineOverBudget
	}
	if overlayBytes(s.st)+int64(h.runes)*runeCost+lineCost > s.maxOverlay {
		return lineInfo{}, ErrOverlayFull
	}
	buf := make([]byte, h.size)
	if err := readFull(s.src.src, s.stats, buf, h.start); err != nil {
		s.fail(err)
		return lineInfo{}, err
	}
	runes, invalid := decodeRunes(buf)
	if invalid {
		return lineInfo{}, ErrInvalidUTF8
	}
	return lineInfo{runes: runes}, nil
}

// isASCII reports whether every byte of data is below 0x80.
func isASCII(data []byte) bool {
	for len(data) >= 8 {
		w := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
			uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
		if w&0x8080808080808080 != 0 {
			return false
		}
		data = data[8:]
	}
	for _, c := range data {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// hugeSlice returns runes [col, col+n) of huge line h, clamped to the line, with U+FFFD for an
// invalid byte as a decoded line has. It reads from the mark before col, a few KiB, and the
// text it returns.
func (s *bigStore) hugeSlice(h *hugeLine, col, n int) string {
	text, err := hugeSliceOf(&s.baseReader, h, col, n)
	if err != nil {
		s.fail(err)
		return ""
	}
	return text
}

// hugeSliceOf is hugeSlice read through r, so that a snapshot can read a window of a huge line.
// It returns the error of a failed read.
func hugeSliceOf(r *baseReader, h *hugeLine, col, n int) (string, error) {
	col = min(max(col, 0), h.runes)
	n = min(max(n, 0), h.runes-col)
	if n == 0 {
		return "", nil
	}
	k := col / h.stride
	off := h.marks[k]
	skip := col - k*h.stride
	// The runes lie between the mark before col and the mark after the window, so the read
	// costs the window's own bytes and at most one stride more, not four bytes a rune.
	end := h.size
	if j := k + (skip+n+h.stride-1)/h.stride; j < len(h.marks) {
		end = h.marks[j]
	}
	buf := make([]byte, end-off)
	if err := readFull(r.src.src, r.stats, buf, h.start+off); err != nil {
		return "", err
	}
	out := make([]rune, 0, n)
	for at, i := 0, 0; at < len(buf) && i < skip+n; i++ {
		r, size := utf8.DecodeRune(buf[at:])
		at += size
		if i >= skip {
			out = append(out, r)
		}
	}
	return string(out), nil
}
