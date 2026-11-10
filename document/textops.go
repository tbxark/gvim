package document

import (
	"github.com/tbxark/gvim/buffer"
)

// setRange puts the caret at head and the selection between anchor and head.
// Equal positions clear the selection. The undo group ends, as it does for any
// caret move.
func (d *Document) SetRange(anchor, head buffer.Pos) {
	d.Want = -1
	d.Buffer.Break()
	d.Cursor = head
	d.Sel = nil
	if anchor != head {
		d.Sel = &buffer.Selection{Anchor: anchor, Head: head}
	}
}

// jumpTo places the caret at p, clamped into the document, and resets the
// wanted column. With extend set the selection grows from its anchor. A
// selection that ends up empty is cleared.
func (d *Document) JumpTo(p buffer.Pos, extend bool) {
	d.Want = -1
	d.Move(d.Buffer.Clamp(p), extend)
	if d.Sel != nil && d.Sel.Anchor == d.Sel.Head {
		d.Sel = nil
	}
}

// selectedLines returns the first and last line that the caret or the
// selection covers. A selection that ends at column 0 leaves its last line out.
// Without a selection both are the caret line.
func (d *Document) selectedLines() (first, last int) {
	if !d.HasSelection() {
		return d.Cursor.Line, d.Cursor.Line
	}
	from, to := d.Sel.Ordered()
	last = to.Line
	if to.Col == 0 && to.Line > from.Line {
		last--
	}
	return from.Line, last
}

// openLine inserts a line break at the caret and leaves the caret where it was,
// so the text after the caret moves to a new line below. A selection is dropped
// without deleting its text.
func (d *Document) OpenLine() {
	d.Sel = nil
	d.Buffer.Insert(d.Cursor, "\n")
	d.Want = -1
}

// selectLine selects the caret line with its line break, and puts the caret at
// the start of the next line. A last line ends at the end of the document. An
// existing selection is replaced.
func (d *Document) SelectLine() {
	start, end := buffer.LineBounds(d.Buffer, d.Cursor.Line)
	d.SetRange(start, end)
}

// expandLineSelection grows the selection to whole lines. With no selection it
// selects the caret line. With one, it keeps the lines the selection covers and
// moves the head to the end of the line it is on, so a repeated press takes one
// more line. An upward selection is squared off over the same lines. With extra
// carets, each caret's line selection grows the same way, as Fresh's
// expand_line_selection does; a visual block keeps its primary-caret behavior.
func (d *Document) ExpandLineSelection() {
	if _, block := d.BlockSelection(); !block {
		if cs := d.AllCarets(); len(cs) > 1 {
			grown := make([]caret, len(cs))
			for i, c := range cs {
				low, high := c.Bounds()
				start, _ := buffer.LineBounds(d.Buffer, low.Line)
				_, end := buffer.LineBounds(d.Buffer, high.Line)
				grown[i] = caret{Anchor: start, Head: end, Primary: c.Primary, Want: c.Want, Pad: c.Pad}
			}
			d.SetCarets(grown)
			return
		}
	}
	low, high := d.Cursor, d.Cursor
	if d.Sel != nil {
		low, high = d.Sel.Ordered()
	}
	start, _ := buffer.LineBounds(d.Buffer, low.Line)
	_, end := buffer.LineBounds(d.Buffer, high.Line)
	d.SetRange(start, end)
}

// caseTarget returns the range that a case conversion changes: the selection,
// or the word the caret is in or directly after. ok is false when there is
// neither.
func (d *Document) caseTarget() (from, to buffer.Pos, ok bool) {
	if d.HasSelection() {
		from, to = d.Sel.Ordered()
		return from, to, true
	}
	p := d.Cursor
	if d.Buffer.ClassAt(p) != buffer.ClassWord {
		prev, has := prevPos(d.Buffer, p)
		if !has || d.Buffer.ClassAt(prev) != buffer.ClassWord {
			return from, to, false
		}
		p = prev
	}
	from, to = buffer.WordBounds(d.Buffer, p)
	return from, to, true
}
