package editor

import (
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

// Virtual space is the editor's side of document virtual space (see
// document/virtualspace.go): which documents it applies to, and the cursor moves
// that enter and leave it. The buffer itself is never changed by a move.

// VirtualSpaceEnabled reports whether the active document may put its caret past
// the end of a line. It is editor.virtual_space (or the per-buffer toggle when
// the document set one), and it needs line wrap off: a wrapped line has no empty
// region past its end, and the pane draws the caret there only unwrapped.
func (e *Editor) VirtualSpaceEnabled() bool { return e.virtualSpace(e.Active()) }

// virtualSpace reports whether d may put its caret past the end of a line.
// Multi-caret editing is not taught about virtual space, and neither is large file
// mode, so both keep the caret on the text.
func (e *Editor) virtualSpace(d *Document) bool {
	if e == nil || d == nil || len(d.Extra) != 0 || d.LargeState() != nil {
		return false
	}
	if e.SettingsFor(d).LineWrap {
		return false
	}
	if v := d.View().VirtualSpace; v != nil {
		return *v
	}
	return e.opts.VirtualSpace == document.VirtualSpaceOn
}

// VirtualSpaceOn is the global switch as the editor reads it: only "on" puts the
// caret past the end of lines. "block" is accepted by the config and still to be
// implemented for block selections; it keeps the caret on the text for now.
func (e *Editor) VirtualSpaceOn() bool { return e.opts.VirtualSpace == document.VirtualSpaceOn }

// virtualSpaceActions keep the caret's virtual column through Do: the moves and
// edits virtual space teaches, the clipboard actions that do not move the caret,
// and the per-buffer switch itself. Every other action ends the virtual position
// first (see Do).
var virtualSpaceActions = map[Action]bool{
	"move_left": true, "move_right": true, "move_up": true, "move_down": true,
	"select_left": true, "select_right": true, "select_up": true, "select_down": true,
	"insert_char": true, "insert_tab": true, "insert_newline": true,
	"delete_backward": true, "paste": true,
	"copy":                                true,
	"toggle_virtual_space_current_buffer": true,
}

// SetVirtualColumns parks the active document's caret n display columns past the
// end of its line, and returns the value it took. It is refused (and the virtual
// column cleared) when the mode does not allow virtual space for the document.
func (e *Editor) SetVirtualColumns(n int) int {
	d := e.Active()
	if d == nil {
		return 0
	}
	if !e.virtualSpace(d) {
		d.ClearVirtual()
		return 0
	}
	return d.SetVirtualColumns(n)
}

// virtualMoveKind names the plain moves virtual space teaches. Every other move
// clears the virtual column.
type virtualMoveKind int

const (
	virtualMoveNone virtualMoveKind = iota
	virtualMoveLeft
	virtualMoveRight
	virtualMoveUp
	virtualMoveDown
)

// virtualMove handles a plain (not selecting) left, right, up or down move in
// virtual space. It reports whether it took the move; false means the normal
// motion runs.
func (e *Editor) virtualMove(d *Document, kind virtualMoveKind, extend bool) bool {
	if !e.virtualSpace(d) {
		d.ClearVirtual()
		return false
	}
	if extend {
		// Fresh's scoped limit: linear selections stay clamped to the text, so
		// Shift+arrow collapses a virtual caret onto the line end.
		d.ClearVirtual()
		return false
	}
	if d.HasSelection() {
		// A plain move first collapses the selection with the normal motion;
		// the next press may enter virtual space.
		return false
	}
	tab := e.tabSizeOf(d)
	switch kind {
	case virtualMoveRight:
		if d.Cursor.Col < d.Buffer.LineLen(d.Cursor.Line) {
			d.ClearVirtual()
			return false
		}
		d.SetVirtualColumns(d.VirtualColumns() + 1)
		d.Want = -1
		return true
	case virtualMoveLeft:
		if n := d.VirtualColumns(); n > 0 {
			d.SetVirtualColumns(n - 1)
			d.Want = -1
			return true
		}
		d.ClearVirtual()
		return false
	case virtualMoveUp, virtualMoveDown:
		dir := -1
		if kind == virtualMoveDown {
			dir = 1
		}
		line := d.Cursor.Line + dir
		if line < 0 || line >= d.Buffer.Lines() {
			// Vertical virtual lines past the top and bottom of the buffer are
			// not implemented; the caret stays where it is.
			return true
		}
		goal := cellColumn(d.Buffer, d.Cursor, tab) + d.VirtualColumns()
		d.Want = goal
		d.ClearVirtual()
		width := cellColumn(d.Buffer, buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}, tab)
		if goal > width {
			d.Move(buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}, false)
			d.SetVirtualColumns(goal - width)
			return true
		}
		d.Move(buffer.Pos{Line: line, Col: colAtCell(d.Buffer, line, goal, tab)}, false)
		return true
	}
	return false
}
