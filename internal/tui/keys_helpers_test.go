package tui

import tea "charm.land/bubbletea/v2"

// doAction runs a keymap action the way a key press does, without a key. The presets keep
// only the common shortcuts, so the actions that lost their default key (the search and
// replace set, for one) are driven by name: what those tests check is the action, not the
// key that used to start it.
func doAction(m *Model, action string) tea.Cmd {
	return m.dispatch(Command{Action: action})
}
