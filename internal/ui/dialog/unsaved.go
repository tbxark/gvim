package dialog

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// UnsavedChanges asks what to do with a buffer that has unsaved changes. The
// buttons are Save, Discard and Cancel. Save starts with the focus, so Enter
// saves. The keys are s, d and c, and Esc cancels.
type UnsavedChanges struct {
	modal
	name string
}

var _ Overlay = (*UnsavedChanges)(nil)

// NewUnsavedChanges returns the dialog for the buffer name. Tag comes back in
// the Result.
func NewUnsavedChanges(clock Clock, name string, tag any) *UnsavedChanges {
	d := &UnsavedChanges{name: name}
	d.setup(clock, "Unsaved Changes", tag, ChoiceCancel, []button{
		newButton("Save", 0, ChoiceSave),
		newButton("Discard", 0, ChoiceDiscard),
		newButton("Cancel", 0, ChoiceCancel),
	})
	return d
}

// Update handles one message. See Overlay.
func (d *UnsavedChanges) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *UnsavedChanges) Draw(g *grid.Grid) {
	d.drawText(g, "Save changes to "+d.name+"?")
}
