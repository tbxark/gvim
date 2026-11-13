package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
)

// openTabList opens the list of the tab names for Switch to Tab by Name. Choosing a row
// activates that tab. With no tab open the notice says so, and no list opens.
func (m *Model) openTabList() tea.Cmd {
	tabs := m.ed.Tabs()
	if len(tabs) == 0 {
		m.notice = "No open tabs to switch to"
		return nil
	}
	rows := make([]list.Row, len(tabs))
	for i, t := range tabs {
		rows[i] = list.Row{Left: t.Name, Value: strconv.Itoa(i)}
	}
	run := func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		i, err := strconv.Atoi(r.Value)
		if err != nil {
			return nil
		}
		m.activateTab(i)
		m.focusEditor()
		m.notice = "Switched to " + r.Left
		return nil
	}
	m.pushOverlay(list.New("Switch to tab", rows, m.listChrome(), run))
	return nil
}
