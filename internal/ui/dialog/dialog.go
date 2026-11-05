// Package dialog holds the modal dialogs of gvim: Unsaved Changes, Save As with
// its overwrite check, Confirm Delete (move to Trash), Open Folder, and the
// error message. Each dialog is an overlay that the root stacks above the frame.
//
// A dialog never acts on the editor or the file system. When it ends, it
// returns a Result as a command, and the root applies the answer. The opener
// passes a Tag, so one root handler can serve several dialogs. The checks that
// need the file system (does a target exist, is a folder valid) are functions
// the opener passes in.
//
// Keys. Tab and Shift+Tab move the focus through the text field, the list and
// the buttons. Left and Right move it between buttons. Enter activates the
// focused button; on the field it activates the first button, and on the list
// it opens the selected entry. A letter activates the button whose underlined
// letter it is, case-insensitively. A dialog with a text field types letters
// into the field, so its buttons answer to Alt plus the letter instead. Esc
// ends the dialog with its cancel answer. The root normally pops a dialog on
// Esc without forwarding it, so the root should treat a popped dialog as a
// Cancel.
//
// Grace period. Enter is ignored for 425 ms after the dialog opens. Each Enter
// that is ignored extends the window by 425 ms, up to 1500 ms after opening.
// Time comes from an injected Clock.
//
// Mouse. A left click on a button activates it. A click on a list entry selects
// it, and a click on the field focuses it. A click anywhere else does nothing.
package dialog

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// Overlay is the shape the root's overlay stack expects, minus the root's own
// type. The root wraps each dialog in a small adapter whose Update returns the
// root's Overlay type. The methods here are the same as the root's Overlay,
// with Cursor added for the caret of a text field.
type Overlay interface {
	// Update handles one key or mouse message. It returns the overlay that takes
	// this one's place: the dialog itself while it is open, and nil once it has
	// answered. The command reports the Result.
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	// Draw paints the dialog centered in g. Only the cells of the box change.
	Draw(g *grid.Grid)
	// Cursor returns the caret of the focused text field in a frame of w by h
	// cells. ok is false when no text field has the focus.
	Cursor(w, h int) (x, y int, ok bool)
	// Active reports whether the dialog is still open.
	Active() bool
}

// Choice is the answer a dialog gives.
type Choice int

// The answers. ChoiceCancel is the zero value, so an unset Choice is a cancel.
const (
	ChoiceCancel      Choice = iota // Cancel, Esc, or a dismissed Save As overwrite
	ChoiceSave                      // Unsaved Changes: save the buffer; Save As: save to Result.Text
	ChoiceDiscard                   // Unsaved Changes: drop the changes
	ChoiceReplace                   // Save As: replace the existing file at Result.Text
	ChoiceMoveToTrash               // Confirm Delete: move the item to the Trash
	ChoiceOpen                      // Open Folder: open the folder at Result.Text
	ChoiceOK                        // Error: acknowledged
)

// String names the choice, for logs and tests.
func (c Choice) String() string {
	switch c {
	case ChoiceCancel:
		return "cancel"
	case ChoiceSave:
		return "save"
	case ChoiceDiscard:
		return "discard"
	case ChoiceReplace:
		return "replace"
	case ChoiceMoveToTrash:
		return "move-to-trash"
	case ChoiceOpen:
		return "open"
	case ChoiceOK:
		return "ok"
	}
	return "unknown"
}

// Result is the message a dialog emits as a command when it answers. Tag is
// the value the opener passed in. Text is the path of Save As (the name to save
// to, or to replace) and of Open Folder (the folder to open). It is empty for
// the other dialogs.
type Result struct {
	Tag    any
	Choice Choice
	Text   string
}

// Clock returns the current time. The grace period reads it. Nil means
// time.Now.
type Clock func() time.Time

// PathChecks are the file system questions a path dialog asks the opener. Both
// fields are optional. The dialog does not look at the disk itself.
type PathChecks struct {
	// Exists reports whether a file is already at path. Save As then asks before
	// it replaces the file. Nil means that no file exists.
	Exists func(path string) bool
	// Validate returns why path cannot be used, such as a missing parent folder.
	// The message is shown inline and the dialog stays open. Nil accepts any
	// non-empty path.
	Validate func(path string) error
}

// result returns the command that reports r.
func result(r Result) tea.Cmd {
	return func() tea.Msg { return r }
}
