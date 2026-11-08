package tui

import tea "charm.land/bubbletea/v2"

// fileIndexRestarter is the optional surface of the palette's file list that can enumerate
// again in place. finder.Index implements it; tests can fake it. The palette's Files port
// itself only needs Snapshot, so this stays an optional interface.
type fileIndexRestarter interface {
	Restart()
}

// fileIndexRefreshCmd returns the command that re-enumerates the file list, or nil when the
// index cannot restart. The restart runs in a command: it cancels and waits for the old
// enumeration and starts a new one, so it must never run on the update goroutine.
func (m *Model) fileIndexRefreshCmd() tea.Cmd {
	r, ok := m.files.(fileIndexRestarter)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		r.Restart()
		return nil
	}
}

// refreshExplorerCmd answers the File Explorer: Refresh action: reload the tree and, when
// it can, re-enumerate the file list too (the quick-open picker's list is a snapshot of the
// workspace and would otherwise stay the one it saw at launch/switch).
func (m *Model) refreshExplorerCmd() tea.Cmd {
	return tea.Batch(m.reloadTreeAsync(), m.fileIndexRefreshCmd())
}
