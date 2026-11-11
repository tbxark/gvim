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
// Stacking. An overlay that implements Exclusive is a transient interaction that
// owns the keyboard alone, so the root closes the Exclusive overlays below one it
// opens (see Exclusive). Every other overlay stacks: a dialog or a prompt that
// needs an answer may cover the palette or the find bar, and closing it returns to
// the layer under it. An Alert (see Alert) keeps the input: a new Exclusive overlay
// opens under the Alert, not over it, so a background or plugin request cannot
// bypass the question being asked.
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

// Exclusive is the optional part of an Overlay that is a transient interaction:
// the palette, the find bar, the browser, a picker list. Only one Exclusive overlay
// is open at a time. The root closes the Exclusive overlays below one it opens,
// running each one's cancel path and its focus restoration, and keeps the layers
// that stack: dialogs and prompts that need an answer (the alerts), menus, popups,
// information boxes and the overlays plugins open. An overlay that does not
// implement Exclusive keeps the ordinary stack behaviour.
type Exclusive interface {
	// ExclusiveOverlay marks the overlay as a transient interaction that owns the
	// keyboard alone.
	ExclusiveOverlay()
}

// Alert is the optional part of an Overlay that must keep the input until it is
// answered: a dialog, a prompt or a confirmation. A new Exclusive overlay opens
// under the bottom-most Alert instead of on top of the stack, so a request that
// arrives while an alert is up cannot take the keyboard from it. Alerts still stack
// over each other and over the ordinary overlays.
type Alert interface {
	// AlertOverlay marks the overlay as needing an answer before the layers under
	// it get the input again.
	AlertOverlay()
}

// Caret is the optional part of an Overlay that owns the terminal caret: a
// dialog's text field, a prompt, or a list's filter row. The root asserts it on
// the top overlay and places the terminal caret where it says.
type Caret interface {
	// Cursor returns the caret position in a frame of w by h cells. ok is false
	// when the overlay has no caret to show.
	Cursor(w, h int) (x, y int, ok bool)
}
