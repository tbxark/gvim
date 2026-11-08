//go:build unix

package terminal

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeBackend is a terminal transport fed by a pipe: the test writes bytes to out to make
// the session paint, and reads input / resizes / close calls back.
type fakeBackend struct {
	mu        sync.Mutex
	in        bytes.Buffer
	resizes   [][2]int
	killed    bool
	closed    bool
	outR      *io.PipeReader
	outW      *io.PipeWriter
	waitCh    chan struct{}
	closeOnce sync.Once
}

func newFakeBackend() *fakeBackend {
	r, w := io.Pipe()
	return &fakeBackend{outR: r, outW: w, waitCh: make(chan struct{})}
}

func (b *fakeBackend) Read(p []byte) (int, error) { return b.outR.Read(p) }
func (b *fakeBackend) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.in.Write(p)
}
func (b *fakeBackend) Resize(cols, rows int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resizes = append(b.resizes, [2]int{cols, rows})
	return nil
}
func (b *fakeBackend) Kill() error {
	b.mu.Lock()
	b.killed = true
	b.mu.Unlock()
	b.stop()
	return nil
}
func (b *fakeBackend) Wait() error {
	<-b.waitCh
	return nil
}
func (b *fakeBackend) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.stop()
	return nil
}
func (b *fakeBackend) stop() {
	b.closeOnce.Do(func() {
		close(b.waitCh)
		_ = b.outW.Close()
	})
}
func (b *fakeBackend) input() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.in.String()
}
func (b *fakeBackend) lastResize() ([2]int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.resizes) == 0 {
		return [2]int{}, false
	}
	return b.resizes[len(b.resizes)-1], true
}

// TestStartWithBackend checks a session over a non-PTY transport: output paints,
// input is written through, resize reaches the backend and Close releases it.
func TestStartWithBackend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newFakeBackend()
	s, err := StartWithBackend(ctx, Options{Cols: 80, Rows: 24}, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSession(t, s) })

	if _, err := b.outW.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "the backend output", hasText("hello"))
	if sc := s.Screen(); sc.Cols != 80 || sc.Rows != 24 {
		t.Fatalf("screen = %dx%d, want 80x24", sc.Cols, sc.Rows)
	}
	if _, err := s.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for b.input() != "x" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := b.input(); got != "x" {
		t.Fatalf("backend input = %q", got)
	}
	if err := s.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "the resize", func(sc Screen) bool { return sc.Cols == 100 && sc.Rows == 30 })
	if got, ok := b.lastResize(); !ok || got != [2]int{100, 30} {
		t.Fatalf("backend resize = %v, %v", got, ok)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if !closed {
		t.Fatal("Close did not reach the backend")
	}
}

// TestStartWithBackendBadOptions checks the size validation is unchanged over a backend.
func TestStartWithBackendBadOptions(t *testing.T) {
	if _, err := StartWithBackend(context.Background(), Options{Cols: 70000, Rows: 24}, newFakeBackend()); err == nil {
		t.Fatal("an oversized size was accepted")
	}
	if _, err := StartWithBackend(context.Background(), Options{Cols: 1, Rows: 1}, nil); err == nil {
		t.Fatal("a nil backend was accepted")
	}
}
