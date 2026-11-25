package terminal

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// feedProc is a fake ptyProc for feed-level tests: the child never runs, its
// output is pushed through Session.feed directly, and the bytes the session
// writes back (CSI-u replies, OSC 52 answers, user input) land in in. Read
// blocks until Close so the session's read goroutine simply waits.
type feedProc struct {
	mu     sync.Mutex
	in     bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func newFeedProc() *feedProc {
	return &feedProc{closed: make(chan struct{})}
}

func (p *feedProc) Read([]byte) (int, error) {
	<-p.closed
	return 0, io.EOF
}

func (p *feedProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in.Write(b)
}

func (p *feedProc) Resize(int, int) error { return nil }
func (p *feedProc) Kill() error           { p.close(); return nil }
func (p *feedProc) Wait() error           { <-p.closed; return nil }
func (p *feedProc) Close() error          { p.close(); return nil }

func (p *feedProc) close() { p.once.Do(func() { close(p.closed) }) }

func (p *feedProc) supportsForegroundProcess() bool       { return false }
func (p *feedProc) foregroundProcessName() (string, bool) { return "", false }

// written reports the bytes the session has sent the child so far.
func (p *feedProc) written() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in.String()
}

// reset drops the bytes written so far, so a waitWritten match covers only what
// follows it.
func (p *feedProc) reset() {
	p.mu.Lock()
	p.in.Reset()
	p.mu.Unlock()
}

// startFeedSession builds a Session over the fake pty: feed drives the emulator
// without a real child or PTY, so the tests are platform-independent.
func startFeedSession(t *testing.T) (*Session, *feedProc) {
	t.Helper()
	p := newFeedProc()
	s := newSession(p, 80, 24, 0)
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}

// waitWritten returns once the session has sent exactly want to the child's
// input. Replies travel a single writer goroutine in order, so an exact match
// also proves that earlier sequences in the same feed queued nothing.
func waitWritten(t *testing.T, p *feedProc, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for p.written() != want {
		if time.Now().After(deadline) {
			t.Fatalf("child input = %q, want %q", p.written(), want)
		}
		time.Sleep(time.Millisecond)
	}
}
