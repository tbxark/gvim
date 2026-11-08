//go:build unix

package ui

import (
	"os"
	"os/signal"
	"syscall"
)

// onContinue calls f each time the process continues after a stop, and returns the
// function that ends the watch. A resize that happens while the process is stopped is
// not always delivered as a SIGWINCH afterwards, so f is where the size is read again.
func onContinue(f func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(ch, syscall.SIGCONT)
	go func() {
		for {
			select {
			case <-ch:
				f()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
