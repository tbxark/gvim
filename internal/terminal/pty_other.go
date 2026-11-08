//go:build !unix

package terminal

import (
	"os"
	"os/exec"
)

// spawn reports ErrUnsupported: PTY sessions are only implemented on Unix.
func spawn(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	return nil, ErrUnsupported
}

// setSize reports ErrUnsupported: PTY sessions are only implemented on Unix.
func setSize(ptmx *os.File, cols, rows int) error {
	return ErrUnsupported
}
