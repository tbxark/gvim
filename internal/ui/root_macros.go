package ui

import "fmt"

// listMacros opens the steps of the kept macro, in order. The recorder keeps one
// macro, so there is one list. With no macro it says so on the status row.
func (m *Model) listMacros() {
	steps := m.root.macro.last
	if len(steps) == 0 {
		m.notice = "No macro recorded"
		return
	}
	rows := make([]listRow, len(steps))
	for i, s := range steps {
		rows[i] = listRow{left: fmt.Sprintf("%d. %s", i+1, s.cmd.Action)}
	}
	m.pushOverlay(listOverlay{v: newListView("Recorded macro", rows, m.listChrome(), nil)})
}
