package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
)

// ClipboardWriter is the optional write half of the system clipboard. The ClipboardReader
// of DockPorts may implement it; copy and cut then also write through it, since not every
// terminal honours OSC 52.
type ClipboardWriter interface {
	Write(text string) error
}

// editorPasteMsg carries what the system clipboard held when paste read it. err is the
// failure of the read, and text is empty when it held nothing.
type editorPasteMsg struct {
	text string
	err  error
}

// clipboardOut returns the command that puts the text of a finished copy or cut on the
// system clipboard: an OSC 52 write, and a native write when the app gave one. Nothing is
// written for a refused action or an empty text, so a failed copy keeps the old content.
func (m *Model) clipboardOut(action string, res editor.Result) tea.Cmd {
	switch action {
	case "copy", "cut", "copy_with_theme":
	default:
		return nil
	}
	if res.Err != nil || res.Text == "" {
		return nil
	}
	cmd := tea.SetClipboard(res.Text)
	if w, ok := m.dock.clipboard.(ClipboardWriter); ok {
		text := res.Text
		cmd = tea.Batch(cmd, func() tea.Msg {
			_ = w.Write(text)
			return nil
		})
	}
	return cmd
}

// editorPaste starts a paste in the editor by reading the system clipboard in a command.
// It reports whether it took the action. The editor pastes its own register when the
// clipboard cannot be read, so a paste without a reader runs in the editor as it is. A
// macro replay does not read it either: the read arrives after the steps that follow the
// paste, so the paste runs now, from the editor's register.
func (m *Model) editorPaste(action string, args map[string]any) (tea.Cmd, bool) {
	if action != "paste" || args["text"] != nil || m.dock.clipboard == nil || m.ed.Active() == nil || m.root.macro.playing {
		return nil, false
	}
	cb := m.dock.clipboard
	return func() tea.Msg {
		text, err := cb.Read()
		return editorPasteMsg{text: text, err: err}
	}, true
}

// clipMessage handles the clipboard messages: the result of a paste's read, and a bracketed
// paste from the terminal. It reports whether it took msg.
func (m *Model) clipMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case editorPasteMsg:
		if m.explorerFocus {
			return nil, true
		}
		var args map[string]any
		if msg.err == nil && msg.text != "" {
			args = map[string]any{"text": msg.text}
		}
		m.editorDo("paste", args)
		m.syncViewport()
		return nil, true
	case tea.PasteMsg:
		// An overlay has the message already: the update routed it there. A popup takes
		// no text, so it closes and the editor gets the paste, as it does for a key.
		m.closeTopPopups()
		if len(m.overlays) > 0 || m.explorerFocus {
			return nil, true
		}
		if m.dockKeyboard() {
			text := pasteText(msg.Content)
			m.dock.queue = append(m.dock.queue, func(t Terminal) error { return t.Paste(text) })
			return nil, true
		}
		if m.ed.Active() == nil {
			m.notice = noDocumentNotice
			return nil, true
		}
		if cmd, ok := m.pluginPaste(msg.Content); ok {
			return cmd, true
		}
		m.editorDo("paste", map[string]any{"text": msg.Content})
		m.syncViewport()
		return nil, true
	}
	return nil, false
}

// closeTopPopups closes the popups on top of the overlay stack, which take no text input.
func (m *Model) closeTopPopups() {
	for len(m.overlays) > 0 {
		top, ok := m.overlays[len(m.overlays)-1].(popupOverlay)
		if !ok {
			return
		}
		if c := m.lsp.comp; c != nil && c.pop == top.p {
			m.closeCompletion(c)
		} else {
			m.popOverlay()
		}
	}
}
