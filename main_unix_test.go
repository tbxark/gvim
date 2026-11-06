//go:build unix

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestShutdownSignalsCancelTheContext checks that closing the terminal (SIGHUP) and a plain
// kill (SIGTERM) cancel the context of the program, as an interrupt does, so that the
// session and the unsaved text are saved on the way out.
func TestShutdownSignalsCancelTheContext(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
			defer stop()
			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatalf("%v did not cancel the context", sig)
			}
		})
	}
}
