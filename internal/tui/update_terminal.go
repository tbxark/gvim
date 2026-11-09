package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// The configured update command (config update_command) runs in a terminal dock tab,
// like Fresh's updater terminal: the output stays in the dock after the command exits,
// and Show update output (open_update_log) comes back to it. Without a configured
// command the two Fresh rows stay hidden and their actions keep the not-available
// notice, so gvim offers no command that would do nothing.

const (
	// updateTabSource identifies the update tab, so a second run restarts that tab
	// (which keeps its transcript) instead of opening a duplicate.
	updateTabSource = "gvim:update"
	// updateTabName is the dock tab's name.
	updateTabName = "Update"
)

// runUpdateCommand starts the configured update command in the dock, or restarts the
// existing update tab. Everything runs through the dock's terminal ports as a command,
// never on the update goroutine; a dock that cannot start reports why.
func (m *Model) runUpdateCommand() tea.Cmd {
	argv := slices.Clone(m.updates.Command)
	if len(argv) == 0 {
		m.notice = uiText("gvim.notice.update_command_missing", "Update command is not configured (update_command)")
		return nil
	}
	if index, ok := m.dock.FindSource(updateTabSource); ok {
		// Keep the previous output above the new run.
		m.dock.SetActive(index)
		m.notice = uiText("gvim.notice.update_running", "Updating...")
		return tea.Batch(m.showDock(), m.dock.Restart())
	}
	launch := terminal.Launch{Argv: argv, Source: updateTabSource}
	start, ok := m.dock.StartProgram(launch, updateTabName, m.dock.LaunchDir())
	if !ok {
		m.notice = uiText("gvim.notice.update_dock_failed", "Update: the terminal dock could not start the update command")
		return nil
	}
	m.notice = uiText("gvim.notice.update_running", "Updating...")
	return tea.Batch(m.showDock(), start)
}

// focusUpdateOutput shows the update tab and gives it the focus; before a run there is
// no output, and the notice says which command makes it.
func (m *Model) focusUpdateOutput() tea.Cmd {
	index, ok := m.dock.FindSource(updateTabSource)
	if !ok {
		m.notice = uiText("gvim.notice.update_no_output", "No update output yet: run Update fresh first")
		return nil
	}
	m.dock.SetActive(index)
	return m.showDock()
}
