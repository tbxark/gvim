package ui

import tea "charm.land/bubbletea/v2"

// notApplicable gives the reason each command that cannot work in gvim is not run. The
// commands are the ones that only make sense for Fresh's own init file, its updater, its
// GUI or its daemon, and the ones that have no counterpart in a terminal editor. Each
// one shows the reason on the status row, so the palette does not report it as
// unsupported.
var notApplicable = map[string]string{
	"detach":                    "gvim has no server to detach from. The session is saved when gvim quits, and the next start restores it.",
	"workspace_trust_prompt":    "gvim has no workspace trust model, so there is nothing to trust, restrict or block.",
	"update_fresh":              "gvim does not update itself. Install a newer build of the binary to update it.",
	"open_update_log":           "gvim does not update itself, so it has no update output to show.",
	"init_check":                "gvim has no init script to check. Its settings are in the JSON config file, opened by Open Settings.",
	"init_edit":                 "gvim has no init script to edit. Its settings are in the JSON config file, opened by Open Settings.",
	"init_reload":               "gvim has no init script to reload. Its settings are read when gvim starts.",
	"prompt_save_macro_to_init": "gvim has no init script to save a macro into. Macros last for this session.",
	"prompt_promote_macro":      "gvim has no init script to promote a macro into. Macros last for this session.",
	"trigger_wave_animation":    "The wave is a Fresh easter egg, and gvim does not animate the screen.",
	"inspect_theme_at_cursor":   "the caret has no theme key. It is drawn in reverse video over the colors of its cell (editor.fg, editor.bg, the current line or the selection).",
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
