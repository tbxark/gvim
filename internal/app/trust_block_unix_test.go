//go:build unix

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/tbxark/gvim/trust"
)

// TestTrustDecideStoreWriteDoesNotBlockState checks the store write runs outside the
// lock State takes on the update goroutine: the store file is a FIFO, so its read
// blocks until a writer opens it.
func TestTrustDecideStoreWriteDoesNotBlockState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("no FIFO: %v", err)
	}
	c := newTrustController(trust.NewStore(path), trust.State{Root: t.TempDir(), Level: trust.Restricted, Decided: true})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.Decide(trust.Trusted)
	}()
	time.Sleep(100 * time.Millisecond) // let Decide reach the blocked store read
	got := make(chan trust.State, 1)
	go func() { got <- c.State() }()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("State blocked while Decide wrote the store")
	}
	// Release the read so Decide (and its write, which replaces the FIFO) can finish.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Decide did not finish")
	}
}
