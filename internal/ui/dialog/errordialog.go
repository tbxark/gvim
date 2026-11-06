package dialog

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// ErrorDialog shows a message, such as a failed save, with one OK button. Esc
// and O also answer OK.
type ErrorDialog struct {
	modal
	message string
}

var _ Overlay = (*ErrorDialog)(nil)

// NewErrorDialog returns the dialog that shows message. Tag comes back in the
// Result.
func NewErrorDialog(clock Clock, message string, tag any, ov style.Overlay) *ErrorDialog {
	d := &ErrorDialog{message: message}
	d.setup(clock, "Error", tag, ChoiceOK, []button{
		newButton("OK", 0, ChoiceOK),
	}, ov)
	return d
}

// Update handles one message. See Overlay.
func (d *ErrorDialog) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *ErrorDialog) Draw(g *grid.Grid) {
	d.drawText(g, d.message)
}
