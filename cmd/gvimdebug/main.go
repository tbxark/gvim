// Command gvimdebug runs gvim in a pseudo-terminal and serves its screen as a web
// page, so the UI can be watched and driven while it is developed. The arguments
// after the flags go to gvim unchanged:
//
//	go run ./cmd/gvimdebug [flags] [--] [DIR] [FILE[:LINE[:COL]]]...
//
// The page is at the -addr address, http://127.0.0.1:8765/ by default. It shows the
// screen, and sends keys, text and mouse input to gvim. Restart builds gvim from this
// module again and starts it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	os.Exit(run())
}

// run starts the debug page and returns the exit code.
func run() int {
	addr := flag.String("addr", "127.0.0.1:8765", "address the debug page listens on")
	cols := flag.Int("cols", 140, "gvim terminal width in columns")
	rows := flag.Int("rows", 40, "gvim terminal height in rows")
	bin := flag.String("bin", "", "gvim binary to run; empty builds this module into a temp directory on each start")
	realHome := flag.Bool("real-home", false, "keep the user's HOME and config instead of a temp directory")
	font := flag.String("font", "", "font file the page loads for the screen, so the browser does not have to find the font by name")
	flag.Parse()
	if *font != "" {
		if _, err := os.Stat(*font); err != nil {
			fmt.Fprintln(os.Stderr, "gvimdebug: -font:", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tmp, err := os.MkdirTemp("", "gvimdebug-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gvimdebug:", err)
		return 1
	}
	// The defers run in reverse: gvim stops first, then the temp directory goes.
	defer func() { _ = os.RemoveAll(tmp) }()
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gvimdebug:", err)
		return 1
	}
	d := &debugger{
		ctx:      ctx,
		tmp:      tmp,
		args:     flag.Args(),
		dir:      dir,
		bin:      *bin,
		realHome: *realHome,
		font:     *font,
		cols:     *cols,
		rows:     *rows,
		state:    "stopped",
	}
	defer d.stop()
	if err := d.restart(); err != nil {
		// The page shows the reason in its status line, so it is served anyway.
		fmt.Fprintln(os.Stderr, "gvimdebug:", err)
	}

	srv := &http.Server{Addr: *addr, Handler: d.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Fprintf(os.Stderr, "gvimdebug: open http://%s/ (gvim args: %v, temp: %s)\n",
		*addr, d.args, tmp)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "gvimdebug:", err)
		return 1
	}
	return 0
}
