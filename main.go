// Command gvim is the terminal editor: a Go port of the Fresh TUI.
//
// Usage:
//
//	gvim [DIR] [FILE[:LINE[:COL]]]...
//
// Run gvim --help for the full command tree and the flags.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/tbxark/gvim/internal/app"
	"github.com/tbxark/gvim/internal/cli"
)

// shutdownSignals end the program the way a quit does: the session and the unsaved text are
// saved before the process exits. SIGHUP is the closing of the terminal window and SIGTERM
// is a plain kill, and either would otherwise end the process with the unsaved text lost.
// SIGQUIT would make the Go runtime dump the goroutines and exit without restoring the
// terminal.
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
	code := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, app.Run)
	// os.Exit skips deferred calls, so stop is called first.
	stop()
	os.Exit(code)
}
