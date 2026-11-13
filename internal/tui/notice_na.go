package tui

import tea "charm.land/bubbletea/v2"

// notApplicable gives the reason each command that cannot work in gvim is not run. The
// commands are the ones that only make sense for Fresh's updater or its GUI, and the ones
// that have no counterpart in a terminal editor. Each one shows the reason
// on the status row, so the palette does not report it as unsupported.
var notApplicable = map[string]string{
	"update_fresh":           "gvim does not update itself. Install a newer build of the binary to update it.",
	"open_update_log":        "gvim does not update itself, so it has no update output to show.",
	"trigger_wave_animation": "The wave is a Fresh easter egg, and gvim does not animate the screen.",
}

// notApplicableNotice runs a command of notApplicable: it shows the reason and starts
// nothing. It reports false for any other action.
func (m *Model) notApplicableNotice(action string) (tea.Cmd, bool) {
	reason, ok := notApplicable[action]
	if !ok {
		return nil, false
	}
	m.notice = "Not available in gvim: " + reason
	return nil, true
}
