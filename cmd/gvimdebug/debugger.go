package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/tbxark/gvim/internal/terminal"
)

// debugger runs gvim in a pseudo-terminal and keeps the session the web page shows.
type debugger struct {
	ctx      context.Context
	tmp      string   // holds the built binary and the isolated home
	args     []string // arguments for gvim
	dir      string   // working directory of gvim
	bin      string   // gvim binary to run; empty means build it on each start
	realHome bool     // keep the user's HOME and config
	font     string   // font file the page loads, served at /font; empty for none

	restartMu sync.Mutex // one restart at a time
	mu        sync.Mutex // guards sess, state, gen, cols and rows
	cols      int        // terminal size, changed by resize
	rows      int
	sess      *terminal.Session
	state     string // the status line: running, exited, building, and so on
	// gen is closed at the next change the page must see, and then set to nil. Event
	// streams wait on it, see changed.
	gen chan struct{}
}

// restart stops gvim, builds it again if no binary was given, and starts it. A failed
// build or start leaves gvim stopped, and the reason is in the status line.
func (d *debugger) restart() error {
	d.restartMu.Lock()
	defer d.restartMu.Unlock()
	d.stop()
	d.setState("building")
	bin, err := d.binary()
	if err != nil {
		d.setState("build failed")
		return err
	}
	env, err := d.env()
	if err != nil {
		d.setState("start failed")
		return err
	}
	d.mu.Lock()
	cols, rows := d.cols, d.rows
	d.mu.Unlock()
	sess, err := terminal.Start(d.ctx, terminal.Options{
		Shell: bin,
		Args:  d.args,
		Dir:   d.dir,
		Env:   env,
		Cols:  cols,
		Rows:  rows,
	})
	if err != nil {
		d.setState("start failed")
		return err
	}
	d.mu.Lock()
	d.sess = sess
	d.state = "running"
	d.mu.Unlock()
	d.bump()
	go d.watch(sess)
	go d.forward(sess)
	return nil
}

// stop closes the running gvim, if there is one. The last screen stays readable.
func (d *debugger) stop() {
	d.mu.Lock()
	sess := d.sess
	d.sess = nil
	d.state = "stopped"
	d.mu.Unlock()
	d.bump()
	if sess != nil {
		_ = sess.Close()
	}
}

// watch records how gvim ended. It does nothing once the session has been replaced
// or stopped, because then the exit is expected.
func (d *debugger) watch(sess *terminal.Session) {
	<-sess.Done()
	err := sess.Wait()
	d.mu.Lock()
	if d.sess != sess {
		d.mu.Unlock()
		return
	}
	if err != nil {
		d.state = "exited: " + err.Error()
	} else {
		d.state = "exited: status 0"
	}
	d.mu.Unlock()
	d.bump()
}

// forward wakes the event streams on each screen change of sess. The screen changes
// happen on gvim's output, and the channel closes when that output ends.
func (d *debugger) forward(sess *terminal.Session) {
	for range sess.Updates() {
		d.bump()
	}
}

// changed returns a channel that closes at the next change the page must see. Take the
// channel before the state it guards, so that a change in between is not missed.
func (d *debugger) changed() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gen == nil {
		d.gen = make(chan struct{})
	}
	return d.gen
}

// bump wakes every event stream, after a change. Call it without holding mu.
func (d *debugger) bump() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gen != nil {
		close(d.gen)
		d.gen = nil
	}
}

// snapshot returns what the page shows. Without a gvim the screen fields are empty.
func (d *debugger) snapshot() pageState {
	d.mu.Lock()
	sess, state, cols, rows := d.sess, d.state, d.cols, d.rows
	d.mu.Unlock()
	st := pageState{Status: state, Cols: cols, Rows: rows}
	if sess == nil {
		return st
	}
	sc := sess.Screen()
	st.Frame = frameHTML(sc)
	st.Cursor = pageCursor{Row: sc.Cursor.Row, Col: sc.Cursor.Col, Visible: sc.Cursor.Visible}
	return st
}

// current returns the session of the running gvim, or nil when there is none.
func (d *debugger) current() *terminal.Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sess
}

// status returns the status line.
func (d *debugger) status() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func (d *debugger) setState(state string) {
	d.mu.Lock()
	d.state = state
	d.mu.Unlock()
	d.bump()
}

// resize sets the terminal size. A running gvim is resized at once, and the next start
// uses the same size.
func (d *debugger) resize(cols, rows int) error {
	d.mu.Lock()
	sess := d.sess
	d.cols, d.rows = cols, rows
	d.mu.Unlock()
	if sess != nil {
		if err := sess.Resize(cols, rows); err != nil {
			return fmt.Errorf("resize: %w", err)
		}
	}
	d.bump()
	return nil
}

// binary returns the gvim binary to run: the one given with -bin, or a fresh build.
func (d *debugger) binary() (string, error) {
	if d.bin != "" {
		return d.bin, nil
	}
	return build(d.ctx, d.tmp)
}

// build compiles gvim from this module into dir and returns the binary's path. It
// runs in the current directory, so that must be inside the module.
func build(ctx context.Context, dir string) (string, error) {
	out := filepath.Join(dir, "gvim")
	b, err := exec.CommandContext(ctx, "go", "build", "-o", out, "github.com/tbxark/gvim").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go build: %w\n%s", err, b)
	}
	return out, nil
}

// env isolates gvim's config and state in the temp directory, so a debug run does not
// read or write the user's session, recent files or config. The terminal variables that
// gvim checks to pick its icon mode are cleared, so every run looks the same.
func (d *debugger) env() ([]string, error) {
	if d.realHome {
		return nil, nil
	}
	home := filepath.Join(d.tmp, "home")
	for _, sub := range []string{"home", "config", "state", "cache", "data"} {
		if err := os.MkdirAll(filepath.Join(d.tmp, sub), 0o755); err != nil {
			return nil, err
		}
	}
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(d.tmp, "config"),
		"XDG_STATE_HOME=" + filepath.Join(d.tmp, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(d.tmp, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(d.tmp, "data"),
		"KITTY_WINDOW_ID=",
		"WEZTERM_EXECUTABLE=",
		"GHOSTTY_RESOURCES_DIR=",
		"TERM_PROGRAM=",
	}, nil
}
