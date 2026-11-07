//go:build unix

package editor

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A FIFO blocks os.ReadFile in open until a writer shows up. readDocument runs on the
// UI goroutine for a plugin's gvim.buf.open, so it must refuse the file without opening it.
func TestReadDocumentRefusesSpecialFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// Opening both ends unblocks the reader if the guard is missing, so a failure does not leak it.
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
	})
	type result struct {
		d   *Document
		err error
	}
	done := make(chan result, 1)
	go func() {
		d, err := readDocument(path, loadSpec{})
		done <- result{d, err}
	}()
	select {
	case r := <-done:
		if r.err == nil || !strings.Contains(r.err.Error(), "not a regular file") {
			t.Fatalf("readDocument(fifo) = %v, %v; want a not-a-regular-file error", r.d, r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("readDocument blocked on a FIFO")
	}
}
