package ui

import tea "charm.land/bubbletea/v2"

// featureAction runs an action of the root features in this file group: the warnings,
// the bookmark and macro commands, the hidden-files toggle, the stash review, the
// screen commands and the dumps. It reports whether it handled the action, and the
// command the action starts. The commands that cannot work in gvim are in notice_na.go.
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
	case "load_plugin_from_buffer":
		// The plugin host loads the whole plugin folder, and has no call to load one
		// file, so the buffer cannot be loaded as a plugin yet.
		m.notice = "Load Plugin from Buffer is not available yet: the plugin host cannot load one file"
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
	switch msg := msg.(type) {
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
