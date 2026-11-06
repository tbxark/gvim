package dialog

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
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

// key edits the text with one key press. It returns the clipboard read that Ctrl+V starts.
func (f *field) key(kp tea.KeyPressMsg) tea.Cmd {
	f.clearPrefill(replacesPrefill(kp))
	var cmd tea.Cmd
	f.in, cmd = f.in.Update(kp)
	if tea.Key(kp).Keystroke() != "ctrl+v" {
		return nil
	}
	return cmd
}

// update passes any other message to the input model. A paste replaces an
// untouched prefill as typing does.
func (f *field) update(msg tea.Msg) {
	if p, ok := msg.(tea.PasteMsg); ok {
		f.clearPrefill(p.Content != "")
	}
	f.in, _ = f.in.Update(msg)
}

// draw paints the field on row y across w cells starting at x, and scrolls the
// text so that the caret stays inside. It returns the caret column and whether
// the caret is visible.
func (f *field) draw(g *grid.Grid, x, y, w int, st grid.Style) (int, bool) {
	f.in.SetWidth(w)
	g.Fill(x, x+w, y, st)
	runes := []rune(f.in.Value())
	pos := min(f.in.Position(), len(runes))
	start := 0
	for start < pos && ansi.StringWidth(string(runes[start:pos])) >= w {
		start++
	}
	g.Put(x, y, x+w, string(runes[start:]), st)
	caret := x + ansi.StringWidth(string(runes[start:pos]))
	return caret, caret < x+w
}
