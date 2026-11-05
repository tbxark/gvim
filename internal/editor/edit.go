package editor

import (
	"errors"

	"github.com/tbxark/gvim/internal/buffer"
)

// ErrNothingSelected is returned by copy and cut when there is no selection.
var ErrNothingSelected = errors.New("nothing selected")

// insertText types s at the cursor, replacing any selection first.
func (d *Document) insertText(s string) {
	d.deleteSelection()
	d.Cursor = d.Buffer.Insert(d.Cursor, s)
	d.want = -1
}

// deleteBackward removes the selection, or the character before the cursor,
// or the line break before the cursor.
func (d *Document) deleteBackward() {
	if d.deleteSelection() {
		return
	}
	prev, ok := prevPos(d.Buffer, d.Cursor)
	if !ok {
		return
	}
	d.Buffer.Delete(prev, d.Cursor)
	d.Cursor = prev
}

// deleteForward removes the selection, or the character after the cursor, or
// the line break after the cursor.
func (d *Document) deleteForward() {
	if d.deleteSelection() {
		return
	}
	next, ok := nextPos(d.Buffer, d.Cursor)
	if !ok {
		return
	}
	d.Buffer.Delete(d.Cursor, next)
}

// deleteTo removes the text between the cursor and target, whichever comes first.
func (d *Document) deleteTo(target buffer.Pos) {
	if d.deleteSelection() {
		return
	}
	d.Buffer.Delete(d.Cursor, target)
	if target.Less(d.Cursor) {
		d.Cursor = target
	}
}

// moveLine swaps the cursor's line with the line at neighbor (-1 or +1).
// Both lines are rewritten, so the swap takes two undo steps.
func (d *Document) moveLine(neighbor int) bool {
	line := d.Cursor.Line
	other := line + neighbor
	if other < 0 || other >= d.Buffer.Lines() {
		return false
	}
	top := min(line, other)
	bottom := max(line, other)
	a, b := d.Buffer.Line(top), d.Buffer.Line(bottom)
	d.Buffer.Delete(buffer.Pos{Line: top}, buffer.Pos{Line: bottom, Col: d.Buffer.LineLen(bottom)})
	d.Buffer.Insert(buffer.Pos{Line: top}, b+"\n"+a)
	d.Cursor.Line = other
	d.Cursor.Col = min(d.Cursor.Col, d.Buffer.LineLen(other))
	d.Sel = nil
	return true
}

// copySelection returns the selected text.
func (d *Document) copySelection() (string, error) {
	if !d.HasSelection() {
		return "", ErrNothingSelected
	}
	from, to := d.Sel.Ordered()
	return d.Buffer.Range(from, to), nil
}

// selectAll selects the whole document, leaving the cursor at the end.
func (d *Document) selectAll() {
	end := d.Buffer.End()
	d.Sel = &buffer.Selection{Anchor: buffer.Pos{}, Head: end}
	d.Cursor = end
}

// selectWord selects the word under the cursor. On a non-word character it
// selects nothing and leaves the cursor in place.
func (d *Document) selectWord() {
	if d.Buffer.ClassAt(d.Cursor) != buffer.ClassWord {
		return
	}
	from, to := buffer.WordBounds(d.Buffer, d.Cursor)
	d.Sel = &buffer.Selection{Anchor: from, Head: to}
	d.Cursor = to
}
