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

// MaxVirtualLines bounds how many empty rows below the last line the caret can
// park on. The view clamps the caret to its bottom row rather than scrolling past
// the text, as Fresh's does, so deeper lines would never be visible; the bound
// keeps the count from growing without end.
const MaxVirtualLines = 10000

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
// column. Zero clears it. A horizontal virtual position ends any virtual lines:
// the caret is on a real line again.
func (d *Document) SetVirtualColumns(n int) int {
	if d != nil && n > 0 {
		d.virtualLines = 0
	}
	if d == nil || n <= 0 || d.HasSelection() || d.Cursor.Col != d.Buffer.LineLen(d.Cursor.Line) {
		if d != nil {
			d.virtual = 0
		}
		return 0
	}
	d.virtual = min(n, MaxVirtualColumns)
	return d.virtual
}

// VirtualLines returns how many empty rows below the last line the caret sits
// on, and 0 when it is on text. It is meaningful only while the caret is at the
// buffer end with no selection, so any other position reports 0; see
// SetVirtualLines.
func (d *Document) VirtualLines() int {
	if d == nil || d.virtualLines <= 0 || d.HasSelection() || len(d.Extra) > 0 {
		return 0
	}
	if d.Cursor != d.Buffer.End() {
		return 0
	}
	return d.virtualLines
}

// VirtualLineColumn is the display column of a caret on a virtual line, and 0
// when the caret is on text. The virtual lines are empty, so the column is the
// whole wanted column, spaces from the line's start.
func (d *Document) VirtualLineColumn() int {
	if d.VirtualLines() <= 0 {
		return 0
	}
	return max(d.Want, 0)
}

// SetVirtualLines parks the caret lines empty rows below the last line at display
// column col, and returns the count it took. It is refused, and the virtual lines
// cleared, for a caret that has a selection or is not alone. Zero clears them.
// The byte position stays at the buffer end; the rows are a view and editing
// concept, and an edit materializes them as line breaks plus spaces.
func (d *Document) SetVirtualLines(lines, col int) int {
	if d == nil {
		return 0
	}
	if lines <= 0 || d.HasSelection() || len(d.Extra) > 0 {
		d.virtualLines = 0
		return 0
	}
	d.Cursor = d.Buffer.End()
	d.Sel = nil
	d.virtual = 0
	d.virtualLines = min(lines, MaxVirtualLines)
	d.Want = min(max(col, 0), MaxVirtualColumns)
	d.Seen = d.Cursor
	return d.virtualLines
}

// ClearVirtualLines ends a virtual-line position but keeps a horizontal virtual
// column: the caret stays at the buffer end.
func (d *Document) ClearVirtualLines() {
	if d != nil {
		d.virtualLines = 0
	}
}

// ClearVirtual ends the virtual position: the caret stays where it is on the
// text. A move or an edit that is not taught about virtual space calls it.
func (d *Document) ClearVirtual() {
	if d != nil {
		d.virtual = 0
		d.virtualLines = 0
	}
}

// MaterializeVirtual turns the virtual gap into real spaces, leaving the caret
// after the padding, and returns how many were inserted. It is the first step of
// an edit at a virtual caret, so the edit and its padding are one change to the
// text and one undo step. A caret that is not in virtual space, or a refused
// edit, inserts nothing and returns 0.
func (d *Document) MaterializeVirtual() int {
	if n := d.VirtualLines(); n > 0 {
		// A caret on a virtual line pads with the line breaks down to it, then
		// the spaces out to its column, all as one edit: the virtual lines are
		// empty, so the column is the whole wanted column.
		gap := strings.Repeat("\n", n) + strings.Repeat(" ", max(d.Want, 0))
		d.virtual, d.virtualLines = 0, 0
		if d.editRefused() {
			return 0
		}
		d.Cursor = d.Buffer.Insert(d.Buffer.End(), gap)
		d.Want = -1
		d.Seen = d.Cursor
		return len(gap)
	}
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
