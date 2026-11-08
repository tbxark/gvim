package app

import (
	"context"
	"errors"

	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
)

// largeWindows is the app's windowed large-file port for the TUI. The active document
// may be a remote large file opened read-only over an asynchronous window source;
// the root loads the window its paint missed here, in a command, never on the update
// loop. A local document reports AsyncLarge false, so the root arms no polling.
type largeWindows struct {
	ed *editor.Editor
}

// newLargeWindows returns the port over the editor, or nil without one.
func newLargeWindows(ed *editor.Editor) tui.LargeWindowLoader {
	if ed == nil {
		return nil
	}
	return largeWindows{ed: ed}
}

// AsyncLarge reports whether the active document is a remote windowed large file.
func (l largeWindows) AsyncLarge() bool {
	d := l.ed.Active()
	return d != nil && d.AsyncLarge()
}

// PendingLargeWindow returns the window the last paint missed.
func (l largeWindows) PendingLargeWindow() (tui.LargeWindow, bool) {
	d := l.ed.Active()
	if d == nil || !d.AsyncLarge() {
		return tui.LargeWindow{}, false
	}
	w, ok := d.TakePendingLargeWindow()
	if !ok {
		return tui.LargeWindow{}, false
	}
	return tui.LargeWindow{Path: w.Path, Off: w.Off, N: w.N}, true
}

// LoadLargeWindow loads one window, blocking in the caller's command. The path must
// still be the active document: a load cannot fill a window of another document.
func (l largeWindows) LoadLargeWindow(ctx context.Context, w tui.LargeWindow) error {
	d := l.ed.Active()
	if d == nil || d.Path != w.Path {
		return errors.New("remote large file: the document is no longer active")
	}
	return d.LoadLargeWindow(ctx, document.LargeWindow{Path: w.Path, Off: w.Off, N: w.N})
}
