// Package buffertest holds test doubles for buffer.Source. It is test support
// only: no production package may import it (import rule R8).
package buffertest

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/tbxark/gvim/internal/buffer"
)

// MemSource is an in-memory buffer.Source that counts the bytes read from it
// and can be made to fail, shrink, grow or change, so tests can reach the
// error paths of large file mode. It is safe for concurrent use.
type MemSource struct {
	mu       sync.Mutex
	data     []byte
	size     int64 // size at construction, what Size reports
	mtime    int64
	origTime int64
	failN    int
	failErr  error
	closed   bool
	read     atomic.Int64
}

// NewMemSource returns a source over a copy of data.
func NewMemSource(data []byte) *MemSource {
	d := append([]byte(nil), data...)
	return &MemSource{data: d, size: int64(len(d))}
}

// ReadAt implements io.ReaderAt over the current contents. Reads past the end
// return a short count and io.EOF; the rest of p is left untouched.
func (m *MemSource) ReadAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	if m.failN > 0 {
		m.failN--
		return 0, m.failErr
	}
	if off < 0 || off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	m.read.Add(int64(n))
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Size returns the size when the source was created.
func (m *MemSource) Size() int64 { return m.size }

// Check reports buffer.ErrSourceGrew when the contents are longer than at
// creation, and buffer.ErrSourceChanged when they are shorter or Touch was
// called.
func (m *MemSource) Check() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch n := int64(len(m.data)); {
	case n > m.size:
		return buffer.ErrSourceGrew
	case n < m.size, m.mtime != m.origTime:
		return buffer.ErrSourceChanged
	}
	return nil
}

// Close marks the source closed; later reads fail.
func (m *MemSource) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// Closed reports whether Close was called.
func (m *MemSource) Closed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// BytesRead is the number of bytes ReadAt has returned so far.
func (m *MemSource) BytesRead() int64 { return m.read.Load() }

// FailNext makes the next n ReadAt calls fail with err.
func (m *MemSource) FailNext(n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failN, m.failErr = n, err
}

// Truncate cuts the contents to n bytes.
func (m *MemSource) Truncate(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = m.data[:min(n, int64(len(m.data)))]
}

// Grow appends extra to the contents.
func (m *MemSource) Grow(extra []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append(m.data, extra...)
}

// Mutate overwrites the contents at off in place without changing the size or
// the modification time, as a rewrite that Check cannot see.
func (m *MemSource) Mutate(off int64, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy(m.data[off:], b)
}

// Touch changes the modification time, so Check reports buffer.ErrSourceChanged.
func (m *MemSource) Touch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mtime++
}
