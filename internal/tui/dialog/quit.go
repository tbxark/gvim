package dialog

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// ConfirmQuit asks before gvim quits when editor.confirm_quit is on and no tab has
// unsaved changes. The buttons are Quit and Cancel. Cancel starts with the focus, so a
// stray Enter does not quit. The keys are q and c, and Esc cancels.
type ConfirmQuit struct {
	modal
}

var _ Overlay = (*ConfirmQuit)(nil)

// NewConfirmQuit returns the dialog. Tag comes back in the Result.
func NewConfirmQuit(clock Clock, tag any, ov style.Overlay) *ConfirmQuit {
	d := &ConfirmQuit{}
	d.setup(clock, dialogText("gvim.dialog.title.confirm_quit", "Confirm Quit"), tag, ChoiceCancel, []button{
		newButton(dialogText("gvim.dialog.button.quit", "Quit"), 0, ChoiceQuit),
		newButton(dialogText("gvim.dialog.button.cancel", "Cancel"), 0, ChoiceCancel),
	}, ov)
	d.focus = 1
	return d
}

// TakesEscape tells the root to pass Esc to the dialog, which answers Cancel as a result,
// so the root can say that the quit was cancelled.
func (d *ConfirmQuit) TakesEscape() bool { return true }

// Update handles one message. See Overlay.
func (d *ConfirmQuit) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *ConfirmQuit) Draw(g *grid.Grid) {
	d.drawText(g, dialogText("gvim.dialog.quit_confirm", "Quit gvim?"))
}
