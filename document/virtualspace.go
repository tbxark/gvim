package document

import (
	"strings"

	"github.com/tbxark/gvim/buffer"
)

// Virtual space: with editor.virtual_space on, the caret may sit past the end of
// its line. The buffer is never changed by moving there: the caret keeps its
// byte-clamped position (at the end of the line) and a visual column past the end
// is kept beside it. Typing materializes the gap as spaces, in the same undo step
// as the text typed, so the buffer only changes when an edit does.
//
// The virtual column is display columns, not runes: it is measured from the
// line's last cell, so a tab or a wide character before it does not move it.

// MaxVirtualColumns bounds the virtual column the caret can park at. It matches
// the bound of a view column, so a horizontal scroll cannot run away; a caret
// further out would be off every practical terminal anyway.
const MaxVirtualColumns = MaxViewColumn

// VirtualColumns returns how many display columns the primary caret sits past the
// end of its line, and 0 when it is on text. A caret with a selection is never in
// virtual space (selections stay clamped to the text), and neither is one that is
// not at the end of its line.
func (d *Document) VirtualColumns() int {
	if d == nil || d.virtual <= 0 || d.HasSelection() {
		return 0
	}
	if d.Cursor.Col != d.Buffer.LineLen(d.Cursor.Line) {
		return 0
	}
	return d.virtual
}

// SetVirtualColumns parks the caret n display columns past the end of its line
// and returns the value it took. It only takes a positive column for a collapsed
// caret that is at the end of its line; any other position clears the virtual
// column. Zero clears it.
func (d *Document) SetVirtualColumns(n int) int {
	if d == nil || n <= 0 || d.HasSelection() || d.Cursor.Col != d.Buffer.LineLen(d.Cursor.Line) {
		if d != nil {
			d.virtual = 0
		}
		return 0
	}
	d.virtual = min(n, MaxVirtualColumns)
	return d.virtual
}

// ClearVirtual ends the virtual position: the caret stays where it is on the
// text. A move or an edit that is not taught about virtual space calls it.
func (d *Document) ClearVirtual() {
	if d != nil {
		d.virtual = 0
	}
}

// MaterializeVirtual turns the virtual gap into real spaces, leaving the caret
// after the padding, and returns how many were inserted. It is the first step of
// an edit at a virtual caret, so the edit and its padding are one change to the
// text and one undo step. A caret that is not in virtual space, or a refused
// edit, inserts nothing and returns 0.
func (d *Document) MaterializeVirtual() int {
	n := d.VirtualColumns()
	if n <= 0 {
		return 0
	}
	d.virtual = 0
	if d.editRefused() {
		return 0
	}
	end := buffer.Pos{Line: d.Cursor.Line, Col: d.Buffer.LineLen(d.Cursor.Line)}
	d.Cursor = d.Buffer.Insert(end, strings.Repeat(" ", n))
	d.Want = -1
	d.Seen = d.Cursor
	return n
}

// VirtualLine is the line the caret is on, which its virtual column is measured
// from.
func (d *Document) VirtualLine() int { return d.Cursor.Line }
