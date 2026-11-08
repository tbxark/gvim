//go:build unix

package terminal

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// spawn starts cmd attached to a new PTY of the given size and returns the
// platform handle of the terminal and the child behind it.
func spawn(cmd *exec.Cmd, cols, rows int) (ptyProc, error) {
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixPTY{ptmx: ptmx, cmd: cmd}, nil
}

// unixPTY is a Unix PTY master and the child process.
type unixPTY struct {
	ptmx *os.File
	cmd  *exec.Cmd
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.ptmx.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.ptmx.Write(b) }

// Resize resizes the PTY behind the master.
func (p *unixPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Kill terminates the child, if it is still running.
func (p *unixPTY) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Wait waits for the child and returns its exit status.
func (p *unixPTY) Wait() error { return p.cmd.Wait() }

// Close releases the PTY master.
func (p *unixPTY) Close() error { return p.ptmx.Close() }
