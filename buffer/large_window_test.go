package buffer_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// fakeWindowSource is a WindowSource over synthetic bytes. While syncLoads is set it
// serves misses synchronously (so OpenLarge can index its prefix); afterwards a miss
// records the window and returns buffer.ErrSourcePending immediately, and only
// LoadWindow performs the "remote" read.
type fakeWindowSource struct {
	data      []byte
	window    int64
	syncLoads atomic.Bool

	mu       sync.Mutex
	cache    map[int64][]byte
	order    []int64
	maxWin   int
	pending  int64
	pendingN int64
	havePend bool
	closed   bool
	gen      atomic.Uint64
	loads    atomic.Int64
	readMiss atomic.Int64
}

func newFakeWindowSource(data string) *fakeWindowSource {
	s := &fakeWindowSource{data: []byte(data), window: 8, cache: map[int64][]byte{}, maxWin: 2}
	s.syncLoads.Store(true)
	return s
}

func (s *fakeWindowSource) Size() int64  { return int64(len(s.data)) }
func (s *fakeWindowSource) Check() error { return nil }
func (s *fakeWindowSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cache = map[int64][]byte{}
	return nil
}
func (s *fakeWindowSource) Generation() uint64 { return s.gen.Load() }

func (s *fakeWindowSource) TakePendingWindow() (int64, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	off, n, ok := s.pending, s.pendingN, s.havePend
	s.havePend = false
	return off, n, ok
}

func (s *fakeWindowSource) ReadAt(p []byte, off int64) (int, error) {
	if s.syncLoads.Load() {
		if err := s.LoadWindow(context.Background(), off, int64(len(p))); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	read := 0
	for read < len(p) {
		pos := off + int64(read)
		index := pos / s.window
		data, ok := s.cache[index]
		if !ok {
			s.readMiss.Add(1)
			s.pending, s.pendingN, s.havePend = index*s.window, s.window, true
			return read, buffer.ErrSourcePending
		}
		skip := pos - index*s.window
		n := copy(p[read:], data[skip:])
		if n == 0 {
			return read, io.EOF
		}
		read += n
	}
	return read, nil
}

func (s *fakeWindowSource) LoadWindow(ctx context.Context, off, n int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.loads.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	first := off / s.window
	last := (off + n - 1) / s.window
	for index := first; index <= last; index++ {
		start := index * s.window
		if start >= int64(len(s.data)) {
			break
		}
		end := min(start+s.window, int64(len(s.data)))
		s.cache[index] = append([]byte(nil), s.data[start:end]...)
		s.order = append(s.order, index)
		for len(s.order) > s.maxWin {
			old := s.order[0]
			s.order = s.order[1:]
			delete(s.cache, old)
		}
	}
	s.gen.Add(1)
	return nil
}

// completeWindowedIndex drives the async index scan to completion, loading the
// windows it misses the way the root's command does.
func completeWindowedIndex(t *testing.T, b *buffer.Buffer, src *fakeWindowSource) {
	t.Helper()
	ctx := context.Background()
	for range 64 {
		scan := b.BeginIndexScan()
		if scan == nil {
			return
		}
		batch, err := scan.Next(ctx, 1<<20)
		scan.Close()
		b.ApplyIndex(batch)
		if errors.Is(err, buffer.ErrSourcePending) {
			off, n, ok := b.TakePendingWindow()
			if !ok {
				t.Fatalf("an index read missed a window without recording it")
			}
			if err := b.LoadWindow(ctx, off, n); err != nil {
				t.Fatalf("loading the missed window: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("index scan: %v", err)
		}
		if batch.Done {
			return
		}
	}
	t.Fatal("index did not complete")
}

func openFakeLarge(t *testing.T, src *fakeWindowSource) *buffer.Buffer {
	t.Helper()
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: 8, CachePages: 2, InitialBytes: 1})
	if err != nil {
		t.Fatalf("OpenLarge: %v", err)
	}
	src.syncLoads.Store(false)
	return b
}

func TestWindowSourceMissReturnsImmediately(t *testing.T) {
	t.Parallel()
	src := newFakeWindowSource("line0\nline1\nline2\nline3\n")
	b := openFakeLarge(t, src)
	defer func() { _ = b.Close() }()
	completeWindowedIndex(t, b, src)
	if !b.AsyncLarge() {
		t.Fatal("buffer is not reported as async large")
	}
	// The index scan loaded the later windows and evicted the first page
	// (CachePages 2). Find a line whose read now misses: the paint path must return
	// a blank placeholder immediately.
	missLine, missText := -1, ""
	for i, text := range []string{"line0", "line1", "line2", "line3"} {
		loadsBefore, readsBefore := src.loads.Load(), src.readMiss.Load()
		if got := b.Line(i); got != "" {
			continue
		}
		if src.loads.Load() != loadsBefore {
			t.Fatalf("ReadAt performed %d remote loads", src.loads.Load()-loadsBefore)
		}
		if src.readMiss.Load() == readsBefore {
			t.Fatal("no miss was recorded")
		}
		missLine, missText = i, text
		break
	}
	if missLine < 0 {
		t.Fatal("no line missed after the index scan; the cache test is not exercising a miss")
	}
	if err := b.SourceErr(); err != nil {
		t.Fatalf("a window miss froze the buffer: %v", err)
	}
	off, n, ok := b.TakePendingWindow()
	if !ok {
		t.Fatal("no pending window after the miss")
	}
	if err := b.LoadWindow(context.Background(), off, n); err != nil {
		t.Fatalf("LoadWindow: %v", err)
	}
	if got := b.Line(missLine); got != missText {
		t.Fatalf("Line(%d) after load = %q, want %q", missLine, got, missText)
	}
	if b.WindowGeneration() == 0 {
		t.Fatal("the window generation did not change")
	}
}

func TestWindowSourcePendingDoesNotFreezeOrChangeSource(t *testing.T) {
	t.Parallel()
	src := newFakeWindowSource("line0\nline1\nline2\nline3\n")
	b := openFakeLarge(t, src)
	defer func() { _ = b.Close() }()
	completeWindowedIndex(t, b, src)
	_ = b.Line(0) // miss
	if err := b.SourceErr(); err != nil {
		t.Fatalf("SourceErr after a miss = %v, want nil", err)
	}
	if err := b.CheckSource(); err != nil {
		t.Fatalf("CheckSource after a miss = %v, want nil", err)
	}
}

func TestWindowSourceCloseReleases(t *testing.T) {
	t.Parallel()
	src := newFakeWindowSource("line0\nline1\n")
	b := openFakeLarge(t, src)
	completeWindowedIndex(t, b, src)
	_ = b.Line(0)
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	src.mu.Lock()
	closed := src.closed
	src.mu.Unlock()
	if !closed {
		t.Fatal("the source was not closed")
	}
}

func TestWindowSourceLocalFileIsNotAsync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/f.txt"
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := buffer.OpenFileSource(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: 8, CachePages: 2, InitialBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	if b.AsyncLarge() {
		t.Fatal("a local file source reported async large")
	}
	if b.WindowGeneration() != 0 {
		t.Fatalf("local window generation = %d, want 0", b.WindowGeneration())
	}
	if err := b.LoadWindow(context.Background(), 0, 1); !errors.Is(err, buffer.ErrNotWindowed) {
		t.Fatalf("LoadWindow on a local source = %v, want ErrNotWindowed", err)
	}
}

func TestWindowSourceWriteToLoadsPendingSpanAndCopy(t *testing.T) {
	t.Parallel()
	const text = "alpha01\nbravo02\ncharl03\ndelta04\necho005\nfoxt006\ngolf007\nhotel08\n"
	src := newFakeWindowSource(text)
	src.window = 16
	src.maxWin = 3 // 48 bytes cached, so 64-byte text still evicts window 0
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: 16, CachePages: 1, InitialBytes: 1})
	if err != nil {
		t.Fatalf("OpenLarge: %v", err)
	}
	src.syncLoads.Store(false)
	defer func() { _ = b.Close() }()
	completeWindowedIndex(t, b, src)

	// Load lines 2..3 (window 1) before editing line 3 so settleEOL can read line 2's EOL.
	for b.Line(2) == "" || b.Line(3) == "" {
		off, n, ok := b.TakePendingWindow()
		if !ok {
			t.Fatal("missing line did not record pending window")
		}
		if err := b.LoadWindow(context.Background(), off, n); err != nil {
			t.Fatalf("LoadWindow: %v", err)
		}
	}
	b.Insert(buffer.Pos{Line: 3, Col: 0}, "EDITED ")
	if ref := b.Refusal(); ref != nil {
		t.Fatalf("Insert refused: %v", ref)
	}
	// Seed an unrelated pending window so byte-copy cannot rely on TakePendingWindow
	// returning its own window and must call LoadWindow(ctx, off, n) directly.
	_, _ = src.ReadAt(make([]byte, 1), 0)

	snap := b.Snapshot()
	defer snap.Release()
	var out bytes.Buffer
	res, err := snap.WriteTo(context.Background(), &out, buffer.WriteOptions{})
	if err != nil {
		t.Fatalf("Snapshot.WriteTo: %v", err)
	}
	want := strings.Replace(text, "delta04", "EDITED delta04", 1)
	if out.String() != want || res.Bytes != int64(len(want)) {
		t.Fatalf("WriteTo = %q (bytes=%d), want %q (bytes=%d)", out.String(), res.Bytes, want, len(want))
	}
}
