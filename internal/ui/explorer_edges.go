package ui

import tea "charm.land/bubbletea/v2"

// explorerEdgeKey moves the explorer cursor to the first or the last row on Home and
// End. Fresh binds neither key in the file explorer, so no keymap carries them: every
// tree view takes them, and a key no binding claims would otherwise do nothing.
func (m *Model) explorerEdgeKey(kp KeyPress) (tea.Cmd, bool) {
	if len(kp.Mods) != 0 {
		return nil, false
	}
	switch kp.Name {
	case "Home":
		return m.explorerAction("file_explorer_first")
	case "End":
		return m.explorerAction("file_explorer_last")
	}
	return nil, false
}
