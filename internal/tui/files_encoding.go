package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/list"
)

// encodingAction opens the chooser for set_encoding, reload_with_encoding and
// set_line_ending when the command names no value, and reports whether it did. A
// command with its argument, which is what a choice dispatches, goes on to the
// editor.
func (m *Model) encodingAction(action string, args map[string]any) bool {
	switch action {
	case "set_encoding":
		if _, ok := args["name"]; !ok {
			m.openEncodings(action, "Select Encoding")
			return true
		}
	case "reload_with_encoding":
		if _, ok := args["name"]; !ok {
			m.openEncodings(action, "Reload with Encoding")
			return true
		}
	case "set_line_ending":
		if _, ok := args["value"]; !ok {
			m.openLineEndings()
			return true
		}
	}
	return false
}

// openEncodings lists the encodings the editor can read and write. The encoding of
// the active document is marked, and choosing a row runs action with the encoding's
// name.
func (m *Model) openEncodings(action, title string) {
	current := ""
	if doc := m.ed.Active(); doc != nil {
		current = doc.Encoding()
	}
	choices := editor.EncodingChoices()
	rows := make([]list.Row, len(choices))
	for i, c := range choices {
		right := ""
		// Text with no byte above 0x7f is reported as ASCII, and is UTF-8 on disk.
		if current == c.Label || current == "ASCII" && c.Name == "utf-8" {
			right = "current"
		}
		rows[i] = list.Row{Left: c.Label, Right: right, Value: c.Name}
	}
	v := list.New(title, rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		return m.dispatch(Command{Action: action, Args: map[string]any{"name": r.Value}})
	})
	m.pushOverlay(v)
}

// askOpenWithEncoding is the browser's Detect-Encoding-off path: it asks which
// encoding the chosen file should be read with, opens the file, then reloads it with
// that encoding so the tab's codec and label are the chosen one. A large file falls
// back to the normal auto-detect open: its read is windowed and the encoding chooser
// would require loading it whole first.
func (m *Model) askOpenWithEncoding(path string, line, col int) {
	rows := []list.Row{}
	for _, c := range editor.EncodingChoices() {
		rows = append(rows, list.Row{Left: c.Label, Value: c.Name})
	}
	v := list.New("Open with Encoding", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		if pc, ok := m.ed.(pathClassifier); ok && pc.IsLargePath(path) {
			cmd := m.openFileCmd(path, false, line, col)
			m.notice = "Encoding selection is not available for a large file; opened with auto-detection"
			m.focusEditor()
			return cmd
		}
		if err := m.openFileErr(path, false); err != nil {
			m.notice = err.Error()
			return nil
		}
		cmd := m.dispatch(Command{Action: "reload_with_encoding", Args: map[string]any{"name": r.Value}})
		if line > 0 {
			m.gotoLine(line, col)
		}
		m.focusEditor()
		return cmd
	})
	m.pushOverlay(v)
}

// openLineEndings lists the two line endings. The ending of the active document is
// marked, and choosing a row runs set_line_ending.
func (m *Model) openLineEndings() {
	current := ""
	if doc := m.ed.Active(); doc != nil {
		current = doc.LineEnding()
	}
	rows := make([]list.Row, 0, 2)
	for _, e := range []struct{ label, value string }{{"LF", "lf"}, {"CRLF", "crlf"}} {
		right := ""
		if e.label == current {
			right = "current"
		}
		rows = append(rows, list.Row{Left: e.label, Right: right, Value: e.value})
	}
	v := list.New("Select Line Ending", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		return m.dispatch(Command{Action: "set_line_ending", Args: map[string]any{"value": r.Value}})
	})
	v.Fit = true
	m.pushOverlay(v)
}

// statusSegmentClick runs the command behind the status row segment i: the line
// ending and encoding choosers, the language prompt, the LSP status and the
// Workspace Trust dialog.
func (m *Model) statusSegmentClick(i int) tea.Cmd {
	m.focusEditor()
	switch i {
	case frame.StatusSegmentEOL:
		return m.dispatch(Command{Action: "set_line_ending"})
	case frame.StatusSegmentEncoding:
		return m.dispatch(Command{Action: "set_encoding"})
	case frame.StatusSegmentLanguage:
		return m.dispatch(Command{Action: "set_language"})
	case frame.StatusSegmentPosition:
		return m.dispatch(Command{Action: "goto_line"})
	case frame.StatusSegmentLSP:
		return m.dispatch(Command{Action: "show_lsp_status"})
	case frame.StatusSegmentTrust:
		return m.openWorkspaceTrust()
	case frame.StatusSegmentUpdate:
		return m.dispatch(Command{Action: "show_update_output"})
	case frame.StatusSegmentHexView:
		return m.toggleHexView()
	}
	return nil
}
