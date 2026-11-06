package ui

import "github.com/tbxark/gvim/internal/term"

// The ports in this file are the terminal dock's view of the outside world: the
// shell it shows and the clipboard a paste reads. The app adapters implement them.
// The dock calls them only from a tea.Cmd, so Update never starts a process, reads
// the clipboard or writes to the PTY.

// KeyContextTerminal is the context a key press lands in while the terminal dock
// has the focus. The keymap binds the keys that stay with the editor there. A key
// that no binding claims goes to the shell.
const KeyContextTerminal KeyContext = "terminal"

// Terminal is the shell the dock shows. *term.Session satisfies it.
type Terminal interface {
	// Write queues bytes for the shell's input. Paste sends text as a paste, with
	// the bracketed-paste markers when the shell has turned that mode on.
	Write(p []byte) (int, error)
	Paste(text string) error
	// Resize changes the size of the shell's screen.
	Resize(cols, rows int) error
	// Screen is a snapshot of the shell's screen and cursor.
	Screen() term.Screen
	// AppCursorKeys reports whether the shell has turned on application cursor keys.
	AppCursorKeys() bool
	// Updates receives after each screen change. It is closed when the output ends.
	Updates() <-chan struct{}
	// Done is closed once the shell has exited.
	Done() <-chan struct{}
	// Close ends the shell and releases its PTY.
	Close() error
}

// TerminalStarter starts a shell in dir with the given size. A zero size is the
// starter's default. On a platform without terminals it returns an error that
// matches term.ErrUnsupported with errors.Is, and it returns a nil Terminal.
type TerminalStarter interface {
	Start(dir string, cols, rows int) (Terminal, error)
}

// ClipboardReader reads the text a paste sends to the shell.
type ClipboardReader interface {
	Read() (string, error)
}

// DockPorts are the terminal dock's ports. A nil Starter means the dock cannot
// start a shell, and a nil Clipboard means terminal_paste does nothing.
type DockPorts struct {
	Starter   TerminalStarter
	Clipboard ClipboardReader
}

// SetDock gives the terminal dock its ports. The app calls it once, after New.
func (m *Model) SetDock(p DockPorts) {
	m.dock.starter = p.Starter
	m.dock.clipboard = p.Clipboard
}
