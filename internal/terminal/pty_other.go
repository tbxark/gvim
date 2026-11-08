//go:build !unix && !windows

package terminal

import "os/exec"

// spawn reports ErrUnsupported: PTY sessions are implemented on Unix and, on
// Windows, through ConPTY (pty_windows.go).
func spawn(cmd *exec.Cmd, cols, rows int) (ptyProc, error) {
	return nil, ErrUnsupported
}
