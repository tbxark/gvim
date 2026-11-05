package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/dialog"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// WrapDialog adapts a dialog to the root's Overlay so the app can open a dialog
// without importing bubbletea itself.
func WrapDialog(d dialog.Overlay) Overlay { return wrappedDialog{d: d} }

// wrappedDialog adapts a dialog to the root's Overlay. The dialog answers by
// returning nil from Update, and the adapter passes that on so the stack pops it.
type wrappedDialog struct {
	d dialog.Overlay
}

// Update forwards the message to the dialog. It returns nil once the dialog is
// closed.
func (o wrappedDialog) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	next, cmd := o.d.Update(msg)
	if next == nil {
		return nil, cmd
	}
	return wrappedDialog{d: next}, cmd
}

// Draw paints the dialog over the frame.
func (o wrappedDialog) Draw(g *grid.Grid) { o.d.Draw(g) }

// Active reports whether the dialog is still open.
func (o wrappedDialog) Active() bool { return o.d.Active() }

// Cursor returns the caret of the dialog's focused text field, so the root can
// place the terminal caret there.
func (o wrappedDialog) Cursor(w, h int) (int, int, bool) { return o.d.Cursor(w, h) }
