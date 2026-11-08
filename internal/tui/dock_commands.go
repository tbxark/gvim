package tui

import tea "charm.land/bubbletea/v2"

// The palette's terminal commands that dock_glue.go's dockAction runs: focus_terminal,
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
	kept := m.dock.Running()
	cmd := m.showDock()
	switch {
	case m.dock.Notice() != "":
		m.notice = m.dock.Notice()
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
	if !m.dock.Open() {
		return m.showDock()
	}
	// A restored dock can take the focus too: it is a read-only transcript, and
	// focusing it is the user's dive that may revive a tab that was live when the
	// session was saved (terminal.restore_live).
	if m.dock.Running() || m.dock.Restored() {
		m.focusDock()
	} else if !m.dock.Starting() {
		m.notice = noTerminalNotice()
	}
	return nil
}
