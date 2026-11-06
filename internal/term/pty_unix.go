//go:build unix

package term

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// spawn starts cmd attached to a new PTY of the given size and returns the
// master side.
func spawn(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// setSize resizes the PTY behind ptmx.
func setSize(ptmx *os.File, cols, rows int) error {
	return pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
