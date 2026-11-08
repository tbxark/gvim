package dialog

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
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
func NewUnsavedChanges(clock Clock, name string, tag any, ov style.Overlay) *UnsavedChanges {
	d := &UnsavedChanges{name: name}
	d.setup(clock, dialogText("gvim.dialog.title.unsaved_changes", "Unsaved Changes"), tag, ChoiceCancel, []button{
		newButton(dialogText("gvim.dialog.button.save", "Save"), 0, ChoiceSave),
		newButton(dialogText("gvim.dialog.button.discard", "Discard"), 0, ChoiceDiscard),
		newButton(dialogText("gvim.dialog.button.cancel", "Cancel"), 0, ChoiceCancel),
	}, ov)
	return d
}

// Update handles one message. See Overlay.
func (d *UnsavedChanges) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *UnsavedChanges) Draw(g *grid.Grid) {
	d.drawText(g, dialogTextVars("gvim.dialog.save_changes", "Save changes to %{name}?", map[string]any{"name": d.name}))
}
