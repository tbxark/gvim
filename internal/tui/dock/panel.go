package dock

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
)

// The notices the dock shows in the status row.
const (
	// NoTerminalNotice is shown when a dock action needs a shell and none runs.
	NoTerminalNotice = "No terminal is open."

	noClipboardNotice   = "The clipboard is not available in this build."
	noTerminalsNotice   = "Terminals are not available in this build."
	unsupportedNotice   = "Terminals are not supported on this platform yet."
	startingShellNotice = "Starting a shell..."
	captureOnNotice     = "Keyboard capture ON - all keys go to terminal (F9 to toggle)"
	captureOffNotice    = "Keyboard capture OFF - UI bindings active (F9 to toggle)"
)

// shellSize is a screen size in cells.
type shellSize struct{ cols, rows int }

// Panel is the terminal dock: the shell it shows, its focus and the work in
// flight. The zero value is a hidden dock with no ports.
type Panel struct {
	host      Host
	starter   TerminalStarter
	clipboard ClipboardReader

	open     bool // the dock is shown
	focus    bool // key presses go to the shell, or to the terminal bindings
	term     Terminal
	dir      string // the directory the last shell was started in; a restart uses it
	gen      int    // counts the shells the dock has had; messages carry it
	starting bool   // a start is in flight
	notice   string // why no shell runs, shown in place of the screen
	exited   bool   // the shell has exited; its last screen stays on show
	capture  bool   // keyboard capture: the shell gets every key while the dock has the focus

	live     bool                   // Updates is open, so screen changes still arrive
	armed    bool                   // a wait for the next output or the exit is in flight
	applied  shellSize              // the size the shell was last given
	resizing bool                   // a resize is in flight
	queue    []func(Terminal) error // input for the shell, in the order it was typed
	writing  bool                   // a batch from queue is in flight
}

// startedMsg is the result of a start. term is nil when err is set.
type startedMsg struct {
	term       Terminal
	err        error
	cols, rows int // the size the shell was started with
}

// outputMsg reports a screen change of the shell. ended is true once the output has
// ended: the shell's Updates channel is closed.
type outputMsg struct {
	gen   int
	ended bool
}

// exitMsg reports that the shell has exited.
type exitMsg struct{ gen int }

// wroteMsg reports the end of a batch of input.
type wroteMsg struct {
	gen int
	err error
}

// resizedMsg reports the end of a resize to cols by rows.
type resizedMsg struct {
	gen        int
	cols, rows int
	err        error
}

// clipboardMsg carries the clipboard text that a paste read.
type clipboardMsg struct {
	text string
	err  error
}

// Open reports whether the dock is shown.
func (p *Panel) Open() bool { return p.open }

// Focus reports whether the dock has the keyboard focus.
func (p *Panel) Focus() bool { return p.focus }

// SetFocus sets the keyboard focus. The root keeps the focus exclusive with the
// explorer and the tab strip.
func (p *Panel) SetFocus(v bool) { p.focus = v }

// Running reports whether a shell runs for the dock: one has started and has not
// exited.
func (p *Panel) Running() bool { return p.term != nil && !p.exited }

// Starting reports whether a start is in flight.
func (p *Panel) Starting() bool { return p.starting }

// Exited reports whether the shell has exited.
func (p *Panel) Exited() bool { return p.exited }

// Notice is why no shell runs, shown in place of the screen, or "".
func (p *Panel) Notice() string { return p.notice }

// Capture reports whether keyboard capture is on: the shell gets every key while
// the dock has the focus.
func (p *Panel) Capture() bool { return p.capture }

// Keyboard reports whether key presses go to the dock: it has the focus, a shell
// runs in it, and it is laid out. A dock the frame has no room for is not drawn, so
// it takes no keys.
func (p *Panel) Keyboard(l frame.Layout) bool {
	return p.open && p.focus && p.Running() && l.DockOn
}

// Show shows the dock and starts a shell unless one runs. It focuses the dock when
// a shell already runs; a shell that starts takes the focus when it is up.
func (p *Panel) Show() tea.Cmd {
	p.open = true
	if p.Running() {
		p.focusDock()
	}
	return p.start()
}

// Hide hides the dock and gives up its focus. The shell keeps running.
func (p *Panel) Hide() {
	p.open = false
	p.focus = false
}

// focusDock gives the dock the focus through the host.
func (p *Panel) focusDock() {
	if !p.Running() {
		return
	}
	if p.host.Focus != nil {
		p.host.Focus()
	}
	p.focus = true
}

// start starts a shell unless one is starting or running. A shell that has exited
// is closed and replaced. The start runs in a tea.Cmd.
func (p *Panel) start() tea.Cmd {
	if p.starting || (p.term != nil && !p.exited) {
		return nil
	}
	dir := ""
	if p.host.RootDir != nil {
		dir = p.host.RootDir()
	}
	return p.launch(dir)
}

// Restart closes the shell the dock shows and starts another in the directory the
// shell was started in. The dock is shown, and it takes the focus once the new shell
// is up. A dock that holds no shell starts its shell in the workspace root. A
// restart while a start is in flight does nothing, so two shells never start at
// once.
func (p *Panel) Restart() tea.Cmd {
	if p.starting {
		return nil
	}
	dir := ""
	if p.host.RootDir != nil {
		dir = p.host.RootDir()
	}
	if p.term != nil {
		dir = p.dir
	}
	p.open = true
	// The focus leaves the shell that is closing. The start gives it back, because
	// the dock is shown.
	p.focus = false
	return p.launch(dir)
}

// launch closes the shell the dock holds, if any, and starts a shell in dir. The
// close and the start run in a tea.Cmd, and the result comes back as a startedMsg.
func (p *Panel) launch(dir string) tea.Cmd {
	if p.starter == nil {
		p.notice = noTerminalsNotice
		return nil
	}
	old := p.term
	p.term = nil
	p.starting = true
	p.notice = ""
	p.dir = dir
	cols, rows := p.contentSize()
	starter := p.starter
	return func() tea.Msg {
		if old != nil {
			_ = old.Close()
		}
		t, err := starter.Start(dir, cols, rows)
		return startedMsg{term: t, err: err, cols: cols, rows: rows}
	}
}

// contentSize is the size of the shell's screen in the frame as it is now. It is
// zero when the dock is not laid out.
func (p *Panel) contentSize() (cols, rows int) {
	if p.host.ContentSize == nil {
		return 0, 0
	}
	return p.host.ContentSize()
}

// Update applies a dock message. It reports whether msg was one. Messages from an
// earlier shell are ignored, because their generation is not the current one.
func (p *Panel) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case startedMsg:
		p.starting = false
		if msg.err != nil {
			p.notice = startNotice(msg.err)
			return nil, true
		}
		p.gen++
		p.term = msg.term
		p.live = true
		p.exited = false
		p.notice = ""
		p.applied = shellSize{msg.cols, msg.rows}
		p.armed, p.resizing, p.writing = false, false, false
		p.queue = nil
		if p.open {
			// The dock was shown to get this shell, so the shell takes the focus.
			p.focusDock()
		}
		return nil, true
	case outputMsg:
		if msg.gen == p.gen {
			p.armed = false
			if msg.ended {
				p.live = false
			}
		}
		return nil, true
	case exitMsg:
		if msg.gen == p.gen {
			p.armed = false
			p.exited = true
			p.focus = false
			p.queue = nil
		}
		return nil, true
	case wroteMsg:
		if msg.gen == p.gen {
			p.writing = false
			if msg.err != nil {
				p.queue = nil
			}
		}
		return nil, true
	case resizedMsg:
		if msg.gen == p.gen {
			p.resizing = false
			p.applied = shellSize{msg.cols, msg.rows}
		}
		return nil, true
	case clipboardMsg:
		if msg.err != nil {
			p.noticeOf("Paste failed: " + msg.err.Error())
			return nil, true
		}
		if p.term == nil || p.exited {
			p.noticeOf(NoTerminalNotice)
			return nil, true
		}
		p.SendPaste(msg.text)
		return nil, true
	}
	return nil, false
}

// noticeOf shows a status-row message through the host.
func (p *Panel) noticeOf(text string) {
	if p.host.Notice != nil {
		p.host.Notice(text)
	}
}

// startNotice is the notice for a start that failed.
func startNotice(err error) string {
	if errors.Is(err, terminal.ErrUnsupported) {
		return unsupportedNotice
	}
	return "Could not start a shell: " + err.Error()
}

// ToggleCapture turns keyboard capture on or off, with the dock's notices. It does
// nothing while no shell runs.
func (p *Panel) ToggleCapture() {
	if !p.Running() {
		return
	}
	p.capture = !p.capture
	if p.capture {
		p.noticeOf(captureOnNotice)
		return
	}
	p.noticeOf(captureOffNotice)
}

// SendKey queues the bytes a key press sends to the shell. A key that sends nothing
// is dropped, so it never reaches the editor while the dock has the focus.
func (p *Panel) SendKey(k tea.Key) {
	if !p.Running() {
		return
	}
	b, ok := dockBytes(k, p.term.AppCursorKeys())
	if !ok {
		return
	}
	p.queue = append(p.queue, writeBytes(b))
}

// writeBytes is the input that writes b to the shell.
func writeBytes(b []byte) func(Terminal) error {
	return func(t Terminal) error {
		_, err := t.Write(b)
		return err
	}
}

// SendPaste queues text for the shell as a paste, with the line breaks and ESC rule
// of a paste.
func (p *Panel) SendPaste(text string) {
	if p.term == nil || p.exited {
		return
	}
	text = pasteText(text)
	p.queue = append(p.queue, func(t Terminal) error { return t.Paste(text) })
}

// SendToShell queues text as if it were typed and ended in Enter, so a shell runs
// it. ok is false, and the status row says why, when no shell runs.
func (p *Panel) SendToShell(text string) bool {
	if p.term == nil || p.exited {
		p.noticeOf(NoTerminalNotice)
		return false
	}
	text = pasteText(text)
	if len(text) == 0 || text[len(text)-1] != '\n' {
		text += "\n"
	}
	p.queue = append(p.queue, writeBytes([]byte(text)))
	p.open = true
	return true
}

// PasteClipboard reads the clipboard in a command. The text reaches the shell when
// the read's message comes back.
func (p *Panel) PasteClipboard() tea.Cmd {
	switch {
	case p.term == nil || p.exited:
		p.noticeOf(NoTerminalNotice)
		return nil
	case p.clipboard == nil:
		p.noticeOf(noClipboardNotice)
		return nil
	}
	cb := p.clipboard
	return func() tea.Msg {
		text, err := cb.Read()
		return clipboardMsg{text: text, err: err}
	}
}

// Close ends the shell. The root calls it when the program ends.
func (p *Panel) Close() {
	if p.term != nil {
		_ = p.term.Close()
		p.term = nil
	}
}

// Sync starts the commands the dock needs at this point: the wait for output or the
// exit, a resize when the layout gave the shell a new size, and the next batch of
// input. The root runs it after every update, so a layout change or a key press is
// picked up without the caller knowing about the dock.
func (p *Panel) Sync() tea.Cmd {
	if p.term == nil {
		return nil
	}
	t, gen := p.term, p.gen
	var cmds []tea.Cmd
	// One wait is in flight at a time. It returns at the next screen change, or at
	// the exit until the exit is seen. The exit is watched apart from the output, so
	// a child that exits while a background job still holds the PTY is seen as
	// exited.
	if !p.armed && (p.live || !p.exited) {
		p.armed = true
		cmds = append(cmds, waitEvent(t, gen, p.live, !p.exited))
	}
	if cols, rows := p.contentSize(); cols > 0 && !p.resizing && (shellSize{cols, rows}) != p.applied {
		p.resizing = true
		cmds = append(cmds, func() tea.Msg {
			return resizedMsg{gen: gen, cols: cols, rows: rows, err: t.Resize(cols, rows)}
		})
	}
	if len(p.queue) > 0 && !p.writing {
		batch := p.queue
		p.queue = nil
		p.writing = true
		cmds = append(cmds, func() tea.Msg {
			var err error
			for _, write := range batch {
				if e := write(t); e != nil && err == nil {
					err = e
				}
			}
			return wroteMsg{gen: gen, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// waitEvent waits for the next screen change of t when live is set, and for the exit
// of t when watchExit is set. At least one of them is set. A closed Updates channel
// reports the end of the output.
func waitEvent(t Terminal, gen int, live, watchExit bool) tea.Cmd {
	return func() tea.Msg {
		var updates <-chan struct{}
		if live {
			updates = t.Updates()
		}
		var done <-chan struct{}
		if watchExit {
			done = t.Done()
		}
		select {
		case _, ok := <-updates:
			return outputMsg{gen: gen, ended: !ok}
		case <-done:
			return exitMsg{gen: gen}
		}
	}
}

// Press gives the dock the focus when a left press lands in its rows. It reports
// whether the press was in the dock, so the caller takes no other action for it.
func (p *Panel) Press(l frame.Layout, y int) bool {
	if !l.DockOn || y < l.DockY || y >= l.DockY+l.DockH {
		return false
	}
	p.focusDock()
	return true
}
