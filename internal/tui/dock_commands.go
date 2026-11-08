package tui

import tea "charm.land/bubbletea/v2"

// The palette's terminal commands that dock.go's dockAction runs: focus_terminal,
// restart_terminal, open_terminal, open_terminal_below and open_terminal_right. The dock
// has one place, below the editor, so the open commands show it there and set the status
// notice to what happened. Each of them keeps a shell that runs.

// Status notices of the commands that open the dock.
const (
	shellKeptNotice      = "Shell kept; terminal below."
	terminalOpenedNotice = "Terminal opened below."
	terminalRightNotice  = "Terminal stays below: no right column yet."
	noRoomNotice         = "No room for the terminal."
)

// openTerminal shows the dock below the editor and gives it the focus, as Open Terminal
// and Open Terminal Below do.
func (m *Model) openTerminal() tea.Cmd {
	return m.openDockBelow(false)
}

// openTerminalRight shows the dock below the editor, and says so. A column on the right
// would need the layout to narrow the editor panes and the tab strip, and the layout
// does not place one yet. The dock keeps its position and its running shell.
func (m *Model) openTerminalRight() tea.Cmd {
	return m.openDockBelow(true)
}

// openDockBelow shows the dock below the editor, gives it the focus, and sets the status
// notice to what happened. A shell that runs is kept, because showDock starts one only
// when none runs. right is set for Open Terminal to the Right, which the layout cannot
// place. The notice is the first of these that holds: a shell that cannot start (the dock
// shows that in place of a screen), a frame with no rows for the dock, the right column,
// a kept shell, and otherwise a new shell.
func (m *Model) openDockBelow(right bool) tea.Cmd {
	kept := m.dockRunning()
	cmd := m.showDock()
	switch {
	case m.dock.notice != "":
		m.notice = m.dock.notice
	case !m.layout().DockOn:
		m.notice = noRoomNotice
	case right:
		m.notice = terminalRightNotice
	case kept:
		m.notice = shellKeptNotice
	default:
		m.notice = terminalOpenedNotice
	}
	return cmd
}

// focusTerminal gives the keyboard focus to the dock. A hidden dock is shown, and it
// starts a shell as open_terminal does. A dock whose shell is not running keeps the
// focus where it is, and the status row says why.
func (m *Model) focusTerminal() tea.Cmd {
	if !m.dock.open {
		return m.showDock()
	}
	if m.dockRunning() {
		m.focusDock()
	} else if !m.dock.starting {
		m.notice = noTerminalNotice
	}
	return nil
}

// restartDock closes the shell the dock shows and starts another in the directory the
// shell was started in. The dock is shown, and it takes the focus once the new shell is
// up. A dock that holds no shell, before any start or after a failed one, starts its
// shell in the workspace root. A restart while a start is in flight does nothing, so two
// shells never start at once.
func (m *Model) restartDock() tea.Cmd {
	d := &m.dock
	if d.starting {
		return nil
	}
	dir := m.rootDir()
	if d.term != nil {
		dir = d.dir
	}
	d.open = true
	// The focus leaves the shell that is closing. dockStarted gives it back, because
	// the dock is shown.
	d.focus = false
	return m.launchDock(dir)
}
