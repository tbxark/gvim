package tui

import tea "charm.land/bubbletea/v2"

// toggleHidden flips the show-hidden setting of the tree and reloads the tree through
// the background reload the file watcher uses, so the listing shows the change.
func (m *Model) toggleHidden() tea.Cmd {
	if m.tree == nil {
		m.notice = "No file explorer"
		return nil
	}
	show := !m.tree.ShowHidden()
	m.tree.SetShowHidden(show)
	if show {
		m.notice = "Hidden files: shown"
	} else {
		m.notice = "Hidden files: hidden"
	}
	return m.reloadTreeAsync()
}
