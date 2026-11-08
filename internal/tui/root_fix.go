package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// fixState is the state of the root fixes: the choices about auto-revert. Its zero value
// leaves auto-revert on for every file.
type fixState struct {
	// autoRevert holds the choice Toggle Auto-Revert made for a path: false switches the
	// reload of its buffer off. A path that is not there is on. See autoRevertOn.
	autoRevert map[string]bool
	// polled holds the stamp of the change the poll last acted on for each path. See
	// revertPolled.
	polled map[string]string
}

// fixAction runs the actions the root fixes own: Revert File, the tab list, the copy of
// a file path, Toggle Auto-Revert and the extract-tab command. It reports whether it
// handled the action, and the command the action starts. args are the command's
// arguments, nil when the action comes from the palette or the menu.
func (m *Model) fixAction(action string, args map[string]any) (tea.Cmd, bool) {
	switch action {
	case "revert":
		force, _ := args["force"].(bool)
		return m.revertFile(force), true
	case "switch_to_tab_by_name":
		if _, named := args["name"].(string); named {
			// A command that names the tab goes to the editor, as scripts send it.
			return nil, false
		}
		return m.openTabList(), true
	case "copy_file_path", "copy_relative_file_path":
		return m.copyPath(editor.Action(action)), true
	case "toggle_auto_revert":
		return m.toggleAutoRevert(), true
	case "extract_tab_to_new_workspace":
		m.notice = extractTabNotice
		return nil, true
	}
	return nil, false
}

// fixMessage handles the messages of the root fixes. It reports whether it took msg, and
// the command the message starts. A watch message is observed here and not taken, so the
// root still reloads the tree on it.
func (m *Model) fixMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case revertDoneMsg:
		return m.revertDone(msg), true
	case prompt.Done:
		switch msg.Tag.(type) {
		case revertTag:
			return m.revertAnswer(msg), true
		case revertRetryTag:
			return m.revertRetryAnswer(msg), true
		}
	case watchMsg:
		return m.autoRevertChanged(msg.files), false
	case revertPollMsg:
		return m.revertPollTick(), true
	case revertStatMsg:
		return m.revertPolled(msg), true
	}
	return nil, false
}
