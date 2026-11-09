package terminal

// ptyProc is a platform pseudo-terminal and the child process running behind it.
// Unix implements it over creack/pty and os/exec; Windows over ConPTY (see
// pty_windows.go). The Session uses it for every platform operation, so the
// emulator, the inbox and the dock stay platform-independent.
type ptyProc interface {
	// Read reads the child's output.
	Read(p []byte) (int, error)
	// Write writes the child's input.
	Write(p []byte) (int, error)
	// Resize changes the terminal size the child sees.
	Resize(cols, rows int) error
	// Kill terminates the child if it is still running.
	Kill() error
	// Wait blocks until the child exits and reports its exit status.
	Wait() error
	// Close releases the terminal handles. It does not kill the child.
	Close() error
	// supportsForegroundProcess reports whether this transport can name the
	// foreground process group's program. Only a local Unix PTY can; a Windows
	// ConPTY or a remote backend cannot.
	supportsForegroundProcess() bool
	// foregroundProcessName reads the foreground process group's program name. It
	// performs bounded OS introspection (tcgetpgrp + /proc or a bounded ps), never
	// touches the dock's Update loop and returns false when unavailable.
	foregroundProcessName() (string, bool)
}
