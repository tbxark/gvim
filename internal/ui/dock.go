package ui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/term"
)

// The terminal dock shows a shell under the editor body. The shell runs in
// internal/term, and this file keeps the dock's state, its actions and the commands
// that talk to the shell. Update only changes state and returns commands: starts,
// resizes, writes, the clipboard read and the output wait all run in tea.Cmds, and
// their results come back as the dock messages below.

// Notices the dock and the status row show.
const (
	noTerminalNotice    = "No terminal is open."
	noClipboardNotice   = "The clipboard is not available in this build."
	noTerminalsNotice   = "Terminals are not available in this build."
	unsupportedNotice   = "Terminals are not supported on this platform yet."
	startingShellNotice = "Starting a shell..."
	captureOnNotice     = "Keyboard capture ON - all keys go to terminal (F9 to toggle)"
	captureOffNotice    = "Keyboard capture OFF - UI bindings active (F9 to toggle)"
)

// shellSize is a screen size in cells.
type shellSize struct{ cols, rows int }

// dockState is the terminal dock: the shell it shows, its focus and the work in
// flight. The zero value is a hidden dock with no ports.
type dockState struct {
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

// dockStarted is the result of a start. term is nil when err is set.
type dockStarted struct {
	term       Terminal
	err        error
	cols, rows int // the size the shell was started with
}

// dockOutputMsg reports a screen change of the shell. ended is true once the output
// has ended: the shell's Updates channel is closed.
type dockOutputMsg struct {
	gen   int
	ended bool
}

// dockExitMsg reports that the shell has exited.
type dockExitMsg struct{ gen int }

// dockWroteMsg reports the end of a batch of input.
type dockWroteMsg struct {
	gen int
	err error
}

// dockResizedMsg reports the end of a resize to cols by rows.
type dockResizedMsg struct {
	gen        int
	cols, rows int
	err        error
}

// dockClipboardMsg carries the clipboard text that terminal_paste read.
type dockClipboardMsg struct {
	text string
	err  error
}

// dockAction runs the dock's actions. It reports whether action is one of them, and
// the command the action starts.
func (m *Model) dockAction(action string) (tea.Cmd, bool) {
	switch action {
	case "open_terminal_in_dock":
		return m.showDock(), true
	case "open_terminal", "open_terminal_below":
		return m.openTerminal(), true
	case "open_terminal_right":
		return m.openTerminalRight(), true
	case "focus_terminal":
		return m.focusTerminal(), true
	case "restart_terminal":
		return m.restartDock(), true
	case "toggle_utility_dock":
		if m.dock.open {
			m.hideDock()
			return nil, true
		}
		return m.showDock(), true
	case "toggle_dock_focus":
		switch {
		case !m.dock.open:
			return m.showDock(), true
		case m.dock.focus:
			m.dock.focus = false
		default:
			m.focusDock()
		}
		return nil, true
	case "terminal_escape":
		m.dock.focus = false
		return nil, true
	case "toggle_keyboard_capture":
		m.toggleKeyboardCapture()
		return nil, true
	case "terminal_paste":
		return m.pasteClipboard(), true
	case "send_selection_to_terminal":
		return m.sendSelection(), true
	}
	return nil, false
}

// showDock shows the dock, gives it the focus, and starts a shell unless one runs.
func (m *Model) showDock() tea.Cmd {
	m.dock.open = true
	m.focusDock()
	return m.startDock()
}

// hideDock hides the dock and gives up its focus. The shell keeps running.
func (m *Model) hideDock() {
	m.dock.open = false
	m.dock.focus = false
}

// focusDock gives keyboard focus to the dock. The explorer and the tab strip lose it.
// The dock takes no focus while no shell runs, so the focus stays where it is then.
func (m *Model) focusDock() {
	if !m.dockRunning() {
		return
	}
	m.explorerFocus = false
	m.tabFocus = false
	m.dock.focus = true
}

// dockRunning reports whether a shell runs for the dock: one has started and has not
// exited.
func (m *Model) dockRunning() bool {
	return m.dock.term != nil && !m.dock.exited
}

// dockKeyboard reports whether key presses go to the dock: it has the focus, a shell
// runs in it, and it is laid out. A dock the frame has no room for is not drawn, so it
// takes no keys.
func (m *Model) dockKeyboard() bool {
	return m.dock.open && m.dock.focus && m.dockRunning() && m.layout().dockOn
}

// toggleKeyboardCapture turns keyboard capture on or off, with Fresh's notices. It does
// nothing while no shell runs.
func (m *Model) toggleKeyboardCapture() {
	if !m.dockRunning() {
		return
	}
	m.dock.capture = !m.dock.capture
	if m.dock.capture {
		m.notice = captureOnNotice
		return
	}
	m.notice = captureOffNotice
}

// startDock starts a shell unless one is starting or running. A shell that has exited
// is closed and replaced. The start runs in a tea.Cmd.
func (m *Model) startDock() tea.Cmd {
	d := &m.dock
	if d.starting || (d.term != nil && !d.exited) {
		return nil
	}
	return m.launchDock(m.rootDir())
}

// launchDock closes the shell the dock holds, if any, and starts a shell in dir. The
// close and the start run in a tea.Cmd, and the result comes back as a dockStarted.
func (m *Model) launchDock(dir string) tea.Cmd {
	d := &m.dock
	if d.starter == nil {
		d.notice = noTerminalsNotice
		return nil
	}
	old := d.term
	d.term = nil
	d.starting = true
	d.notice = ""
	d.dir = dir
	cols, rows := m.dockContentSize()
	starter := d.starter
	return func() tea.Msg {
		if old != nil {
			_ = old.Close()
		}
		t, err := starter.Start(dir, cols, rows)
		return dockStarted{term: t, err: err, cols: cols, rows: rows}
	}
}

// dockContentSize is the size of the shell's screen in the frame as it is now. It is
// zero when the dock is not laid out.
func (m *Model) dockContentSize() (cols, rows int) {
	l := m.layout()
	if !l.dockOn {
		return 0, 0
	}
	return l.w, l.dockH - 1
}

// dockUpdate applies a dock message. It reports whether msg was one. Messages from an
// earlier shell are ignored, because their generation is not the current one.
func (m *Model) dockUpdate(msg tea.Msg) (tea.Cmd, bool) {
	d := &m.dock
	switch msg := msg.(type) {
	case dockStarted:
		d.starting = false
		if msg.err != nil {
			d.notice = dockStartNotice(msg.err)
			return nil, true
		}
		d.gen++
		d.term = msg.term
		d.live = true
		d.exited = false
		d.notice = ""
		d.applied = shellSize{msg.cols, msg.rows}
		d.armed, d.resizing, d.writing = false, false, false
		d.queue = nil
		if d.open {
			// The dock was shown to get this shell, so the shell takes the focus.
			m.focusDock()
		}
		return nil, true
	case dockOutputMsg:
		if msg.gen == d.gen {
			d.armed = false
			if msg.ended {
				d.live = false
			}
		}
		return nil, true
	case dockExitMsg:
		if msg.gen == d.gen {
			d.armed = false
			d.exited = true
			d.focus = false
			d.queue = nil
		}
		return nil, true
	case dockWroteMsg:
		if msg.gen == d.gen {
			d.writing = false
			if msg.err != nil {
				d.queue = nil
			}
		}
		return nil, true
	case dockResizedMsg:
		if msg.gen == d.gen {
			d.resizing = false
			d.applied = shellSize{msg.cols, msg.rows}
		}
		return nil, true
	case dockClipboardMsg:
		if msg.err != nil {
			m.notice = "Paste failed: " + msg.err.Error()
			return nil, true
		}
		if d.term == nil || d.exited {
			m.notice = noTerminalNotice
			return nil, true
		}
		text := pasteText(msg.text)
		d.queue = append(d.queue, func(t Terminal) error { return t.Paste(text) })
		return nil, true
	}
	return nil, false
}

// dockStartNotice is the notice for a start that failed.
func dockStartNotice(err error) string {
	if errors.Is(err, term.ErrUnsupported) {
		return unsupportedNotice
	}
	return "Could not start a shell: " + err.Error()
}

// dockKey handles a key press while the dock has the focus. The bindings of the
// terminal context run first, except while keyboard capture is on: then only the key
// that turns capture off runs as a binding. A key that none claims goes to the shell as
// the bytes it sends. A key that sends no bytes is dropped, so it never reaches the
// editor while the dock has the focus.
func (m *Model) dockKey(k tea.Key, kp KeyPress) tea.Cmd {
	if !m.dockRunning() {
		return nil
	}
	if m.keys != nil {
		cmd, ok := m.keys.Resolve(kp, KeyContextTerminal)
		if ok && (!m.dock.capture || cmd.Action == "toggle_keyboard_capture") {
			return m.dispatch(cmd)
		}
	}
	b, ok := dockBytes(k, m.dock.term.AppCursorKeys())
	if !ok {
		return nil
	}
	m.dock.queue = append(m.dock.queue, writeBytes(b))
	return nil
}

// writeBytes is the input that writes b to the shell.
func writeBytes(b []byte) func(Terminal) error {
	return func(t Terminal) error {
		_, err := t.Write(b)
		return err
	}
}

// pasteClipboard reads the clipboard in a command. The text reaches the shell when
// the read's message comes back.
func (m *Model) pasteClipboard() tea.Cmd {
	switch {
	case m.dock.term == nil || m.dock.exited:
		m.notice = noTerminalNotice
		return nil
	case m.dock.clipboard == nil:
		m.notice = noClipboardNotice
		return nil
	}
	cb := m.dock.clipboard
	return func() tea.Msg {
		text, err := cb.Read()
		return dockClipboardMsg{text: text, err: err}
	}
}

// sendSelection queues the selection, or the cursor line, for the shell. The text has
// the same line breaks and ESC rule as a paste, and it ends in a line break, so the
// shell runs it. The dock takes the focus, as Fresh's send does.
func (m *Model) sendSelection() tea.Cmd {
	if m.dock.term == nil || m.dock.exited {
		m.notice = noTerminalNotice
		return nil
	}
	doc := m.ed.Active()
	if doc == nil {
		return nil
	}
	text, ok := sentText(doc)
	if !ok {
		m.notice = "Selection too large to send in large file mode"
		return nil
	}
	text = pasteText(text)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	m.dock.queue = append(m.dock.queue, writeBytes([]byte(text)))
	m.dock.open = true
	m.focusDock()
	return nil
}

// closeDock ends the shell. Run calls it when the program ends.
func (m *Model) closeDock() {
	if m.dock.term != nil {
		_ = m.dock.term.Close()
		m.dock.term = nil
	}
}

// dockSync starts the commands the dock needs at this point: the wait for output or the
// exit, a resize when the layout gave the shell a new size, and the next batch of input.
// It runs after every update, so a layout change or a key press is picked up without the
// caller knowing about the dock.
func (m *Model) dockSync() tea.Cmd {
	d := &m.dock
	if d.term == nil {
		return nil
	}
	t, gen := d.term, d.gen
	var cmds []tea.Cmd
	// One wait is in flight at a time. It returns at the next screen change, or at the
	// exit until the exit is seen. The exit is watched apart from the output, so a child
	// that exits while a background job still holds the PTY is seen as exited.
	if !d.armed && (d.live || !d.exited) {
		d.armed = true
		cmds = append(cmds, waitDockEvent(t, gen, d.live, !d.exited))
	}
	if cols, rows := m.dockContentSize(); cols > 0 && !d.resizing && (shellSize{cols, rows}) != d.applied {
		d.resizing = true
		cmds = append(cmds, func() tea.Msg {
			return dockResizedMsg{gen: gen, cols: cols, rows: rows, err: t.Resize(cols, rows)}
		})
	}
	if len(d.queue) > 0 && !d.writing {
		batch := d.queue
		d.queue = nil
		d.writing = true
		cmds = append(cmds, func() tea.Msg {
			var err error
			for _, write := range batch {
				if e := write(t); e != nil && err == nil {
					err = e
				}
			}
			return dockWroteMsg{gen: gen, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// waitDockEvent waits for the next screen change of t when live is set, and for the
// exit of t when watchExit is set. At least one of them is set. A closed Updates channel
// reports the end of the output.
func waitDockEvent(t Terminal, gen int, live, watchExit bool) tea.Cmd {
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
			return dockOutputMsg{gen: gen, ended: !ok}
		case <-done:
			return dockExitMsg{gen: gen}
		}
	}
}

// dockPress gives the dock the focus when a left press lands in its rows. It reports
// whether the press was in the dock, so the caller takes no other action for it.
func (m *Model) dockPress(y int) bool {
	l := m.layout()
	if !l.dockOn || y < l.dockY || y >= l.dockY+l.dockH {
		return false
	}
	m.focusDock()
	return true
}

// selectedText is the text the send sends: the primary selection, or the cursor line
// when nothing is selected.
func selectedText(doc Document) string {
	if anchor, head, ok := doc.Selection(); ok && anchor != head {
		return textBetween(doc, anchor, head)
	}
	line := doc.Cursor().Line
	if line < 0 || line >= doc.LineCount() {
		return ""
	}
	return doc.Line(line)
}
