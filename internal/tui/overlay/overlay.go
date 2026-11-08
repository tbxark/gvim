// Package overlay defines the contract of the layers that the root model stacks
// over the frame: the palette, the find bar, the prompts, the browser, the
// dialogs, the lists and the menus.
//
// Routing. A key or mouse message goes to the top overlay first, and the root
// does not handle it further. Other messages, such as the palette's file ranking
// results, also go to the top overlay, and the root handles them as usual. Esc
// is not forwarded: it pops the top overlay, so every overlay is cancelled by
// Esc. After an Update, the root replaces the top overlay with the one Update
// returned, or pops it when Update returns nil. It also pops any overlay that
// reports Active false, so an overlay that is done can say so from Update or
// from its state.
//
// Painting. Every active overlay is drawn after the frame, bottom of the stack
// first. An overlay covers only the cells it draws. The cells it does not draw
// keep the frame under them.
//
// This package is a leaf: it imports only the grid and bubbletea, so a
// sub-package can implement the contract without importing the root model.
package overlay

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// Overlay is a layer drawn over the frame and given the input while it is on
// top. The root keeps a stack of overlays.
type Overlay interface {
	// Update handles one key or mouse message. It returns the overlay that
	// takes this one's place (the same overlay to keep it, nil to close it), and
	// the command the message starts.
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	// Draw paints the overlay over the frame in g. The grid's size is the frame
	// size.
	Draw(g *grid.Grid)
	// Active reports whether the overlay is still open.
	Active() bool
}

// Caret is the optional part of an Overlay that owns the terminal caret: a
// dialog's text field, a prompt, or a list's filter row. The root asserts it on
// the top overlay and places the terminal caret where it says.
type Caret interface {
	// Cursor returns the caret position in a frame of w by h cells. ok is false
	// when the overlay has no caret to show.
	Cursor(w, h int) (x, y int, ok bool)
}
