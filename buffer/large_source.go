package buffer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Source is the read-only byte store behind a large buffer, normally a file.
// ReadAt, Size and Check must be safe for concurrent use. Size is the size at
// open time and never changes.
type Source interface {
	io.ReaderAt
	// Size returns the size in bytes when the source was opened.
	Size() int64
	// Check compares the source with its state at open time. It returns nil,
	// ErrSourceGrew (larger than at open) or ErrSourceChanged (smaller, or the
	// same size with a different modification time, or unreadable).
	Check() error
	// Close releases the source. The buffer calls it once, when the last
	// reference is gone.
	Close() error
}

// srcRef shares one Source between a buffer and the index scans and snapshots
// that read it on other goroutines. The source is closed when the last
// reference is released.
type srcRef struct {
	mu   sync.Mutex
	src  Source
	refs int
}

func newSrcRef(src Source) *srcRef { return &srcRef{src: src, refs: 1} }

// retain takes one more reference. It reports false when the source is
// already closed.
func (r *srcRef) retain() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refs <= 0 {
		return false
	}
	r.refs++
	return true
}

// release drops one reference and closes the source at zero.
func (r *srcRef) release() {
	r.mu.Lock()
	r.refs--
	last := r.refs == 0
	r.mu.Unlock()
	if last {
		_ = r.src.Close()
	}
}

// readFull fills p from off. A short read means the source shrank under us and
// is reported as ErrSourceChanged; the unread part of p is never trusted.
func readFull(src Source, stats *statCounters, p []byte, off int64) error {
	n, err := src.ReadAt(p, off)
	stats.readBytes.Add(int64(n))
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrSourceChanged
	}
	return fmt.Errorf("buffer: reading source: %w", err)
}

// fileSource is a Source over an open regular file.
type fileSource struct {
	f     *os.File
	size  int64
	mtime time.Time
}

// OpenFileSource opens path for large file mode. The file is read with pread;
// it is never memory mapped. On Windows it is opened so that another process
// may rename or delete it while it is open.
func OpenFileSource(path string) (Source, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("buffer: %s is not a regular file", path)
	}
	return &fileSource{f: f, size: fi.Size(), mtime: fi.ModTime()}, nil
}

func (s *fileSource) ReadAt(p []byte, off int64) (int, error) { return s.f.ReadAt(p, off) }

func (s *fileSource) Size() int64 { return s.size }

// Stat returns the state of the open file itself, not of the path it was opened
// by. A caller uses it to learn which file a source reads.
func (s *fileSource) Stat() (os.FileInfo, error) { return s.f.Stat() }

func (s *fileSource) Close() error { return s.f.Close() }

func (s *fileSource) Check() error {
	fi, err := s.f.Stat()
	switch {
	case err != nil:
		return ErrSourceChanged
	case fi.Size() > s.size:
		return ErrSourceGrew
	case fi.Size() < s.size, !fi.ModTime().Equal(s.mtime):
		return ErrSourceChanged
	}
	return nil
}
