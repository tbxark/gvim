package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/popup"
)

// ClipboardWriter is the optional write half of the system clipboard. The ClipboardReader
// of DockPorts may implement it; copy and cut then also write through it, since not every
// terminal honours OSC 52.
type ClipboardWriter interface {
	Write(text string) error
}

// HTMLClipboardWriter is the optional rich-text half of the system clipboard. The
// ClipboardReader of DockPorts may implement it; copy with formatting then puts the HTML on
// the clipboard with text as its plain-text form. A terminal clipboard (OSC 52) has no rich
// text, so without it the copy is plain text.
type HTMLClipboardWriter interface {
	WriteHTML(html, text string) error
}

// htmlCopiedMsg is the result of the native write of a copy with formatting.
type htmlCopiedMsg struct {
	theme string
	err   error
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
	return m.clipboardOutArgs(action, nil, res)
}

// clipboardOutArgs is clipboardOut for an action that ran with args. Copy with formatting
// reads its theme from them.
func (m *Model) clipboardOutArgs(action string, args map[string]any, res editor.Result) tea.Cmd {
	switch action {
	case "copy", "cut", "copy_with_theme":
	default:
		return nil
	}
	if res.Err != nil || res.Text == "" {
		return nil
	}
	cmd := tea.SetClipboard(res.Text)
	if action == "copy_with_theme" {
		if rich := m.copyFormatted(args, res.Text); rich != nil {
			return tea.Batch(cmd, rich)
		}
	}
	return tea.Batch(cmd, m.nativeWrite(res.Text))
}

// nativeWrite is the command that writes text through the app's clipboard writer, or nil
// when there is none.
func (m *Model) nativeWrite(text string) tea.Cmd {
	w, ok := m.dock.Clipboard().(ClipboardWriter)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		_ = w.Write(text)
		return nil
	}
}

// copyFormatted returns the command that puts text on the system clipboard as HTML with the
// syntax colours of the theme named in args (the current theme when none is named). It is
// nil when the clipboard cannot hold rich text, and the status row then says that the copy
// is plain text. The row says what was copied at once, and says so again if the write fails.
func (m *Model) copyFormatted(args map[string]any, text string) tea.Cmd {
	w, ok := m.dock.Clipboard().(HTMLClipboardWriter)
	if !ok {
		m.notice = "Copied as plain text: this clipboard has no rich text"
		return nil
	}
	st, name := m.panes, ""
	if n, _ := args["theme"].(string); n != "" {
		th, _, err := m.loadTheme(n)
		if err != nil {
			m.notice = fmt.Sprintf("Copied as plain text: theme %q not found", n)
			return nil
		}
		st, name = editorpane.NewStyles(th), th.Name
	}
	html, ok := editorpane.SelectionHTML(m.ed.Active(), st, text)
	if !ok {
		m.notice = "Copied as plain text: the selection is too long to format"
		return nil
	}
	m.notice = "Copied with formatting"
	if name != "" {
		m.notice += " (" + name + ")"
	}
	plain := m.nativeWrite(text)
	return func() tea.Msg {
		err := w.WriteHTML(html, text)
		if err != nil && plain != nil {
			plain() // the rich write failed, so the plain text still reaches the clipboard
		}
		return htmlCopiedMsg{theme: name, err: err}
	}
}

// editorPaste starts a paste in the editor by reading the system clipboard in a command.
// It reports whether it took the action. The editor pastes its own register when the
// clipboard cannot be read, so a paste without a reader runs in the editor as it is. A
// macro replay does not read it either: the read arrives after the steps that follow the
// paste, so the paste runs now, from the editor's register.
func (m *Model) editorPaste(action string, args map[string]any) (tea.Cmd, bool) {
	if action != "paste" || args["text"] != nil || m.dock.Clipboard() == nil || m.ed.Active() == nil || m.root.macro.playing {
		return nil, false
	}
	cb := m.dock.Clipboard()
	return func() tea.Msg {
		text, err := cb.Read()
		return editorPasteMsg{text: text, err: err}
	}, true
}

// clipMessage handles the clipboard messages: the result of a paste's read, and a bracketed
// paste from the terminal. It reports whether it took msg.
func (m *Model) clipMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case htmlCopiedMsg:
		if msg.err != nil {
			m.notice = "Copied as plain text: " + msg.err.Error()
		}
		return nil, true
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
			m.dock.SendPaste(msg.Content)
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
		top, ok := m.overlays[len(m.overlays)-1].(*popup.Popup)
		if !ok {
			return
		}
		if c := m.lsp.comp; c != nil && c.pop == top {
			m.closeCompletion(c)
		} else {
			m.popOverlay()
		}
	}
}
