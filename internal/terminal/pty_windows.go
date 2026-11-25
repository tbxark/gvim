//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/charmbracelet/x/conpty"
	"golang.org/x/sys/windows"
)

// ConPTY: Windows' pseudo console. The child talks to a pseudo console object
// instead of a Unix PTY; the console, the pipes behind it and the process
// creation are Win32 calls made by github.com/charmbracelet/x/conpty. See
// docs/terminal.md for the build/verification story and the real-Windows
// acceptance checklist.

// spawn starts cmd in a new pseudo console of the given size.
func spawn(cmd *exec.Cmd, cols, rows int) (ptyProc, error) {
	cpty, err := conpty.New(cols, rows, 0)
	if err != nil {
		return nil, fmt.Errorf("term: ConPTY unavailable (Windows 10 version 1809, build 17763, or later is required; Windows Server 2019+): %w", err)
	}
	args := cmd.Args
	if len(args) == 0 {
		args = []string{cmd.Path}
	}
	// conpty fills in the default environment when Env is nil and builds the
	// deduplicated, sorted environment block itself.
	pid, handle, err := cpty.Spawn(cmd.Path, args, &syscall.ProcAttr{
		Dir: cmd.Dir,
		Env: cmd.Env,
		Sys: cmd.SysProcAttr,
	})
	if err != nil {
		_ = cpty.Close()
		return nil, err
	}
	// conpty returns its own handle to the new process and never closes it; Process
	// opens one of its own, so the first is released once that exists (the pid stays
	// valid while any handle is open).
	cmd.Process, err = os.FindProcess(pid)
	_ = windows.CloseHandle(windows.Handle(handle))
	if err != nil {
		_ = cpty.Close()
		return nil, err
	}
	return &windowsPTY{cpty: cpty, cmd: cmd}, nil
}

// windowsPTY is a ConPTY console and the child process behind it.
type windowsPTY struct {
	cpty *conpty.ConPty
	cmd  *exec.Cmd
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.cpty.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.cpty.Write(b) }

// Resize changes the console size, which is what the child sees.
func (p *windowsPTY) Resize(cols, rows int) error {
	return p.cpty.Resize(cols, rows)
}

// Kill terminates the child, if it is still running.
func (p *windowsPTY) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// Wait waits for the child. A non-zero exit comes back as *exec.ExitError,
// the shape Session.ExitStatus decodes with errors.As.
func (p *windowsPTY) Wait() error {
	st, err := p.cmd.Process.Wait()
	if err == nil && !st.Success() {
		err = &exec.ExitError{ProcessState: st}
	}
	return err
}

// Close releases the console and the pipes. It does not kill the child (the
// session kills it first when it closes a live session). ConPty.Close is
// idempotent.
func (p *windowsPTY) Close() error { return p.cpty.Close() }

// supportsForegroundProcess reports false: ConPTY has no portable foreground
// process group to read, so the dock keeps the recorded launch/OSC title.
func (p *windowsPTY) supportsForegroundProcess() bool { return false }

// foregroundProcessName is unavailable on Windows.
func (p *windowsPTY) foregroundProcessName() (string, bool) { return "", false }
