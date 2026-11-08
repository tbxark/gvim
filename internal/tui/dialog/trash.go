package dialog

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// ConfirmTrash asks before a file or folder moves to the Trash. The buttons are
// Move to Trash and Cancel. Cancel starts with the focus, so a stray Enter does
// not delete. The keys are t and c, and Esc cancels.
type ConfirmTrash struct {
	modal
	name string
}

var _ Overlay = (*ConfirmTrash)(nil)

// NewConfirmTrash returns the dialog that asks about the item name. Tag comes
// back in the Result.
func NewConfirmTrash(clock Clock, name string, tag any, ov style.Overlay) *ConfirmTrash {
	d := &ConfirmTrash{name: name}
	d.setup(clock, dialogText("gvim.dialog.title.confirm_delete", "Confirm Delete"), tag, ChoiceCancel, []button{
		newButton(dialogText("gvim.dialog.button.move_to_trash", "Move to Trash"), 8, ChoiceMoveToTrash),
		newButton(dialogText("gvim.dialog.button.cancel", "Cancel"), 0, ChoiceCancel),
	}, ov)
	d.focus = 1
	return d
}

// Update handles one message. See Overlay.
func (d *ConfirmTrash) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *ConfirmTrash) Draw(g *grid.Grid) {
	d.drawText(g, dialogTextVars("gvim.dialog.trash_confirm", "Move \"%{name}\" to Trash?", map[string]any{"name": d.name}))
}
