package dialog

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// field is the one-line text input of a dialog. The textinput model does the
// editing. The field paints its own row, so the dialog can scroll the text to
// keep the caret in view.
//
// A field can be untouched: it holds a prefill that the user has not edited
// yet. The first printable key then replaces the whole prefill, as if the text
// were selected. Any other key ends the untouched state and keeps the text.
type field struct {
	in        textinput.Model
	untouched bool
}

// newField returns a field that starts with value. The caret goes to its end.
func newField(value string) *field {
	in := textinput.New()
	in.Prompt = ""
	in.SetVirtualCursor(false)
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return &field{in: in}
}

// selectAll marks the current text as an untouched prefill, so that the first
// printable key replaces it.
func (f *field) selectAll() {
	f.untouched = f.in.Value() != ""
}

// value returns the text.
func (f *field) value() string {
	return f.in.Value()
}

// replacesPrefill reports whether kp types text, which replaces an untouched
// prefill. Alt and Ctrl combinations do not type text.
func replacesPrefill(kp tea.KeyPressMsg) bool {
	return kp.Text != "" && kp.Mod&^tea.ModShift == 0
}

// clearPrefill ends the untouched state. It clears the text when the message
// types text, and it keeps the text otherwise.
func (f *field) clearPrefill(replace bool) {
	if !f.untouched {
		return
	}
	f.untouched = false
	if replace {
		f.in.SetValue("")
	}
}

// fieldPasteMsg is the result of the clipboard read that Ctrl+V starts: raw is what the
// input model's paste command returned, which only the input model can read.
type fieldPasteMsg struct{ raw tea.Msg }

// key edits the text with one key press. It returns the clipboard read that Ctrl+V starts.
// An untouched prefill is kept until the read arrives, so a failed or empty read leaves it.
func (f *field) key(kp tea.KeyPressMsg) tea.Cmd {
	if tea.Key(kp).Keystroke() == "ctrl+v" {
		return func() tea.Msg { return fieldPasteMsg{raw: textinput.Paste()} }
	}
	f.clearPrefill(replacesPrefill(kp))
	f.in, _ = f.in.Update(kp)
	return nil
}

// update passes any other message to the input model. A paste replaces an
// untouched prefill as typing does: a bracketed paste, and the clipboard text a
// Ctrl+V read.
func (f *field) update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		f.clearPrefill(msg.Content != "")
	case fieldPasteMsg:
		f.pasteRead(msg.raw)
		return
	}
	f.in, _ = f.in.Update(msg)
}

// pasteRead gives the input model the clipboard read of Ctrl+V. The text goes in at the
// caret; when the field held an untouched prefill and the text changed the value, the
// prefill is replaced by that text, as typing replaces it.
func (f *field) pasteRead(raw tea.Msg) {
	before := []rune(f.in.Value())
	pos := min(f.in.Position(), len(before))
	f.in, _ = f.in.Update(raw)
	after := []rune(f.in.Value())
	if !f.untouched || len(after) == len(before) {
		return
	}
	f.untouched = false
	if n := len(after) - len(before); n > 0 && pos+n <= len(after) {
		f.in.SetValue(string(after[pos : pos+n]))
		f.in.CursorEnd()
	}
}

// draw paints the field on row y across w cells starting at x, and scrolls the
// text so that the caret stays inside. It returns the caret column and whether
// the caret is visible.
func (f *field) draw(g *grid.Grid, x, y, w int, st grid.Style) (int, bool) {
	f.in.SetWidth(w)
	g.Fill(x, x+w, y, st)
	shown, col := space.Window(f.in.Value(), f.in.Position(), w)
	g.Put(x, y, x+w, shown, st)
	caret := x + col
	return caret, caret < x+w
}
