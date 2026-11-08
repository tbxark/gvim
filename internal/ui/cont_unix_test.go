//go:build unix

package ui

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestOnContinueCallsBackOnSIGCONT checks that the size is read again when the process
// continues, and not any more after the watch ended.
func TestOnContinueCallsBackOnSIGCONT(t *testing.T) {
	calls := make(chan struct{}, 4)
	stop := onContinue(func() { calls <- struct{}{} })
	if err := syscall.Kill(os.Getpid(), syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGCONT did not call back")
	}
	stop()
	_ = syscall.Kill(os.Getpid(), syscall.SIGCONT)
	select {
	case <-calls:
		t.Error("called back after the watch ended")
	case <-time.After(100 * time.Millisecond):
	}
}
