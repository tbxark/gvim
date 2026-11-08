package tui

import tea "charm.land/bubbletea/v2"

// featureAction runs an action of the root features in this file group: the warnings,
// the bookmark and macro commands, the hidden-files toggle, the stash review, the
// screen commands, the dumps and the plugin management commands (plugin_manager.go). It
// reports whether it handled the action, and the command the action starts. The commands
// that cannot work in gvim are in notice_na.go.
func (m *Model) featureAction(action string) (tea.Cmd, bool) {
	switch action {
	case "show_warnings":
		m.showWarnings()
	case "clear_warnings":
		m.clearWarnings()
	case "list_bookmarks":
		// The editor keeps the bookmark slots and does not list them, so there is
		// nothing to show yet.
		m.notice = "List Bookmarks is not available yet: the editor does not list its bookmark slots"
	case "plugin_manager", "plugin_show_log", "plugin_reload", "plugin_reload_all", "load_plugin_from_buffer",
		"init_check", "init_edit", "init_reload", "prompt_save_macro_to_init", "prompt_promote_macro":
		return m.pluginUIAction(action), true
	case "quit_discard":
		return m.quitDiscard(), true
	case "list_macros":
		m.listMacros()
	case "prompt_set_bookmark":
		m.openBookmarkPrompt(false)
	case "prompt_jump_to_bookmark":
		m.openBookmarkPrompt(true)
	case "prompt_play_macro":
		// The recorder keeps one macro, so there is no slot to ask for: this plays it.
		return m.macroAction("play_last_macro"), true
	case "file_explorer_toggle_hidden":
		return m.toggleHidden(), true
	case "review_interrupted_saves":
		return m.reviewInterrupted(), true
	case "redraw_screen":
		return tea.ClearScreen, true
	case "suspend_process":
		return m.suspend(), true
	case "dump_config":
		return m.openScratch(dumpConfig(m.util.settings)), true
	case "dump_ui_tree":
		return m.openScratch(m.uiTree()), true
	default:
		return m.notApplicableNotice(action)
	}
	return nil, true
}

// featureMessage handles the messages of the root features in this file group. It
// reports whether it took msg, and the command the message starts.
func (m *Model) featureMessage(msg tea.Msg) (tea.Cmd, bool) {
	if cmd, ok := m.clipMessage(msg); ok {
		return cmd, true
	}
	if cmd, ok := m.pluginManagerMessage(msg); ok {
		return cmd, true
	}
	switch msg := msg.(type) {
	case warningDetailMsg:
		m.openWarningDetail(msg.text)
		return nil, true
	case interruptedMsg:
		m.reviewed(msg)
		return nil, true
	case tea.ResumeMsg:
		// The terminal was given back to the shell during a suspend, so the next frame
		// repaints every cell.
		return tea.ClearScreen, true
	}
	return nil, false
}
