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

	"github.com/tbxark/gvim/internal/app"
	"github.com/tbxark/gvim/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, app.Run)
	// os.Exit skips deferred calls, so stop is called first.
	stop()
	os.Exit(code)
}
