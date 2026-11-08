// Package terminal runs a program in a pseudo-terminal and keeps a model of its
// screen, so that a UI pane can show it.
//
// On Unix the PTY comes from github.com/creack/pty. The child runs in a new
// session with the PTY as its controlling terminal. Its output is parsed by the
// terminal emulator in github.com/charmbracelet/x/vt, and Screen returns a copy
// of the emulated grid.
//
// Windows is not supported: ConPTY is not implemented, so Start returns
// ErrUnsupported there.
//
// Screen exposes the rune and the basic character style of each cell, and the
// cursor position and visibility. It does not expose cell widths or grapheme
// clusters beyond their first rune.
//
// The session also exposes what a terminal UI needs around the screen:
// Scrollback returns a bounded snapshot of the main screen's history, and
// ScrollbackLen its size; WorkingDirectory is the directory the child reported
// with OSC 7; AltScreen and MouseReporting are the modes the child turned on;
// SendMouse forwards a mouse event in the encoding the child asked for; and
// SearchScrollback finds a plain substring in the history. The alternate screen
// has no history.
//
// Updates signals each screen change, so a UI can repaint when the child prints.
// Paste sends text the way the child expects a paste, with bracketed-paste
// markers when the child has enabled that mode.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
)

// ErrUnsupported is returned by Start on platforms without PTY support. It is
// errors.ErrUnsupported, so errors.Is matches either name.
var ErrUnsupported = errors.ErrUnsupported

const (
	defaultCols = 80
	defaultRows = 24
)

// Options describes the program a Session runs.
type Options struct {
	// Shell is the program to run. Empty means $SHELL, and then /bin/sh.
	Shell string
	// Args are the arguments passed to Shell.
	Args []string
	// Dir is the working directory. Empty means the current directory.
	Dir string
	// Env entries are appended to the inherited environment, so they replace
	// inherited variables with the same name. TERM is xterm-256color unless
	// Env sets it.
	Env []string
	// Cols and Rows are the initial size. Zero or negative means 80x24.
	Cols, Rows int
	// ScrollbackLines is how many lines of history the emulator keeps for the
	// main screen. Zero means terminal.DefaultScrollbackLines; a negative value
	// disables the scrollback.
	ScrollbackLines int
}

// DefaultScrollbackLines is the history a session keeps when Options does not
// say: the same 10 000 lines x/vt keeps by default.
const DefaultScrollbackLines = 10000

// Start runs opts.Shell with opts.Args in a new pseudo-terminal.
//
// The session is released by Close, or when ctx is cancelled. Close must be
// called even after the child has exited, to release the PTY.
func Start(ctx context.Context, opts Options) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	if err := checkSize(cols, rows); err != nil {
		return nil, err
	}

	shell := opts.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = append(append(os.Environ(), "TERM=xterm-256color"), opts.Env...)

	ptmx, err := spawn(cmd, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("term: start %s: %w", shell, err)
	}
	s := newSession(ptmx, cmd, cols, rows, opts.ScrollbackLines)
	go s.closeOnCancel(ctx)
	return s, nil
}

// checkSize rejects sizes that a PTY cannot hold.
func checkSize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > math.MaxUint16 || rows > math.MaxUint16 {
		return fmt.Errorf("term: invalid size %dx%d", cols, rows)
	}
	return nil
}
