package tui

import (
	"fmt"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/prompt"
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

// promptClipNativeGate is the process-wide permission for a native prompt clipboard
// write. Every workspace model shares the app's SystemClipboard writer, so a hidden warm
// model's late write and a new workspace's write must not overlap. The permission is
// acquired in Update and released only by the operation that owns it; a second copy/cut is
// refused instead of queued, so a hung writer cannot grow an unbounded wait queue. There is
// no cancellation API for a writer that has already started: a hang keeps the permission
// until it returns (the limitation is documented in docs/ui.md).
var promptClipNativeGate atomic.Pointer[promptClipOp]

func promptClipNativeAcquire(op *promptClipOp) bool {
	return promptClipNativeGate.CompareAndSwap(nil, op)
}

func promptClipNativeRelease(op *promptClipOp) {
	promptClipNativeGate.CompareAndSwap(op, nil)
}

func promptClipNativeBusy() bool {
	return promptClipNativeGate.Load() != nil
}

// promptClipOp is the cancellable identity of one native prompt write. The writer command
// checks it after permission is acquired and before calling Write, so a request canceled
// while it was still waiting to start never touches the clipboard. started and finished
// let close/cancel release the permission only when no writer call is in progress. A write
// already in progress cannot be stopped; the API has no cancel.
type promptClipOp struct {
	canceled atomic.Bool
	started  atomic.Bool
	finished atomic.Bool
}

func (o *promptClipOp) cancel() { o.canceled.Store(true) }

// beginWrite marks the write started after a final cancel check. A cancel that lands
// after this point saw started set and will not release the permission; the command that
// started is responsible for releasing it when it finishes.
func (o *promptClipOp) beginWrite() bool {
	if o.canceled.Load() {
		return false
	}
	o.started.Store(true)
	return !o.canceled.Load()
}

// promptClipTarget names the prompt an asynchronous copy/cut started on: a text line or
// a confirm. The pointer identity, not the text, decides whether a result still belongs to
// the active top prompt.
type promptClipTarget struct {
	line    *prompt.Line
	confirm *prompt.Confirm
}

// promptCutMsg is the result of the prompt cut's native clipboard write. The prompt keeps
// its text until this message reports success, so a failed or absent write cannot lose it.
// owner and token must still match the live model and the latest prompt clipboard
// operation; line must still be the active top prompt.
type promptCutMsg struct {
	line         *prompt.Line
	text         string
	err          error
	skipped      bool
	selStart     int
	selEnd       int
	hasSelection bool
	owner        palette.Owner
	token        uint64
}

// promptCopyMsg is the result of the prompt copy's native clipboard write. Copy has no
// prompt mutation, but the notice waits for the write so a failure cannot claim Copied.
type promptCopyMsg struct {
	target  promptClipTarget
	text    string
	err     error
	skipped bool
	owner   palette.Owner
	token   uint64
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
	m.rememberYank(res.Text)
	cmd := tea.SetClipboard(res.Text)
	if action == "copy_with_theme" {
		if rich := m.copyFormatted(args, res.Text); rich != nil {
			return tea.Batch(cmd, rich)
		}
	}
	return tea.Batch(cmd, m.nativeWrite(res.Text))
}

// rememberYank keeps the last text a copy or cut put on the clipboard. A Ctrl+V in the
// terminal falls back to it when the system clipboard cannot provide text
// (terminal.pass_ctrl_v_without_text).
func (m *Model) rememberYank(text string) {
	if text != "" {
		m.lastYank = text
	}
}

// nativeWriteFailedMsg reports a failed native clipboard write (the OSC 52 write sent with
// it may still have reached the terminal).
type nativeWriteFailedMsg struct{ err error }

// nativeWrite is the command that writes text through the app's clipboard writer, or nil
// when there is none. A failed write comes back as a nativeWriteFailedMsg, and a good one
// returns no message.
func (m *Model) nativeWrite(text string) tea.Cmd {
	w, ok := m.dock.Clipboard().(ClipboardWriter)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		if err := w.Write(text); err != nil {
			return nativeWriteFailedMsg{err: err}
		}
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
		m.notice = uiText("gvim.notice.clipboard.plain_no_rich", "Copied as plain text: this clipboard has no rich text")
		return nil
	}
	st, name := m.panes, ""
	if n, _ := args["theme"].(string); n != "" {
		th, _, err := m.loadTheme(n)
		if err != nil {
			m.notice = uiTextVars("gvim.notice.clipboard.theme_not_found", "Copied as plain text: theme %{theme} not found", map[string]any{"theme": fmt.Sprintf("%q", n)})
			return nil
		}
		st, name = editorpane.NewStyles(th), th.Name
	}
	html, ok := editorpane.SelectionHTML(m.ed.Active(), st, text)
	if !ok {
		m.notice = uiText("gvim.notice.clipboard.plain_too_long", "Copied as plain text: the selection is too long to format")
		return nil
	}
	m.notice = uiText("gvim.notice.clipboard.formatted", "Copied with formatting")
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
			m.notice = uiTextVars("gvim.notice.clipboard.plain_error", "Copied as plain text: %{error}", map[string]any{"error": msg.err.Error()})
		}
		return nil, true
	case nativeWriteFailedMsg:
		m.notice = uiTextVars("gvim.notice.clipboard.copy_failed", "Copy failed: %{error}", map[string]any{"error": msg.err.Error()})
		return nil, true
	case promptCutMsg:
		return m.promptCutResult(msg), true
	case promptCopyMsg:
		return m.promptCopyResult(msg), true
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

// promptClipValid reports whether a prompt clipboard result still belongs to the live
// model and the same active top prompt. Every result (success or failure) is checked before
// it may change the notice or the prompt: a closed or replaced prompt, a stale workspace
// generation, or an older operation token is dropped silently.
func (m *Model) promptClipValid(owner palette.Owner, token uint64, target promptClipTarget) bool {
	if owner != m.workspaceOwner || token != m.promptClipToken || len(m.overlays) == 0 {
		return false
	}
	top := m.overlays[len(m.overlays)-1]
	switch {
	case target.line != nil:
		line, ok := top.(*prompt.Line)
		return ok && line == target.line && line.Active()
	case target.confirm != nil:
		confirm, ok := top.(*prompt.Confirm)
		return ok && confirm == target.confirm && confirm.Active()
	}
	return false
}

// promptCutResult applies a native clipboard write result to the prompt it started on.
// Identity, owner and token are checked first, so a late or superseded cut never changes
// the notice or another prompt. On success the same line must still hold the copied text
// before it is cleared; a failure keeps the text and reports the error. No Cut is claimed
// for a stale or changed result.
func (m *Model) promptCutResult(msg promptCutMsg) tea.Cmd {
	if op := m.finishPromptClip(msg.token); op != nil {
		promptClipNativeRelease(op)
	}
	if msg.skipped || !m.promptClipValid(msg.owner, msg.token, promptClipTarget{line: msg.line}) {
		return nil
	}
	if msg.err != nil {
		m.notice = uiTextVars("gvim.notice.clipboard.cut_failed", "Cut failed: %{error}; the prompt was kept", map[string]any{"error": msg.err.Error()})
		return nil
	}
	if msg.hasSelection {
		start, end, ok := msg.line.SelectedRange()
		if !ok || start != msg.selStart || end != msg.selEnd || msg.line.SelectedText() != msg.text {
			m.notice = uiText("gvim.notice.clipboard.cut_changed", "Copied, but the prompt changed before the cut completed; the prompt was kept")
			return nil
		}
		msg.line.DeleteSelection()
		m.notice = i18n.T("", "clipboard.cut", nil)
		return nil
	}
	if msg.line.Value() != msg.text {
		m.notice = uiText("gvim.notice.clipboard.cut_changed", "Copied, but the prompt changed before the cut completed; the prompt was kept")
		return nil
	}
	msg.line.CutValue()
	m.notice = i18n.T("", "clipboard.cut", nil)
	return nil
}

// promptCopyResult applies a native clipboard write result to the prompt copy it started
// on. The same identity/owner/token check runs before any notice, so a copy that finishes
// after its prompt closed or switched never writes Copied or a failure into the UI.
func (m *Model) promptCopyResult(msg promptCopyMsg) tea.Cmd {
	if op := m.finishPromptClip(msg.token); op != nil {
		promptClipNativeRelease(op)
	}
	if msg.skipped || !m.promptClipValid(msg.owner, msg.token, msg.target) {
		return nil
	}
	if msg.err != nil {
		m.notice = uiTextVars("gvim.notice.clipboard.copy_failed", "Copy failed: %{error}", map[string]any{"error": msg.err.Error()})
	} else {
		m.notice = i18n.T("", "clipboard.copied", nil)
	}
	return nil
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
