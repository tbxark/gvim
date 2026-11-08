package editor

import (
	"errors"

	"github.com/tbxark/gvim/buffer"
)

// ErrNothingSelected is returned by copy and cut when there is no selection.
var ErrNothingSelected = errors.New("nothing selected")

// insertText types s at the cursor, replacing any selection first.
func (d *Document) insertText(s string) {
	d.deleteSelection()
	if d.editRefused() {
		// The selection could not be deleted, so the text is not typed over it.
		return
	}
	d.Cursor = d.Buffer.Insert(d.Cursor, s)
	d.want = -1
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

// selectAll selects the whole document, leaving the cursor at the end.
func (d *Document) selectAll() {
	end := d.Buffer.End()
	d.Sel = &buffer.Selection{Anchor: buffer.Pos{}, Head: end}
	d.Cursor = end
}

// selectWord selects the word the cursor is in or directly after. With no word
// at either place it selects nothing and leaves the cursor in place.
func (d *Document) selectWord() {
	from, to, ok := d.wordAt(d.Cursor)
	if !ok {
		return
	}
	d.Sel = &buffer.Selection{Anchor: from, Head: to}
	d.Cursor = to
}
