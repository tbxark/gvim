package dock

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
)

// The notices the dock shows in the status row.
// The dock's notices, translated at display time: the Settings locale can change while
// the editor runs, so they are functions rather than constants.

// NoTerminalNotice() is shown when a dock action needs a shell and none runs.
func NoTerminalNotice() string { return i18n.T("", "terminal.no_terminal_open", nil) }

func noClipboardNotice() string   { return i18n.T("", "gvim.terminal.no_clipboard", nil) }
func noTerminalsNotice() string   { return i18n.T("", "gvim.terminal.no_terminals", nil) }
func unsupportedNotice() string   { return i18n.T("", "gvim.terminal.unsupported", nil) }
func startingShellNotice() string { return i18n.T("", "gvim.terminal.starting", nil) }
func captureOnNotice() string     { return i18n.T("", "gvim.terminal.capture_on", nil) }
func captureOffNotice() string    { return i18n.T("", "gvim.terminal.capture_off", nil) }

// shellSize is a screen size in cells.
type shellSize struct{ cols, rows int }

// Panel is the terminal dock: the shell it shows, its focus and the work in
// flight. The zero value is a hidden dock with no ports.
type Panel struct {
	host      Host
	starter   TerminalStarter
	launcher  Launcher // optional: starts agent/resume programs; nil for a shell-only starter
	clipboard ClipboardReader

	open  bool // the dock is shown
	focus bool // key presses go to the shell, or to the terminal bindings
	term  Terminal
	dir   string // the terminal's current directory (the shell's last report); a plain restart uses it
	// launchDir is the directory the launched program was started in. Cwd may move
	// (the shell reports its own directory), but an agent restart/resume uses the
	// chosen launch directory, so the two are stored apart.
	launchDir string
	gen       int    // counts the shells the dock has had; messages carry it
	starting  bool   // a start is in flight
	notice    string // why no shell runs, shown in place of the screen
	exited    bool   // the shell has exited; its last screen stays on show
	// exitCode/exitKnown describe how the shell ended (captured when its exit was
	// reaped); exitMarker records that the exit marker line still has to be written
	// into the transcript (saved or captured for a restart).
	exitCode   int
	exitKnown  bool
	exitMarker bool
	capture    bool // keyboard capture: the shell gets every key while the dock has the focus

	// effects are the terminal section settings the dock applies itself, and
	// scroll is the read-only scrollback view. pressed is the button a forwarded
	// drag is holding, or terminal.MouseNone. launch is the program the dock's
	// current terminal started (empty for the plain shell): it is the record a
	// restart and the saved session use to rejoin an agent session.
	effects Effects
	launch  terminal.Launch
	scroll  scrollState
	pressed int
	// sel is the text selection over the scrollback view, prior is the transcript
	// a restarted shell keeps above its own history (see selection.go and
	// transcript.go), and hover is the Ctrl+hover link highlight (see hover.go).
	sel   selState
	prior []terminal.Line
	// priorWidth is the pane width the prior transcript was captured at (or the
	// saved width of a restored transcript); a content-width change reflows the
	// rows to the new width (transcript.go reflowPrior).
	priorWidth int
	// termFocusSet/termFocus remember the last focus state reported to the current
	// session, so combined window/dock focus changes never send a duplicate.
	termFocusSet bool
	termFocus    bool
	// history is the dock-wide search-query history shared by every tab (Fresh keeps
	// its prompt history per window). Tabs assigns one store to each panel; a panel
	// built without Tabs (tests, an extracted/standalone panel) lazily uses its own.
	history    *searchHistoryStore
	ownHistory searchHistoryStore
	// fgName is the cached foreground process name of a local shell, refreshed by
	// the bounded poll chain in foreground.go. Empty for a remote/unsupported
	// terminal, a restored tab or an exited shell (the label then falls back to the
	// launch record and then the OSC title).
	fgName string
	// hostBlurred is the outer terminal window's focus state (true when the OS
	// window is not focused). The child gets focus-in only when the dock has the
	// keyboard focus AND the window is focused; zero value = focused, matching a
	// program started without a focus report.
	hostBlurred bool
	// autoExitScroll asks the next Sync/Show to open the read-only scrollback view
	// when the shell has exited, so the frozen transcript (ending in Fresh's exit
	// marker) is what the dock shows, like Fresh's read-only exited terminal. It is
	// consumed on the first successful enter and cleared when the user deliberately
	// returns to the frozen live screen (Ctrl+Space) or the dock restarts.
	autoExitScroll bool
	// titleText is the terminal title recorded for this tab (OSC 0/2): the live
	// session's current title while a shell runs, or the restored one while dead.
	// It is a tab-label fallback behind an explicit rename and the launch record.
	titleText string
	// savedCursor/savedAlt/savedVisibleRows describe a restored styled snapshot:
	// the saved cursor, alternate-screen flag and how many trailing prior rows were
	// the visible screen. They let a restored-but-not-revived tab re-save the full
	// snapshot and let the read-only view paint the saved cursor. A reflow clears
	// them (the row mapping no longer holds).
	savedCursor      terminal.SavedCursor
	savedAlt         bool
	savedVisibleRows int
	// wasLive is a restored tab's saved liveness: it was running when the session
	// was captured and may be revived on the user's first dive into the tab. It is
	// cleared when the revive is attempted, so a failed start is not retried by
	// another dive (an explicit Restart still works).
	wasLive  bool
	reflowed bool // a restored transcript was re-wrapped to a different width
	hover    hoverState
	dragSeq  int // invalidates the auto-scroll ticks of older drags
	// restored is set when the dock shows a transcript restored from the session
	// file, with no shell behind it (see transcript.go).
	restored bool

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

// exitMsg reports that the shell has exited, with its exit status when the
// terminal can report one (code is -1 for a signal on Unix).
type exitMsg struct {
	gen    int
	code   int
	codeOK bool
}

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
// explorer and the tab strip. Losing the focus leaves the scrollback view, so the
// dock is live again when it is next focused.
func (p *Panel) SetFocus(v bool) {
	was := p.focus
	p.focus = v
	if was != v {
		p.reportTerminalFocus(v && !p.hostBlurred)
	}
	if !v {
		p.ClearHover()
		// Losing the keyboard focus closes an open search input cleanly: its pending
		// preview answers are invalidated, the view is restored and no hidden input
		// keeps swallowing keys (a restored transcript keeps its read-only view, it
		// just no longer has a search open).
		if p.scroll.typing {
			_ = p.cancelSearchInput()
		}
		// A restored transcript is the whole dock until it is restarted, so it
		// keeps its read-only view when the focus leaves.
		if p.scroll.on && !p.restored && !p.exited {
			// A live shell leaves scrollback when the focus goes; an exited shell's
			// read-only transcript (with its marker) stays, like Fresh's read-only
			// exited terminal.
			p.scroll = scrollState{}
		}
		p.clearSelection()
	}
}

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
	if !p.open || !p.focus || !l.DockOn || (p.term == nil && !p.restored) {
		return false
	}
	// A shell that exited keeps the dock for its scrollback, and that view takes
	// the keys; a live shell always does.
	return true
}

// Show shows the dock and starts a shell unless one runs. It focuses the dock when
// a shell already runs; a shell that starts takes the focus when it is up.
func (p *Panel) Show() tea.Cmd {
	p.open = true
	if p.Running() {
		p.focusDock()
	}
	// An exited shell shows its read-only transcript (ending in the marker), and a
	// restored tab stays read-only until an explicit dive/restart: opening the dock
	// must not spawn a shell behind either of them.
	if p.exited || p.restored {
		return p.maybeShowExitScrollback()
	}
	if auto := p.maybeShowExitScrollback(); auto != nil {
		return auto
	}
	return p.start()
}

// Hide hides the dock and gives up its focus. The shell keeps running.
func (p *Panel) Hide() {
	p.open = false
	if p.focus {
		p.reportTerminalFocus(false)
	}
	p.focus = false
	if p.scroll.typing {
		_ = p.cancelSearchInput()
	}
	p.scroll = scrollState{}
	p.clearSelection()
	// A hidden dock that shows an exited shell opens its transcript again when it
	// is next shown, so the marker is not lost by hiding the dock.
	p.autoExitScroll = p.exited && p.exitMarker && p.term != nil
}

// focusDock gives the dock the focus through the host.
func (p *Panel) focusDock() {
	if p.term == nil && !p.restored {
		return
	}
	if p.host.Focus != nil {
		p.host.Focus()
	}
	if !p.focus {
		p.focus = true
		p.reportTerminalFocus(!p.hostBlurred)
	}
}

// SetHostFocused merges the outer terminal window's focus into the child's focus
// report: a terminal program is focused only while both the OS window and the dock
// hold the keyboard. It reports only on a real change (a window focus event with
// the dock on a file buffer changes nothing for the terminal).
func (p *Panel) SetHostFocused(focused bool) {
	blurred := !focused
	if p.hostBlurred == blurred {
		return
	}
	p.hostBlurred = blurred
	p.reportTerminalFocus(p.focus && focused)
}

// terminalFocuser is the optional focus-reporting half of a terminal session
// (terminal.Session.Focus). A fake or a transcript without a session simply does
// not implement it, and the dock then sends nothing.
type terminalFocuser interface{ Focus(bool) }

// reportTerminalFocus forwards this panel's keyboard focus to its child when the
// child asked for focus reporting (DECSET 1004). It is called only on real focus
// transitions, so the child receives one CSI I on focus and one CSI O on blur; a
// child that enables 1004 later gets an initial focus-in from the session when the
// dock is focused. A dead/restored panel has no session and reports nothing.
func (p *Panel) reportTerminalFocus(focused bool) {
	if p.term == nil {
		return
	}
	if p.termFocusSet && p.termFocus == focused {
		return
	}
	f, ok := p.term.(terminalFocuser)
	if !ok {
		return
	}
	p.termFocus, p.termFocusSet = focused, true
	f.Focus(focused)
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
	p.launch = terminal.Launch{}
	return p.launchShell(dir)
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
		// Keep the old transcript above the new shell, and restart in the
		// directory the shell reported last (Fresh's respawn cwd).
		p.capturePrior()
		if cwd := p.term.WorkingDirectory(); cwd != "" {
			dir = clipSavedDir(cleanSavedText(cwd))
		} else if p.dir != "" {
			dir = p.dir
		}
	} else if p.dir != "" {
		// A restored tab uses its persisted last cwd; launch_dir is the fallback
		// for an older record (migration keeps the old behavior).
		dir = p.dir
	}
	if dir == "" {
		dir = p.launchDir
	}
	p.open = true
	// The focus leaves the shell that is closing. The start gives it back, because
	// the dock is shown.
	p.focus = false
	return p.relaunch(dir)
}

// relaunch starts the recorded program for a user-initiated restart, with Fresh's
// precedence: the agent's resume argv when terminal.resume_agents is on and the
// record has one, then the recorded launch argv, then the default shell. The
// record is kept across the restart, so a later restart can resume again.
func (p *Panel) relaunch(dir string) tea.Cmd {
	record := p.launch.Clean()
	launch := record
	if len(record.ResumeArgv) > 0 && p.effects.ResumeAgents {
		launch.Argv = record.ResumeArgv
	}
	if launch.Empty() {
		p.launch = terminal.Launch{}
		return p.launchShell(dir)
	}
	return p.launchProgram(dir, launch.Argv, record, launch.Ephemeral)
}

// StartAgent starts a configured agent preset in the dock. It reports whether the
// name is a configured agent; a miss is a status notice. The session terminal is
// ephemeral and records the launch and resume argv, so Restart can rejoin it.
func (p *Panel) StartAgent(name string) (tea.Cmd, bool) {
	for _, agent := range p.effects.Agents {
		if agent.Name != name {
			continue
		}
		dir := ""
		if p.host.RootDir != nil {
			dir = p.host.RootDir()
		}
		record := terminal.Launch{Argv: agent.Argv, Agent: agent.Name, ResumeArgv: agent.ResumeArgv, Ephemeral: true}
		p.open = true
		p.focus = false
		return p.launchProgram(dir, record.Argv, record, true), true
	}
	p.notice = dockText("gvim.dock.no_agent", "No terminal agent is configured under that name.")
	return nil, false
}

// Agents returns the configured agent presets, in config order.
func (p *Panel) Agents() []Agent { return p.effects.Agents }

// Launch returns the program the tab's terminal was started with, empty for the
// plain shell. It is read-only and bounded the way a start or save bounds it; the
// record is what a restart replays and what the tab strip titles a terminal with
// (Fresh names a tab after the program it runs).
func (p *Panel) Launch() terminal.Launch { return p.launch.Clean() }

// TitleText returns the terminal title of this panel: the live shell's OSC 0/2
// title while it runs, else the title recorded with the restored tab. It is the
// tab-label fallback behind an explicit rename and the launch record. The title
// method is optional on the terminal port, so a test fake without it reports "".
func (p *Panel) TitleText() string {
	if p.term != nil {
		if titler, ok := p.term.(interface{ Title() string }); ok {
			return terminal.CleanTitle(titler.Title())
		}
		return ""
	}
	return terminal.CleanTitle(p.titleText)
}

// launchProgram closes the shell the dock holds and starts argv, recording rec as
// the launch a restart should use. The record is set before the start, so a failed
// start can be retried and is not lost.
func (p *Panel) launchProgram(dir string, argv []string, rec terminal.Launch, ephemeral bool) tea.Cmd {
	if p.launcher == nil || len(argv) == 0 {
		p.notice = noTerminalsNotice()
		return nil
	}
	if p.starting {
		return nil
	}
	old := p.term
	p.term = nil
	p.starting = true
	p.notice = ""
	// A new shell has its own exit to come: forget the old status/marker (the
	// marker itself is already in the prior transcript when a restart captured it).
	p.exitCode, p.exitKnown, p.exitMarker = 0, false, false
	p.autoExitScroll = false
	p.dir = dir
	if p.launchDir == "" {
		// Keep the recorded program's chosen launch directory as provenance/fallback
		// across restarts; the cwd (p.dir / the saved Cwd) is what a restart uses.
		p.launchDir = dir
	}
	p.scroll = scrollState{}
	p.restored = false
	p.exited = false
	rec = rec.Clean()
	rec.Argv = append([]string(nil), argv...)
	rec.Ephemeral = ephemeral
	p.launch = rec
	cols, rows := p.contentSize()
	launcher := p.launcher
	launch := rec
	return func() tea.Msg {
		if old != nil {
			_ = old.Close()
		}
		t, err := launcher.StartLaunch(launch, dir, cols, rows)
		return startedMsg{term: t, err: err, cols: cols, rows: rows}
	}
}

// launchShell closes the shell the dock holds, if any, and starts the plain
// configured shell in dir. The close and the start run in a tea.Cmd, and the
// result comes back as a startedMsg.
func (p *Panel) launchShell(dir string) tea.Cmd {
	if p.starter == nil {
		p.notice = noTerminalsNotice()
		return nil
	}
	old := p.term
	p.term = nil
	p.starting = true
	p.notice = ""
	// A new shell has its own exit to come: forget the old status/marker (the
	// marker itself is already in the prior transcript when a restart captured it).
	p.exitCode, p.exitKnown, p.exitMarker = 0, false, false
	p.autoExitScroll = false
	p.dir = dir
	if p.launchDir == "" {
		// Keep the recorded program's chosen launch directory as provenance/fallback
		// across restarts; the cwd (p.dir / the saved Cwd) is what a restart uses.
		p.launchDir = dir
	}
	p.scroll = scrollState{}
	p.restored = false
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

// dive revives a restored tab that was live when the session was captured, the
// first time the user switches to or focuses the tab after a restore (Fresh's
// dive). It starts a fresh PTY with the same argv precedence an explicit Restart
// uses — the recorded agent-resume argv when terminal.resume_agents is on, else
// the launch argv, else the plain shell — in the recorded launch directory, and
// keeps the saved transcript above the new shell. A tab that was exited at the
// capture (Live false, the old-record default) and a record that terminal.restore_live
// turned off never revive. The dive is attempted once: wasLive is cleared before
// the start, so a failed start is not retried by another dive (an explicit Restart
// still works). It returns nil when there is nothing to revive.
func (p *Panel) dive() tea.Cmd {
	if !p.restored || !p.wasLive || !p.effects.RestoreLive || p.term != nil || p.starting {
		return nil
	}
	p.wasLive = false
	p.restored = false
	p.exited = false
	p.open = true
	// Fresh restores/respawns in the persisted last reported directory; gvim's
	// recorded cwd takes precedence and launch_dir is only the fallback for an
	// older record that never reported one.
	dir := p.dir
	if dir == "" {
		dir = p.launchDir
	}
	return p.relaunch(dir)
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
		// The replacement session starts with the dock's current combined focus
		// state (a focus call may have happened before this session existed).
		p.termFocusSet = false
		p.reportTerminalFocus(p.focus && !p.hostBlurred)
		if p.open {
			// The dock was shown to get this shell, so the shell takes the focus.
			p.focusDock()
		}
		// Start the bounded foreground-process poll chain (a command; Update does no
		// IO). A remote or Windows transport reports unsupported and returns nil.
		if p.foregroundPollSupported() {
			return p.foregroundPollCmd(), true
		}
		return nil, true
	case foregroundPollMsg:
		return p.applyForegroundPoll(msg), true
	case foregroundPollTickMsg:
		return p.applyForegroundTick(msg), true
	case dragScrollMsg:
		return p.dragScrollTick(msg), true
	case outputMsg:
		if msg.gen == p.gen {
			p.armed = false
			if msg.ended {
				p.live = false
			}
			// The alternate screen has no history: a full-screen program that
			// starts while the reader is in scrollback hides the view.
			if p.scroll.on && p.altScreen() {
				p.scroll = scrollState{}
				p.clearSelection()
			}
			// terminal.jump_to_end_on_output: new output returns to the live
			// screen; a text selection holds the view instead.
			if p.scroll.on && !p.sel.active && !p.effects.StayInScrollback {
				p.scroll = scrollState{}
				p.clearSelection()
			}
		}
		return nil, true
	case exitMsg:
		if msg.gen == p.gen {
			p.armed = false
			p.exited = true
			p.exitCode, p.exitKnown = msg.code, msg.codeOK
			p.exitMarker = true
			p.termFocusSet = false
			p.fgName = ""
			p.focus = false
			p.queue = nil
			p.scroll = scrollState{}
			p.clearSelection()
			// Fresh turns an exited terminal into its read-only scrollback; gvim keeps
			// the frozen screen as the live view but shows the transcript (whose last
			// row is the exit marker) automatically, so the marker is visible without
			// saving or pressing Ctrl+Space. A dock without layout rows waits for Sync.
			p.autoExitScroll = !p.effects.CloseOnExit
			if p.effects.CloseOnExit {
				p.autoExitScroll = false
				// terminal.close_on_exit: the dock goes away with its shell
				// instead of keeping the last screen as scrollback. The release
				// is IO, so it runs in a command, not in Update.
				p.open = false
				p.live = false
				if old := p.term; old != nil {
					p.term = nil
					return func() tea.Msg {
						_ = old.Close()
						return nil
					}, true
				}
			}
		}
		return p.maybeShowExitScrollback(), true
	case scrollbackMsg:
		if p.applyScrollback(msg) {
			return p.requestScrollback(), true
		}
		return nil, true
	case searchMsg:
		return p.applySearch(msg), true
	case selectionCopyMsg:
		if msg.gen != p.gen {
			return nil, true
		}
		if msg.text == "" {
			p.noticeOf(nothingToCopyNotice())
			return nil, true
		}
		p.noticeOf(copiedSelectionNotice())
		if p.host.Copy == nil {
			p.noticeOf(noClipboardNotice())
			return nil, true
		}
		return p.host.Copy(msg.text), true
	case linkMissMsg:
		p.noticeOf(linkMissNotice(msg.token))
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
		p.applyClipboard(msg)
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
		return unsupportedNotice()
	}
	return dockTextVars("gvim.dock.shell_start_failed", "Could not start a shell: %{error}", map[string]any{"error": err.Error()})
}

// ToggleCapture turns keyboard capture on or off, with the dock's notices. It does
// nothing while no shell runs.
func (p *Panel) ToggleCapture() {
	if !p.Running() {
		return
	}
	p.capture = !p.capture
	if p.capture {
		p.noticeOf(captureOnNotice())
		return
	}
	p.noticeOf(captureOffNotice())
}

// SendBytes queues raw bytes for the shell's input. It does nothing when no shell
// runs. The alternate-scroll rule uses it for the arrow keys a wheel notch sends.
func (p *Panel) SendBytes(b []byte) {
	if !p.Running() {
		return
	}
	p.queue = append(p.queue, writeBytes(b))
}

// SendKey queues the bytes a key press sends to the shell. A key that sends nothing
// is dropped, so it never reaches the editor while the dock has the focus.
func (p *Panel) SendKey(k tea.Key) {
	if !p.Running() {
		return
	}
	flags := 0
	if kt, ok := p.term.(interface{ KittyKeyboardFlags() uint8 }); ok {
		flags = int(kt.KittyKeyboardFlags())
	}
	b, ok := dockBytesFlags(k, p.term.AppCursorKeys(), flags)
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
		p.noticeOf(NoTerminalNotice())
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

// applyClipboard sends a Ctrl+V to the shell following
// terminal.pass_ctrl_v_without_text, Fresh's rule:
//   - a read that holds text pastes it;
//   - a read that holds no text passes the key itself to the shell when
//     pass_ctrl_v_without_text is on (an agent CLI reads the clipboard image
//     itself), and pastes the last yank when it is off;
//   - a clipboard that cannot be read pastes the last yank, and passes the key
//     only when there is no yank and the setting is on.
func (p *Panel) applyClipboard(msg clipboardMsg) {
	if p.term == nil || p.exited {
		p.noticeOf(NoTerminalNotice())
		return
	}
	if msg.err == nil {
		if msg.text != "" {
			p.SendPaste(msg.text)
			return
		}
		if !p.effects.PassCtrlVWithoutText {
			p.pasteLastYank()
			return
		}
		p.sendCtrlV()
		return
	}
	if p.pasteLastYank() {
		return
	}
	if p.effects.PassCtrlVWithoutText {
		p.sendCtrlV()
	}
}

// pasteLastYank pastes the editor's last copy or cut into the shell. It reports
// whether there was one.
func (p *Panel) pasteLastYank() bool {
	if p.host.LastYank == nil {
		return false
	}
	text := p.host.LastYank()
	if text == "" {
		return false
	}
	p.SendPaste(text)
	return true
}

// sendCtrlV passes a literal Ctrl+V byte to the shell, so the program can read
// the clipboard itself.
func (p *Panel) sendCtrlV() {
	if p.term == nil || p.exited {
		return
	}
	p.queue = append(p.queue, writeBytes([]byte{0x16}))
}

// PasteClipboard reads the clipboard in a command. The text reaches the shell when
// the read's message comes back.
func (p *Panel) PasteClipboard() tea.Cmd {
	switch {
	case p.term == nil || p.exited:
		p.noticeOf(NoTerminalNotice())
		return nil
	case p.clipboard == nil:
		p.noticeOf(noClipboardNotice())
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
	p.scroll = scrollState{}
	p.clearSelection()
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
	if cols, _ := p.contentSize(); cols > 0 {
		p.reflowPrior(cols)
	}
	search := p.takeStaleSearch()
	if p.term == nil {
		// A restored transcript has no session to ask for rows; the window only
		// needs clamping when the layout (and so the body height) changed.
		if p.scroll.on {
			p.clampScroll()
		}
		return search
	}
	p.maybeShowExitScrollback()
	t, gen := p.term, p.gen
	var cmds []tea.Cmd
	if search != nil {
		cmds = append(cmds, search)
	}
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
	if p.scroll.on {
		cmds = append(cmds, p.requestScrollback())
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
			msg := exitMsg{gen: gen}
			if es, ok := t.(interface{ ExitStatus() (int, bool) }); ok {
				msg.code, msg.codeOK = es.ExitStatus()
			}
			return msg
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
