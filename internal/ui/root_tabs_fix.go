package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// openTabList opens the list of the tab names for Switch to Tab by Name. Choosing a row
// activates that tab. With no tab open the notice says so, and no list opens.
func (m *Model) openTabList() tea.Cmd {
	tabs := m.ed.Tabs()
	if len(tabs) == 0 {
		m.notice = "No open tabs to switch to"
		return nil
	}
	rows := make([]listRow, len(tabs))
	for i, t := range tabs {
		rows[i] = listRow{left: t.Name, value: strconv.Itoa(i)}
	}
	run := func(v *listView, r listRow) tea.Cmd {
		v.active = false
		i, err := strconv.Atoi(r.value)
		if err != nil {
			return nil
		}
		m.activateTab(i)
		m.focusEditor()
		m.notice = "Switched to " + r.left
		return nil
	}
	m.pushOverlay(listOverlay{v: newListView("Switch to tab", rows, m.listChrome(), run)})
	return nil
}
