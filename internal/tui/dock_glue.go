package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/dock"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// The terminal dock lives in internal/tui/dock. These aliases keep the root's
// exported port surface, which internal/app and internal/uiadapter use.
type (
	Terminal        = dock.Terminal
	TerminalStarter = dock.TerminalStarter
	Tabs            = dock.Tabs
	ClipboardReader = dock.ClipboardReader
	DockPorts       = dock.Ports
	DockEffects     = dock.Effects
	OpenPathMsg     = dock.OpenPathMsg
)

// KeyContextTerminal is the context a key press lands in while the terminal dock
// has the focus. The keymap binds the keys that stay with the editor there.
const KeyContextTerminal KeyContext = "terminal"

// noTerminalNotice is the dock's status text for an action that needs a shell.
func noTerminalNotice() string { return dock.NoTerminalNotice() }

// SetDock gives the terminal dock its ports. The app calls it once, after New.
func (m *Model) SetDock(p DockPorts) {
	m.dock.SetPorts(dock.Ports(p))
	m.dock.SetEffects(dock.Effects(p.Effects))
}

// dockNewPanel returns the dock's tab container with the root services it needs.
func dockNewPanel(m *Model) dock.Tabs {
	var p dock.Tabs
	p.SetHost(dock.Host{
		RootDir:     func() string { return m.rootDir() },
		Notice:      func(text string) { m.notice = text },
		ContentSize: func() (cols, rows int) { return m.dockContentSize() },
		Focus:       func() { m.focusDock() },
		Copy:        func(text string) tea.Cmd { return m.copyToClipboard(text) },
		LastYank:    func() string { return m.lastYank },
	})
	return p
}

// terminalTabRenameTag is the prompt tag of the dock's Rename Terminal Tab. The
// answer names the active tab; an empty answer restores its default name.
type terminalTabRenameTag struct{}

// renameDockTabPrompt asks for the active tab's name, prefilled with the current
// one.
func (m *Model) renameDockTabPrompt() {
	if m.dock.Count() == 0 {
		m.notice = noTerminalNotice()
		return
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:     uiText("gvim.prompt.rename_terminal_tab", "Rename terminal tab:"),
		Input:     m.dock.ActiveName(),
		SelectAll: true,
		Tag:       terminalTabRenameTag{},
	}))
}

// renameDockTab applies the prompt's answer.
func (m *Model) renameDockTab(name string) { m.dock.RenameActive(name) }

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
		if m.dock.Open() {
			m.hideDock()
			return nil, true
		}
		return m.showDock(), true
	case "toggle_dock_focus":
		switch {
		case !m.dock.Open():
			return m.showDock(), true
		case m.dock.Focus():
			m.dock.SetFocus(false)
		default:
			m.focusDock()
		}
		return nil, true
	case "terminal_escape":
		m.dock.SetFocus(false)
		return nil, true
	case "toggle_keyboard_capture":
		m.dock.ToggleCapture()
		return nil, true
	case "terminal_paste":
		return m.dock.PasteClipboard(), true
	case "run_agent":
		return m.openAgentPicker(), true
	case "discover_agent_sessions":
		return m.openAgentDiscover(), true
	case "terminal_tab_new":
		return m.dock.NewTab(), true
	case "terminal_tab_close":
		m.dock.CloseTab()
		return nil, true
	case "terminal_tab_next":
		m.dock.Next()
		return nil, true
	case "terminal_tab_previous":
		m.dock.Previous()
		return nil, true
	case "terminal_tab_rename":
		m.renameDockTabPrompt()
		return nil, true
	case "terminal_tab_move_left":
		m.dock.MoveActive(-1)
		return nil, true
	case "terminal_tab_move_right":
		m.dock.MoveActive(1)
		return nil, true
	case "send_selection_to_terminal":
		return m.sendSelection(), true
	}
	return nil, false
}

// showDock shows the dock, gives it the focus, and starts a shell unless one runs.
func (m *Model) showDock() tea.Cmd { return m.dock.Show() }

// hideDock hides the dock and gives up its focus. The shell keeps running.
func (m *Model) hideDock() { m.dock.Hide() }

// restartDock closes the shell the dock shows and starts another in the directory the
// shell was started in, or in the workspace root when no shell runs. The dock is
// shown, and it takes the focus once the new shell is up.
func (m *Model) restartDock() tea.Cmd { return m.dock.Restart() }

// focusDock gives keyboard focus to the dock. The explorer and the tab strip lose it.
// The dock takes no focus while no shell runs, so the focus stays where it is then.
func (m *Model) focusDock() {
	if !m.dock.Running() && !m.dock.Exited() && !m.dock.Restored() {
		return
	}
	m.explorerFocus = false
	m.tabFocus = false
	m.dock.SetFocus(true)
}

// dockRunning reports whether a shell runs for the dock: one has started and has not
// exited.
func (m *Model) dockRunning() bool { return m.dock.Running() }

// dockKeyboard reports whether key presses go to the dock: it has the focus, a shell
// runs in it, and it is laid out.
func (m *Model) dockKeyboard() bool { return m.dock.Keyboard(m.layout()) }

// dockKey handles a key press while the dock has the focus. The bindings of the
// terminal context run first, except while keyboard capture is on: then only the key
// that turns capture off runs as a binding. A key that none claims goes to the shell as
// the bytes it sends.
func (m *Model) dockKey(k tea.Key, kp KeyPress) tea.Cmd {
	// The dock's quick-switch keys work whether or not the shell has keyboard
	// capture, but not while the pager owns the keys.
	if !m.dock.Scrolling() && m.dock.TabKey(k) {
		return nil
	}
	// The scrollback view has its own keys, and Ctrl+Space is the switch into it
	// from the live screen. Keyboard capture keeps every key for the shell, so it
	// also keeps the switch; the keys it does not take are handled below, by the
	// terminal context bindings or the shell.
	if m.dock.Scrolling() || !m.dock.Capture() {
		if cmd, ok := m.dock.ScrollKey(k); ok {
			return cmd
		}
	}
	if !m.dockRunning() {
		return nil
	}
	// The dock's own keymap context resolves its UI keys (Fresh's dock context) before the
	// terminal context and before the key goes to the shell.
	if m.keys != nil && !m.dock.Capture() {
		if cmd, ok := m.keys.Resolve(kp, KeyContextDock); ok {
			return m.dispatch(cmd)
		}
	}
	if m.keys != nil {
		cmd, ok := m.keys.Resolve(kp, KeyContextTerminal)
		if ok && (!m.dock.Capture() || cmd.Action == "toggle_keyboard_capture") {
			return m.dispatch(cmd)
		}
	}
	m.dock.SendKey(k)
	return nil
}

// sendSelection queues the selection, or the cursor line, for the shell. The text has
// the same line breaks and ESC rule as a paste, and it ends in a line break, so the
// shell runs it. The dock takes the focus, as Fresh's send does.
func (m *Model) sendSelection() tea.Cmd {
	if !m.dockRunning() {
		m.notice = noTerminalNotice()
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
	m.dock.SendToShell(text)
	m.focusDock()
	return nil
}

// closeDock ends the shell. Run calls it when the program ends.
func (m *Model) closeDock() { m.dock.Close() }

// dockSync starts the commands the dock needs at this point: the wait for output or
// the exit, a resize when the layout gave the shell a new size, and the next batch of
// input. It runs after every update.
func (m *Model) dockSync() tea.Cmd { return m.dock.Sync() }

// dockMouse gives a mouse message to the dock when it lands in the dock's rows.
// It reports whether the rows are the dock's, so the root takes no other action
// for the message: the dock decides alone whether it forwards it, opens a link
// with it, or only focuses.
func (m *Model) dockMouse(msg tea.Msg, y, count int) (tea.Cmd, bool) {
	l := m.layout()
	inRows := l.DockOn && y >= l.DockY && y < l.DockY+l.DockH
	if !inRows {
		// A drag that left the dock keeps the dock's mouse handling until the
		// release, so the selection can auto-scroll past the edges. Anything else
		// outside the rows ends the link hover.
		if !m.dock.SelectionDragging() {
			m.dock.ClearHover()
			return nil, false
		}
	}
	cmd, _ := m.dock.Mouse(msg, l, count)
	return cmd, true
}

// TerminalState is the terminal state the session file keeps, or the zero state
// when the dock has no shell and no transcript.
func (m *Model) TerminalState() terminal.SavedState { return m.dock.SavedState() }

// RestoreTerminal shows a terminal restored from the session file: a read-only
// transcript, dead until the user restarts it.
func (m *Model) RestoreTerminal(st terminal.SavedState) {
	if st.Version != terminal.SavedStateVersion {
		return
	}
	m.dock.Restore(st)
}

// copyToClipboard is the dock's clipboard writer, through the same path the
// editor's copy uses: the terminal clipboard command and, when the app has one,
// the native clipboard writer. It is what scrollback's y and Ctrl+C call.
func (m *Model) copyToClipboard(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	m.rememberYank(text)
	cmd := tea.SetClipboard(text)
	if native := m.nativeWrite(text); native != nil {
		return tea.Batch(cmd, native)
	}
	return cmd
}

// dockContentSize is the size of the shell's screen in the frame as it is now. It is
// zero when the dock is not laid out.
func (m *Model) dockContentSize() (cols, rows int) {
	l := m.layout()
	if !l.DockOn {
		return 0, 0
	}
	return l.W, l.DockH - 1
}
