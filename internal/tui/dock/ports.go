// Package dock is the terminal dock of the gvim UI: the shell shown under the
// editor body, its screen and caret, the keys and pastes that go to it, and the
// work in flight around starting, resizing and writing to the shell.
//
// The panel keeps its own state and ports, and gets the few services it needs
// from the root model through Host: the workspace root a shell starts in, the
// status-row notice, the dock's layout size, and the focus handoff. Update only
// changes state and returns commands: starts, resizes, writes, the clipboard read
// and the output wait all run in tea.Cmds, and their results come back as the
// panel's own messages.
//
// The package is a leaf: it imports the terminal session (internal/terminal) and
// the frame/style/grid primitives, never the root model.
package dock

import (
	"github.com/tbxark/gvim/internal/terminal"
)

// Terminal is the shell the dock shows. *terminal.Session satisfies it.
type Terminal interface {
	// Write queues bytes for the shell's input. Paste sends text as a paste, with
	// the bracketed-paste markers when the shell has turned that mode on.
	Write(p []byte) (int, error)
	Paste(text string) error
	// Resize changes the size of the shell's screen.
	Resize(cols, rows int) error
	// Screen is a snapshot of the shell's screen and cursor.
	Screen() terminal.Screen
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
// matches terminal.ErrUnsupported with errors.Is, and it returns a nil Terminal.
type TerminalStarter interface {
	Start(dir string, cols, rows int) (Terminal, error)
}

// ClipboardReader reads the text a paste sends to the shell.
type ClipboardReader interface {
	Read() (string, error)
}

// Ports are the terminal dock's ports. A nil Starter means the dock cannot start a
// shell, and a nil Clipboard means terminal_paste does nothing.
type Ports struct {
	Starter   TerminalStarter
	Clipboard ClipboardReader
}

// Host is what the panel needs from the root model: where a shell starts, how a
// status notice is shown, the screen size the frame gives the dock, and the focus
// handoff that makes the dock the focused part of the frame.
type Host struct {
	// RootDir is the workspace root a shell starts in.
	RootDir func() string
	// Notice shows a message in the status row.
	Notice func(text string)
	// ContentSize is the shell screen size in the frame as it is now, or 0, 0 when
	// the dock is not laid out.
	ContentSize func() (cols, rows int)
	// Focus gives the dock the keyboard focus, taking it from the explorer and the
	// tab strip. It is called only while a shell runs.
	Focus func()
}

// SetPorts gives the panel its ports. It is safe to call before the panel is used.
func (p *Panel) SetPorts(ports Ports) {
	p.starter, p.clipboard = ports.Starter, ports.Clipboard
}

// Clipboard returns the clipboard reader the dock was given, or nil. The root reads
// it for the editor clipboard actions too.
func (p *Panel) Clipboard() ClipboardReader { return p.clipboard }

// SetHost sets the root services. The root model sets it once at construction.
func (p *Panel) SetHost(host Host) { p.host = host }
