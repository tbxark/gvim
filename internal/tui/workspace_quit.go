package tui

import tea "charm.land/bubbletea/v2"

// workspaceQuitPort coordinates the one quit confirmation across live workspaces.
type workspaceQuitPort interface {
	QuitDirtyCount() int
	QuitNextDirty() (*Model, int)
	QuitActivate(*Model) error
	QuitDiscardAll() error
}

// QuitDiscardModified closes modified tabs through the normal forced-close path.
func (m *Model) QuitDiscardModified() error {
	t := m.tabPort()
	if t == nil {
		return nil
	}
	tabs := m.ed.Tabs()
	for i := len(tabs) - 1; i >= 0; i-- {
		if tabs[i].Modified {
			if err := m.closeTab(t, i, true); err != nil {
				return err
			}
		}
	}
	m.syncViewport()
	return nil
}

type workspaceQuitNextMsg struct{}

func workspaceQuitNextCmd() tea.Cmd { return func() tea.Msg { return workspaceQuitNextMsg{} } }
