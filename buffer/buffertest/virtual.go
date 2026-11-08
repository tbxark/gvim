package buffertest

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
)

// Virtual is a generated buffer.Source of a given size that allocates nothing.
// Line n is the 10-digit zero-padded decimal of n, then 'x' up to lineWidth-1
// bytes, then '\n'. The last line is cut where the size ends. Offsets above
// 2^31 and 2^32 are therefore reachable in a test without a big file, and the
// text of any line is known without reading.
type Virtual struct {
	size  int64
	width int
	read  atomic.Int64
}

// NewVirtual returns a source of size bytes with lines lineWidth bytes wide
// including the newline. lineWidth must be at least 12.
func NewVirtual(size int64, lineWidth int) *Virtual {
	if lineWidth < 12 {
		panic(fmt.Sprintf("buffertest: line width %d is under 12", lineWidth))
	}
	return &Virtual{size: size, width: lineWidth}
}

// VirtualLine returns the text of line n (without newline) of a Virtual
// source with the given line width.
func VirtualLine(n int64, lineWidth int) string {
	return fmt.Sprintf("%010d", n) + strings.Repeat("x", lineWidth-1-10)
}

// ReadAt implements io.ReaderAt.
func (v *Virtual) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= v.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), v.size-off))
	w := int64(v.width)
	var digits [10]byte
	cachedLine := int64(-1)
	for i := range n {
		pos := off + int64(i)
		line, col := pos/w, int(pos%w)
		switch {
		case col == v.width-1:
			p[i] = '\n'
		case col < 10:
			if line != cachedLine {
				cachedLine = line
				for d, x := 9, line; d >= 0; d, x = d-1, x/10 {
					digits[d] = byte('0' + x%10)
				}
			}
			p[i] = digits[col]
		default:
			p[i] = 'x'
		}
	}
	v.read.Add(int64(n))
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Size returns the size.
func (v *Virtual) Size() int64 { return v.size }

// Check always reports no change.
func (v *Virtual) Check() error { return nil }

// Close does nothing.
func (v *Virtual) Close() error { return nil }

// BytesRead is the number of bytes generated so far.
func (v *Virtual) BytesRead() int64 { return v.read.Load() }
