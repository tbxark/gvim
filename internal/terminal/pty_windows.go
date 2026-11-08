//go:build windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ConPTY: Windows' pseudo console. The child talks to a pseudo console object
// instead of a Unix PTY; the session reads and writes the two anonymous pipes
// the console was created over, and resizes and closes the console itself.
// See https://learn.microsoft.com/windows/console/creating-a-pseudoconsole-session
// and docs/terminal.md for the build/verification story and the real-Windows
// acceptance checklist. There is no pure-Go pipe trick: the console and the
// process creation are Win32 calls through x/sys/windows.

// spawn starts cmd in a new pseudo console of the given size.
func spawn(cmd *exec.Cmd, cols, rows int) (ptyProc, error) {
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, err
	}
	var console windows.Handle
	err = windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)},
		windows.Handle(inR.Fd()), windows.Handle(outW.Fd()), 0, &console)
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		_ = outR.Close()
		_ = outW.Close()
		return nil, fmt.Errorf("term: ConPTY unavailable (Windows 10 version 1809, build 17763, or later is required; Windows Server 2019+): %w", err)
	}
	// The console owns the child-side ends now; the parent keeps the write and
	// read ends of its own.
	_ = inR.Close()
	_ = outW.Close()
	fail := func(e error) (ptyProc, error) {
		windows.ClosePseudoConsole(console)
		_ = inW.Close()
		_ = outR.Close()
		return nil, e
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fail(err)
	}
	defer attrs.Delete()
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(&console), unsafe.Sizeof(console)); err != nil {
		return fail(err)
	}
	si := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:    uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags: windows.STARTF_USESTDHANDLES,
		},
		ProcThreadAttributeList: attrs.List(),
	}
	args := cmd.Args
	if len(args) == 0 {
		args = []string{cmd.Path}
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return fail(err)
	}
	var appName *uint16
	if cmd.Path != "" {
		if appName, err = windows.UTF16PtrFromString(cmd.Path); err != nil {
			return fail(err)
		}
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	envBlock := windowsEnvBlock(env)
	envPtr := &envBlock[0]
	var dirPtr *uint16
	if cmd.Dir != "" {
		if dirPtr, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return fail(err)
		}
	}
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(appName, commandLine, nil, nil, true, flags,
		envPtr, dirPtr, &si.StartupInfo, &pi); err != nil {
		return fail(err)
	}
	return &windowsPTY{
		in: inW, out: outR,
		console: console,
		process: pi.Process, thread: pi.Thread,
	}, nil
}

// windowsEnvBlock encodes env as the UTF-16 environment block CreateProcessW
// wants: each "name=value" as a NUL-terminated string, then one more NUL. A
// var with an empty name is dropped (Windows cannot represent it).
func windowsEnvBlock(env []string) []uint16 {
	var block []uint16
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i <= 0 {
			continue
		}
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			continue
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		// An empty environment is still a block of two NULs.
		block = append(block, 0)
	}
	return block
}

// windowsPTY is a ConPTY console and the child process.
type windowsPTY struct {
	in      *os.File // parent -> pseudo console input
	out     *os.File // pseudo console output -> parent
	console windows.Handle
	process windows.Handle
	thread  windows.Handle
	mu      sync.Mutex
	closed  bool
	waited  bool
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.in.Write(b) }

// Resize changes the console size, which is what the child sees.
func (p *windowsPTY) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Kill terminates the child, if it is still running.
func (p *windowsPTY) Kill() error {
	if p.process == 0 {
		return nil
	}
	return windows.TerminateProcess(p.process, 1)
}

// Wait waits for the child and returns an exit-status error for a non-zero
// code, mirroring os/exec's error.
func (p *windowsPTY) Wait() error {
	_, err := windows.WaitForSingleObject(p.process, windows.INFINITE)
	var code uint32
	if err == nil {
		err = windows.GetExitCodeProcess(p.process, &code)
	}
	p.mu.Lock()
	if !p.waited {
		p.waited = true
		if p.thread != 0 {
			_ = windows.CloseHandle(p.thread)
			p.thread = 0
		}
		if p.process != 0 {
			_ = windows.CloseHandle(p.process)
			p.process = 0
		}
	}
	p.mu.Unlock()
	if err != nil {
		return err
	}
	if code != 0 {
		return &windowsExitError{code: code}
	}
	return nil
}

// Close releases the console and the pipes. It does not kill the child (the
// session kills it first when it closes a live process).
func (p *windowsPTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	// Closing the console flushes its buffers, so close it before the pipes;
	// the session has already killed the child when it closes a live session.
	windows.ClosePseudoConsole(p.console)
	p.console = 0
	err := p.in.Close()
	if err2 := p.out.Close(); err == nil {
		err = err2
	}
	return err
}

// windowsExitError is the non-zero exit status of a ConPTY child.
type windowsExitError struct{ code uint32 }

func (e *windowsExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

// ExitCode returns the process exit code.
func (e *windowsExitError) ExitCode() int { return int(e.code) }
