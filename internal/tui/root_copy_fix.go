package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
)

// copyPath runs the copy action of the file path, the absolute one or the one relative to
// the editor's root. The editor works out the text, since it knows its root, and it also
// sets its own clipboard. The text goes to the terminal clipboard with tea.SetClipboard, as
// OSC 52. A refusal of the editor is the notice.
func (m *Model) copyPath(a editor.Action) tea.Cmd {
	if m.ed.Active() == nil {
		m.notice = noDocumentNotice
		return nil
	}
	res := m.editorDo(a, nil)
	if res.Err != nil {
		m.notice = res.Err.Error()
		return nil
	}
	m.notice = i18n.T("", "clipboard.copied_path", map[string]any{"path": res.Text})
	return tea.SetClipboard(res.Text)
}
