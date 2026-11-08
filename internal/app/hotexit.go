package app

import (
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
)

// hotExitEditor is the root's editor port with the hot-exit quit policy. The
// root asks the editor for the tabs a quit would lose, one by one; with
// editor.hot_exit on, a session launch keeps them instead (the session save at
// exit stashes their text), so the ask would be about work that is about to be
// kept. A forced quit still passes through, and with the setting off the
// editor's own answer is returned unchanged.
//
// This wrapper is a temporary seam. It overrides only Quit and leaves every
// other method of *uiadapter.Editor promoted, so the optional ports the root
// probes and the root's tab surface still reach the adapter. A later round
// should move the hot-exit flag into the quit path itself, in the editor or the
// TUI, once the display work settles, and retire this type.
type hotExitEditor struct {
	*uiadapter.Editor
	hot bool
}

// The wrapper is still the root's editor port.
var _ tui.EditorState = hotExitEditor{}

// Quit implements the root's tab surface. With hot exit on and no force, the
// quit is granted (nil) and the modified tabs stay modified, so the session save
// at exit stashes their text. Otherwise the editor's answer — the tabs the root
// asks about — is returned unchanged.
func (e hotExitEditor) Quit(force bool) []editor.Dirty {
	if e.hot && !force {
		return e.Editor.Quit(true)
	}
	return e.Editor.Quit(force)
}
