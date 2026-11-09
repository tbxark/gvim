//go:build unix

package terminal

import (
	"os"
	"os/exec"
	"sync"

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
	// fdMu guards the master fd against Close while a foreground-process poll is
	// reading the terminal's foreground process group (tcgetpgrp). The poll holds
	// it only for the ioctl, not for the process-name lookup (which uses the pgid
	// alone), so Close is not delayed by a ps spawn.
	fdMu   sync.RWMutex
	closed bool
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
func (p *unixPTY) Close() error {
	p.fdMu.Lock()
	p.closed = true
	err := p.ptmx.Close()
	p.fdMu.Unlock()
	return err
}

// supportsForegroundProcess reports that a local Unix PTY can expose the
// foreground process group.
func (p *unixPTY) supportsForegroundProcess() bool { return true }
