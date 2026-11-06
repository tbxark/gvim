package editor

import "github.com/tbxark/gvim/internal/buffer"

// Large caret moves are places to come back to. Fresh records a move of more
// than 50 bytes; gvim has no byte offsets, so a move is large when it crosses at
// least jumpFarLines lines, or more than jumpFarCols columns on one line. The
// arrow keys and word motions stay under that, so they leave no trail.
const (
	jumpFarLines = 2
	jumpFarCols  = 50
)

// jumpMark is the place the caret was in before an action ran.
type jumpMark struct {
	en jumpEntry
	ok bool
}

// markJump records the place of the active caret, to compare after an action.
// The untouched placeholder tab is not a place.
func (e *Editor) markJump() jumpMark {
	d := e.Active()
	if d == nil || e.pristineUntitled() {
		return jumpMark{}
	}
	return jumpMark{en: jumpEntryOf(d), ok: true}
}

// recordJump adds the place from markJump to the navigation history when the
// caret of the same tab has moved far since.
func (e *Editor) recordJump(m jumpMark) {
	d := e.Active()
	if !m.ok || d != m.en.doc || !farMove(m.en.pos, d.Cursor) {
		return
	}
	e.extras.jumps.push(m.en)
}

// farMove reports whether a move from a to b is large enough for the history.
func farMove(a, b buffer.Pos) bool {
	lines := max(a.Line-b.Line, b.Line-a.Line)
	cols := max(a.Col-b.Col, b.Col-a.Col)
	return lines >= jumpFarLines || (lines == 0 && cols > jumpFarCols)
}
