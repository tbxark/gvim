//go:build unix

package daemon

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
)

// watchResize forwards terminal size changes to the daemon until the returned stop
// function is called. The client terminal owns the size (the daemon program runs
// without a terminal), so every SIGWINCH becomes a resize frame.
func watchResize(fw *FrameWriter) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				if w, h, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 && h > 0 {
					_ = fw.WriteJSON(TypeResize, Resize{Cols: w, Rows: h})
				}
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
