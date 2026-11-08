package dialog

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// SaveAs asks for the file name to save a buffer to. It has a text field and
// the buttons Save and Cancel. Enter saves. If the path already exists, a second
// step asks to Replace it, with Replace and Cancel. Cancel in that step returns
// to the name, with the text kept. A rejected path (see PathChecks.Validate)
// shows its message inline and the dialog stays open.
type SaveAs struct {
	modal
	input      *field
	checks     PathChecks
	pending    string // the path waiting for the overwrite answer
	confirming bool   // the overwrite step is showing
	sizeText   string // the prefill; the box is at least wide enough for it
}

var _ Overlay = (*SaveAs)(nil)

// NewSaveAs returns the dialog with prefill in the name field. Tag comes back in
// the Result, and checks are the path questions the opener answers.
func NewSaveAs(clock Clock, prefill string, tag any, checks PathChecks, ov style.Overlay) *SaveAs {
	d := &SaveAs{input: newField(prefill), checks: checks, sizeText: prefill}
	d.input.selectAll()
	d.setup(clock, "Save As", tag, ChoiceCancel, saveButtons(), ov)
	d.field = d.input
	d.onPress = d.press
	return d
}

// saveButtons returns the buttons of the name step.
func saveButtons() []button {
	return []button{
		newButton("Save", 0, ChoiceSave),
		newButton("Cancel", 0, ChoiceCancel),
	}
}

// Update handles one message. See Overlay.
func (d *SaveAs) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *SaveAs) Draw(g *grid.Grid) {
	if d.confirming {
		d.drawText(g, fmt.Sprintf("%s already exists. Replace it?", d.pending))
		return
	}
	// The caret sits after the text, so the field needs one cell more than the text.
	d.frame(g, d.natural(d.sizeText+" "), func(cw int) []row {
		rows := []row{d.fieldRow()}
		if d.errText != "" {
			rows = append(rows, textRows(d.errText, cw, d.ov.Error)...)
		}
		return rows
	})
}

// press activates button i of the current step.
func (d *SaveAs) press(i int) tea.Cmd {
	choice := d.buttons[i].choice
	if d.confirming {
		if choice == ChoiceReplace {
			return d.finish(ChoiceReplace, d.pending)
		}
		d.endConfirm()
		return nil
	}
	if choice == ChoiceCancel {
		return d.finish(ChoiceCancel, "")
	}
	path := strings.TrimSpace(d.input.value())
	if path == "" {
		d.errText = "Enter a file name"
		return nil
	}
	if d.checks.Validate != nil {
		if err := d.checks.Validate(path); err != nil {
			d.errText = err.Error()
			return nil
		}
	}
	if d.checks.Exists != nil && d.checks.Exists(path) {
		d.beginConfirm(path)
		return nil
	}
	return d.finish(ChoiceSave, path)
}

// beginConfirm moves to the overwrite step for path. Cancel has the focus, so
// a stray Enter does not replace the file.
func (d *SaveAs) beginConfirm(path string) {
	d.confirming = true
	d.pending = path
	d.field = nil
	d.title = "Replace File"
	d.buttons = []button{
		newButton("Replace", 0, ChoiceReplace),
		newButton("Cancel", 0, ChoiceCancel),
	}
	d.errText = ""
	d.hover = -1
	d.setFocusButton(1)
}

// endConfirm returns from the overwrite step to the name, with the text kept.
func (d *SaveAs) endConfirm() {
	d.confirming = false
	d.pending = ""
	d.field = d.input
	d.title = "Save As"
	d.buttons = saveButtons()
	d.hover = -1
	d.focus = 0
}
