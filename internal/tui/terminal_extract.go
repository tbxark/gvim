package tui

// The extract-terminal-tab surface: the workspace layer (app) captures the active
// dock tab's saved record, moves it into a target workspace's terminal record and
// closes this dock tab. No PTY crosses the workspace boundary: the target restores
// the record and revives it on the first dive (docs/terminal.md, docs/ui.md).

import (
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/dock"
)

// ActiveTerminalTab returns the active dock tab's saved record, or false when the
// dock has no tab. It runs on the UI goroutine; the record carries the transcript,
// the launch/resume argv, the live flag and the styled screen snapshot.
func (m *Model) ActiveTerminalTab() (terminal.SavedTab, bool) {
	return m.dock.ActiveSavedTab()
}

// AppendRestoredTerminalTab appends a saved tab to this dock without running
// anything, for a terminal tab extracted from another workspace. It reports false
// when the record is empty. The tab revives on the user's first dive into it; the
// current tabs and active tab are unchanged.
func (m *Model) AppendRestoredTerminalTab(tab terminal.SavedTab) bool {
	if len(tab.Rows) == 0 && len(tab.LaunchArgv) == 0 && tab.Cwd == "" && tab.Name == "" {
		return false
	}
	m.dock.AppendRestoredTab(tab)
	return true
}

// CloseActiveTerminalTabForExtract force-closes the active dock tab when its saved
// name is name, without asking: the extract action already wrote the tab's record
// into the target workspace, so closing stops this shell and drops nothing. It
// reports false when the active tab changed, leaving everything untouched.
func (m *Model) CloseActiveTerminalTabForExtract(name string) bool {
	tab, ok := m.dock.ActiveSavedTab()
	if !ok || tab.Name != name {
		return false
	}
	m.dock.CloseTab()
	return true
}

// TerminalTabRefs returns every dock tab as an extract-picker reference.
func (m *Model) TerminalTabRefs() []dock.TabRef { return m.dock.TabRefs() }

// TerminalTabStateAt returns dock tab i's saved record, for the mixed extract
// action's capture on the UI goroutine.
func (m *Model) TerminalTabStateAt(i int) (terminal.SavedTab, bool) {
	return m.dock.SavedTabAt(i)
}

// CloseTerminalTabAtForExtract force-closes dock tab i when its saved name still
// matches name. The extract action already wrote the record into the target, so
// closing stops the source shell and drops nothing; a changed tab reports false.
func (m *Model) CloseTerminalTabAtForExtract(i int, name string) bool {
	return m.dock.CloseTabAtForExtract(i, name)
}
