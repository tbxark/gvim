package document

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"sync/atomic"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/encoding"
)

// Remote large-file mode: a remote file at or above the large-file threshold opens
// in the editor's existing large-file mode, but its bytes come through a windowed
// source instead of a local file. The source loads 1 MiB windows in commands and
// keeps at most RemoteWindowCache of them (about 8 MiB per document); a read outside
// the cache returns buffer.ErrSourcePending immediately, so the paint path never
// waits on the remote host. The root loads the missed window in a command and
// repaints when it arrives.
//
// Editing follows Fresh's large-file model: edits are an in-memory overlay over the
// untouched base lines (buffer's large-file mode, bounded by its overlay budget). A
// save merges the overlay into the unchanged ranges read back through the windowed
// source and writes the whole merged file with the authority's atomic write; the
// merged size is bounded (remoteLargeSaveMax) because that write takes the bytes in
// memory. A file that changed underneath refuses the save clearly instead of writing
// over it.

// RangeAccess is the optional FileAccess part for a windowed source: it reads at
// most n bytes at off, returns a short answer with io.EOF, and performs the read where
// the file lives. A FileAccess without it cannot open a remote large file lazily.
type RangeAccess interface {
	// ReadRange reads at most n bytes at off; the context cancels the read when a
	// document closes or the window command is abandoned. It returns io.EOF with the
	// bytes of a short final window.
	ReadRange(ctx context.Context, path string, off, n int64) ([]byte, error)
}

const (
	// RemoteWindowSize is the byte size of one window fetched from the authority.
	// It is the authority's per-answer cap (1 MiB).
	RemoteWindowSize = 1 << 20
	// RemoteWindowCache is how many windows one source keeps (bounded memory).
	RemoteWindowCache = 8
	// remoteInitialIndexBytes is how much of the head is indexed before the open
	// returns; larger prefixes would need windows the source has not loaded.
	remoteInitialIndexBytes = 4096
)

// remoteLargeSaveMax bounds the merged file a remote large-file save writes. The
// authority's atomic write takes the whole file as one byte slice, so the merge reads
// the untouched ranges through the window source into memory; a larger file refuses
// the save with a clear notice instead of risking the machine. Tests lower it.
var remoteLargeSaveMax int64 = 64 << 20

// LargeWindow is one pending window of an asynchronous large document.
type LargeWindow struct {
	Path string
	Off  int64
	N    int64
}

// remoteWindowSource is a buffer.Source over a RangeAccess. ReadAt is cache-only;
// LoadWindow does the remote read.
type remoteWindowSource struct {
	access RangeAccess
	path   string
	size   int64

	mu      sync.Mutex
	windows map[int64]*list.Element
	lru     *list.List
	closed  bool
	// pending is the first window a read missed, for the root to load.
	pendingOff, pendingN int64
	pendingOK            bool
	gen                  atomic.Uint64
}

// remoteWindow is one cached window at its aligned index.
type remoteWindow struct {
	index int64
	data  []byte
}

// newRemoteWindowSource opens a windowed source for a remote file of size bytes. It
// eagerly loads the first window so the encoding sample and the first paint have
// bytes; it runs in a command, never in the update loop.
func newRemoteWindowSource(ctx context.Context, access RangeAccess, path string, size int64) (*remoteWindowSource, error) {
	s := &remoteWindowSource{
		access: access, path: path, size: size,
		windows: make(map[int64]*list.Element), lru: list.New(),
	}
	if size > 0 {
		if err := s.LoadWindow(ctx, 0, RemoteWindowSize); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ReadAt implements io.ReaderAt. The bytes must already be cached: a miss records
// the window and returns ErrSourcePending at once, doing no remote I/O.
func (s *remoteWindowSource) ReadAt(p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, fs.ErrClosed
	}
	read := 0
	for read < len(p) {
		pos := off + int64(read)
		if pos >= s.size {
			return read, io.EOF
		}
		index := pos / RemoteWindowSize
		e, ok := s.windows[index]
		if !ok {
			windowOff := index * RemoteWindowSize
			s.pendingOff, s.pendingN, s.pendingOK = windowOff, min(RemoteWindowSize, s.size-windowOff), true
			return read, buffer.ErrSourcePending
		}
		data := e.Value.(*remoteWindow).data
		s.lru.MoveToFront(e)
		skip := pos - index*RemoteWindowSize
		n := copy(p[read:], data[skip:])
		if n == 0 {
			return read, io.EOF
		}
		read += n
	}
	return read, nil
}

// LoadWindow fetches the windows covering [off, off+n) and one window after them as
// a bounded prefetch, evicting the least recently used beyond RemoteWindowCache. It
// blocks on the authority read; the context cancels it. It is called from a command.
func (s *remoteWindowSource) LoadWindow(ctx context.Context, off, n int64) error {
	if n <= 0 {
		n = RemoteWindowSize
	}
	first := off / RemoteWindowSize
	last := (off + n - 1) / RemoteWindowSize
	for index := first; index <= last+1; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := index * RemoteWindowSize
		if start >= s.size {
			break
		}
		if index > last {
			// Prefetch only fits when the cache has room: do not evict a window the
			// painter is reading for a speculative one.
			if s.cacheLen() >= RemoteWindowCache {
				break
			}
		}
		if s.hasWindow(index) {
			continue
		}
		want := min(RemoteWindowSize, s.size-start)
		data, err := s.access.ReadRange(ctx, s.path, start, want)
		if err != nil {
			if errors.Is(err, io.EOF) && len(data) > 0 {
				// A short final window is normal: keep what came.
			} else {
				return err
			}
		}
		if int64(len(data)) > want {
			data = data[:want]
		}
		if int64(len(data)) < want && start+int64(len(data)) < s.size {
			return fmt.Errorf("remote large file: short window at %d: got %d of %d bytes", start, len(data), want)
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return fs.ErrClosed
		}
		if _, ok := s.windows[index]; !ok {
			s.windows[index] = s.lru.PushFront(&remoteWindow{index: index, data: data})
			for s.lru.Len() > RemoteWindowCache {
				s.evictOldestLocked()
			}
		}
		s.mu.Unlock()
		s.gen.Add(1)
	}
	return nil
}

// hasWindow reports whether window index is cached.
func (s *remoteWindowSource) hasWindow(index int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.windows[index]
	return ok
}

// cacheLen is the number of cached windows.
func (s *remoteWindowSource) cacheLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lru.Len()
}

// evictOldestLocked drops the least recently used window. s.mu must be held.
func (s *remoteWindowSource) evictOldestLocked() {
	e := s.lru.Back()
	if e == nil {
		return
	}
	s.lru.Remove(e)
	delete(s.windows, e.Value.(*remoteWindow).index)
}

// Generation returns how many windows have arrived.
func (s *remoteWindowSource) Generation() uint64 { return s.gen.Load() }

// Loaded reports whether [off, off+n) is entirely cached, without recording a miss.
// It implements buffer.WindowAvailability for line-aware readers such as the syntax
// highlighter, which must not turn a context read into an extra remote window load.
func (s *remoteWindowSource) Loaded(off, n int64) bool {
	if n <= 0 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	for pos := off; pos < off+n; {
		index := pos / RemoteWindowSize
		if _, ok := s.windows[index]; !ok {
			return false
		}
		pos = (index + 1) * RemoteWindowSize
	}
	return true
}

// TakePendingWindow returns and clears the first missed window.
func (s *remoteWindowSource) TakePendingWindow() (off, n int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	off, n, ok = s.pendingOff, s.pendingN, s.pendingOK
	s.pendingOK = false
	return off, n, ok
}

// Size implements buffer.Source.
func (s *remoteWindowSource) Size() int64 { return s.size }

// Check implements buffer.Source. A window source does not stat on a paint: the
// remote change watcher reports changes, and a save compares the stat itself.
func (s *remoteWindowSource) Check() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fs.ErrClosed
	}
	return nil
}

// Close releases the cached windows and makes later loads fail. No goroutine is
// owned by the source, so there is nothing else to stop.
func (s *remoteWindowSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.windows = make(map[int64]*list.Element)
	s.lru.Init()
	s.pendingOK = false
	return nil
}

// readRemoteHead reads at most n bytes at the start of path through a remote
// range reader. It mirrors readHead's local branch but takes the narrower
// interface the remote large-file path already has.
func readRemoteHead(ctx context.Context, access RangeAccess, path string, size, n int64) ([]byte, bool, error) {
	if n <= 0 || size <= 0 {
		return nil, false, nil
	}
	want := min(size, n)
	data, err := access.ReadRange(ctx, path, 0, want)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > want {
		data = data[:want]
	}
	return data, size > int64(len(data)), nil
}

// readRemoteLargeDocument opens path in large-file mode over a windowed remote
// source. Edits are an in-memory overlay over the windowed base; a non-UTF-8 file
// fails with ErrTooLargeToTranscode instead of loading whole.
func readRemoteLargeDocument(ctx context.Context, path string, disk diskState, spec loadSpec, access RangeAccess) (*Document, error) {
	// Probe the head before building the windowed source. A binary or an image
	// needs no line index, and the source would eagerly fetch a 1 MiB window only
	// to throw it away; text still opens the source below. The probe itself is
	// bounded to the same 64 KiB sample the local large path reads.
	head, _, err := readRemoteHead(ctx, access, path, disk.Size, largeSampleSize)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	more := func(n int64) ([]byte, error) {
		b, _, err := readRemoteHead(ctx, access, path, disk.Size, n)
		return b, err
	}
	if info, ok := imageInfoOfHead(head, disk.Size, more); ok {
		d := newImageDocument(path, disk, info)
		d.access = spec.Access
		return d, nil
	}
	if encoding.IsBinary(head) {
		d := binaryDocument(path, disk)
		d.access = spec.Access
		return d, nil
	}
	src, err := newRemoteWindowSource(ctx, access, path, disk.Size)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := readSample(path, src)
	if err != nil {
		_ = src.Close()
		var need ErrLargeNeedsFullLoad
		if errors.As(err, &need) {
			return nil, fmt.Errorf("open %s: remote large file is not UTF-8; a full remote load is not supported: %w", path, ErrTooLargeToTranscode)
		}
		return nil, err
	}
	if info.image != nil {
		// A picture whose header follows more than the 64 KiB head probe is still
		// an image: the source sample sees it, and the windowed text path must not
		// claim it.
		_ = src.Close()
		d := newImageDocument(path, disk, *info.image)
		d.access = spec.Access
		return d, nil
	}
	if info.binary {
		// A binary marker after the head probe is caught by the first source
		// window. It is a summary too; hex reads use bounded RangeAccess windows.
		_ = src.Close()
		d := binaryDocument(path, disk)
		d.access = spec.Access
		return d, nil
	}
	geom := spec.LargeConf.Geom
	geom.Skip = info.Skip
	// Only the first window exists when the file opens; index just enough of the
	// head to answer the first paint. The rest of the index is built by the root's
	// commands, loading windows as they are missed.
	if geom.InitialBytes <= 0 || geom.InitialBytes > remoteInitialIndexBytes {
		geom.InitialBytes = remoteInitialIndexBytes
	}
	buf, err := buffer.OpenLarge(src, geom)
	if err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	d := &Document{Path: path, Buffer: buf, Want: -1, Eol: info.Eol}
	d.Disk = disk
	d.access = spec.Access
	d.Codec = info.Codec
	d.encoding = info.Label
	d.large = &largeDoc{
		Size: src.Size(), EstLine: spec.LargeConf.EstLine, Batch: spec.LargeConf.Batch,
		Mtime: mtimeNano(disk), Skip: info.Skip, StashDir: spec.LargeConf.StashDir,
		RemoteWindowed: true,
	}
	return d, nil
}

// AsyncLarge reports whether d is a large document over an asynchronous window
// source (a remote large file).
func (d *Document) AsyncLarge() bool {
	return d.large != nil && d.Buffer != nil && d.Buffer.AsyncLarge()
}

// TakePendingLargeWindow returns the window the last paint read missed, for the root
// to load in a command. ok is false for a local large file or when nothing missed.
func (d *Document) TakePendingLargeWindow() (LargeWindow, bool) {
	if !d.AsyncLarge() {
		return LargeWindow{}, false
	}
	off, n, ok := d.Buffer.TakePendingWindow()
	if !ok {
		return LargeWindow{}, false
	}
	return LargeWindow{Path: d.Path, Off: off, N: n}, true
}

// LoadLargeWindow loads the windows covering w through the remote source. It blocks
// on the authority read and must run in a command; a closed or replaced buffer
// refuses. The path must be this document: a load cannot cross documents.
func (d *Document) LoadLargeWindow(ctx context.Context, w LargeWindow) error {
	if d.Path != w.Path || !d.AsyncLarge() {
		return fmt.Errorf("large window: not this remote document")
	}
	return d.Buffer.LoadWindow(ctx, w.Off, w.N)
}

// LineLoaded reports whether line i of the document can be read without a remote
// read or a pending-window request. It is true for memory buffers, local large files
// and overlay lines; a windowed remote large file reports false for a line in a
// window that has not arrived yet.
func (d *Document) LineLoaded(i int) bool {
	return d.Buffer != nil && d.Buffer.LineLoaded(i)
}

// WindowGeneration returns the source's window-arrival counter.
func (d *Document) WindowGeneration() uint64 {
	if d.Buffer == nil {
		return 0
	}
	return d.Buffer.WindowGeneration()
}
