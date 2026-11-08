package lsp

import (
	"io"
	"strings"
	"testing"
	"time"
)

// stdioRWC is an io.ReadWriteCloser for a client the caller did not start: reads block on
// a pipe (the client read loop), writes are discarded (the test sends no request).
type stdioRWC struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (s *stdioRWC) Read(p []byte) (int, error) { return s.r.Read(p) }

func (s *stdioRWC) Write(p []byte) (int, error) { return len(p), nil }

func (s *stdioRWC) Close() error {
	_ = s.w.Close()
	_ = s.r.Close()
	return nil
}

// TestNewClientWithErrorOutputCapturesStderr checks the caller-launched transport path:
// the server error output reaches StderrTail and the limits note is recorded, without a
// local process behind the client.
func TestNewClientWithErrorOutputCapturesStderr(t *testing.T) {
	rwcR, rwcW := io.Pipe()
	errR, errW := io.Pipe()
	c := NewClientWithErrorOutput(&stdioRWC{r: rwcR, w: rwcW}, Config{}, errR)
	c.SetLimitsNote("process_limits: not applied remotely")
	defer func() { _ = c.Close() }()
	if _, err := io.WriteString(errW, "first line\nsecond line\n"); err != nil {
		t.Fatal(err)
	}
	_ = errW.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.StderrTail(), "second line") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := c.StderrTail(); !strings.Contains(got, "second line") {
		t.Fatalf("StderrTail = %q, want the remote server error output", got)
	}
	if got := c.LimitsNote(); !strings.Contains(got, "not applied remotely") {
		t.Fatalf("LimitsNote = %q", got)
	}
}
