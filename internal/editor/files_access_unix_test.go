//go:build unix

package editor

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// unwritable runs on the way to the first frame, so it must not open the file: opening a
// FIFO for writing blocks until a reader shows up, and some files block in open for
// other reasons (a hung mount, a sandbox).
func TestUnwritableDoesNotOpenTheFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// A reader unblocks the writer if the probe opens the file, so a failure leaks nothing.
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
	})
	done := make(chan bool, 1)
	go func() { done <- unwritable(path) }()
	select {
	case got := <-done:
		if got {
			t.Fatal("unwritable(fifo with mode 0600) = true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unwritable blocked on a file that cannot be opened for writing without a reader")
	}
}
