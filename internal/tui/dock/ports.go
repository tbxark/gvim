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
	tea "charm.land/bubbletea/v2"

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

	// Scrollback returns a bounded copy of the main screen's history, starting at
	// row from, and the total number of rows it holds. ScrollbackLen is that
	// total alone.
	Scrollback(from, count int) (lines []terminal.Line, total int)
	ScrollbackLen() int
	// SearchScrollback looks for query in the history and returns the matching
	// row indexes, oldest first. SearchScrollbackMode is the same search with a
	// regular expression and case option.
	SearchScrollback(query string, limit int) (matches []int, total int)
	SearchScrollbackMode(query string, regex, matchCase bool, limit int) (matches []int, total int, err error)
	// WorkingDirectory is the directory the shell reported with OSC 7, or "".
	WorkingDirectory() string
	// AltScreen reports whether the shell is on the alternate screen.
	AltScreen() bool
	// MouseReporting reports the shell's mouse modes: reporting when it asked
	// for mouse events, drag when it asked for motion with a button held, and
	// allMotion when it asked for buttonless motion too.
	MouseReporting() (reporting, drag, allMotion bool)
	// SendMouse forwards one mouse event to the shell.
	SendMouse(ev terminal.MouseEvent)
}

// TerminalStarter starts a shell in dir with the given size. A zero size is the
// starter's default. On a platform without terminals it returns an error that
// matches terminal.ErrUnsupported with errors.Is, and it returns a nil Terminal.
type TerminalStarter interface {
	Start(dir string, cols, rows int) (Terminal, error)
}

// Launcher is the optional starter half that runs a program other than the
// configured shell: an agent session, or a resume of one. A starter without it
// can still run the plain shell; the dock reports that agents are unavailable.
type Launcher interface {
	StartLaunch(launch terminal.Launch, dir string, cols, rows int) (Terminal, error)
}

// Agent is one terminal.agents entry, as the dock needs it: the name the picker
// shows and the two argv lists a launch and a resume run.
type Agent struct {
	Name       string
	Argv       []string // the new-session command and arguments
	ResumeArgv []string // the argv that rejoins a previous session (may be nil)
}

// ClipboardReader reads the text a paste sends to the shell.
type ClipboardReader interface {
	Read() (string, error)
}

// Values of Effects.MouseForwarding, Fresh's names for when the mouse reaches the
// program inside a terminal.
const (
	MouseForwardRequested = "requested"
	MouseForwardAltScreen = "alt_screen"
)

// Effects are the terminal section settings the dock applies itself, rather than
// through the starter: what happens when the shell exits, when the mouse reaches
// the shell, and how much history it keeps.
type Effects struct {
	// CloseOnExit hides the dock when its shell exits instead of keeping the last
	// screen as read-only scrollback (terminal.close_on_exit).
	CloseOnExit bool
	// MouseForwarding is terminal.mouse_forwarding: "requested" (or "") sends the
	// mouse to the shell only when the shell asked for it; "alt_screen" sends it
	// to every alternate-screen program.
	MouseForwarding string
	// StayInScrollback is terminal.jump_to_end_on_output = false: new output does
	// not pull the reader back to the live screen. A text selection always holds
	// the view, whatever this says.
	StayInScrollback bool
	// NoMouseDragSelects is terminal.mouse_drag_selects = false: a drag on the
	// live grid does not select text. A drag in scrollback, and a Shift+drag that
	// bypasses forwarding, still select.
	NoMouseDragSelects bool
	// PassCtrlVWithoutText is terminal.pass_ctrl_v_without_text (default true): a
	// Ctrl+V whose system clipboard can be read but holds no text goes to the
	// shell as the key itself instead of pasting the last yank. False pastes the
	// last yank instead; an unreadable clipboard pastes the last yank either way.
	PassCtrlVWithoutText bool
	// ResumeAgents is terminal.resume_agents: a restart of an exited agent
	// terminal runs the agent's resume argv instead of its launch argv. Default
	// true, as Fresh's is.
	ResumeAgents bool
	// RestoreLive is terminal.restore_live (a gvim extension, default true, Fresh's
	// behavior): a restored tab that was live when the session was captured revives
	// with a fresh PTY on the user's first dive into it (switching to or focusing
	// the tab), using the recorded argv precedence (agent resume while
	// ResumeAgents is on, else the launch argv, else a plain shell) in the recorded
	// launch directory, above the saved transcript. An exited-at-capture tab (and
	// every tab of an old record without the live flag) never revives; false
	// restores every tab read-only/dead.
	RestoreLive bool
	// Agents are the terminal.agents presets the Run Agent command offers.
	Agents []Agent
}

// Ports are the terminal dock's ports. A nil Starter means the dock cannot start a
// shell, and a nil Clipboard means terminal_paste does nothing.
type Ports struct {
	Starter   TerminalStarter
	Clipboard ClipboardReader
	// Effects are the terminal section settings the dock applies. The zero value
	// keeps the dock after the shell exits and forwards the mouse on request.
	Effects Effects
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
	// Copy puts text on the system clipboard, through the same path the editor's
	// copy uses. It returns the command that does the write. Scrollback mode's y
	// and Ctrl+C use it. Nil leaves the copy without a clipboard.
	Copy func(text string) tea.Cmd
	// Stat reports whether path exists, for the Ctrl+Click links of the terminal
	// output. Nil means os.Stat; tests replace it so they need no files.
	Stat func(path string) bool
	// LastYank is the last text the editor copied or cut, which a Ctrl+V pastes
	// into the terminal when the system clipboard cannot provide text. Nil means
	// there is none.
	LastYank func() string
}

// SetEffects sets the terminal settings the dock applies itself.
func (p *Panel) SetEffects(e Effects) { p.effects = e }

// SetPorts gives the panel its ports. It is safe to call before the panel is used.
func (p *Panel) SetPorts(ports Ports) {
	p.starter, p.clipboard = ports.Starter, ports.Clipboard
	p.launcher = nil
	if l, ok := ports.Starter.(Launcher); ok {
		p.launcher = l
	}
}

// SetHost sets the root services. The root model sets it once at construction.
func (p *Panel) SetHost(host Host) { p.host = host }
