package editor

import (
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// setRange puts the caret at head and the selection between anchor and head.
// Equal positions clear the selection. The undo group ends, as it does for any
// caret move.
func (d *Document) setRange(anchor, head buffer.Pos) {
	d.want = -1
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
func (d *Document) jumpTo(p buffer.Pos, extend bool) {
	d.want = -1
	d.move(d.Buffer.Clamp(p), extend)
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

// duplicateLines copies the lines that selectedLines covers onto the line below
// them. The caret and selection move with the copy, so they keep their place in
// the text. On the last line, the copy is joined with a new line break.
func (d *Document) duplicateLines() {
	first, last := d.selectedLines()
	block := make([]string, 0, last-first+1)
	for line := first; line <= last; line++ {
		block = append(block, d.Buffer.Line(line))
	}
	text := strings.Join(block, "\n")
	d.Buffer.Group(func() {
		if last+1 < d.Buffer.Lines() {
			d.Buffer.Insert(buffer.Pos{Line: last + 1}, text+"\n")
		} else {
			d.Buffer.Insert(buffer.Pos{Line: last, Col: d.Buffer.LineLen(last)}, "\n"+text)
		}
	})
	n := last - first + 1
	d.Cursor.Line += n
	if d.Sel != nil {
		d.Sel.Anchor.Line += n
		d.Sel.Head = d.Cursor
	}
	d.want = -1
}

// deleteLine removes the caret line with its line break. On the last line the
// text goes but the break before it stays, so the text ends with an empty line.
// Any selection is dropped, and the selection's text is not deleted. The caret
// moves to column 0 of the line that takes the deleted line's place.
func (d *Document) deleteLine() {
	line := d.Cursor.Line
	if line+1 < d.Buffer.Lines() {
		d.Buffer.Delete(buffer.Pos{Line: line}, buffer.Pos{Line: line + 1})
	} else {
		d.Buffer.Delete(buffer.Pos{Line: line}, buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)})
	}
	d.Sel = nil
	d.Cursor = buffer.Pos{Line: line}
	d.want = -1
}

// openLine inserts a line break at the caret and leaves the caret where it was,
// so the text after the caret moves to a new line below. A selection is dropped
// without deleting its text.
func (d *Document) openLine() {
	d.Sel = nil
	d.Buffer.Insert(d.Cursor, "\n")
	d.want = -1
}

// selectLine selects the caret line with its line break, and puts the caret at
// the start of the next line. A last line ends at the end of the document. An
// existing selection is replaced.
func (d *Document) selectLine() {
	start, end := buffer.LineBounds(d.Buffer, d.Cursor.Line)
	d.setRange(start, end)
}

// expandLineSelection grows the selection to whole lines. With no selection it
// selects the caret line. With one, it keeps the lines the selection covers and
// moves the head to the end of the line it is on, so a repeated press takes one
// more line. An upward selection is squared off over the same lines.
func (d *Document) expandLineSelection() {
	low, high := d.Cursor, d.Cursor
	if d.Sel != nil {
		low, high = d.Sel.Ordered()
	}
	start, _ := buffer.LineBounds(d.Buffer, low.Line)
	_, end := buffer.LineBounds(d.Buffer, high.Line)
	d.setRange(start, end)
}

// transposeChars swaps the character before the caret with the one at it, and
// moves the caret past the pair. At the start or end of a line there is no
// character on both sides, so nothing changes. Line breaks are never swapped.
// A selection is dropped.
func (d *Document) transposeChars() {
	p := d.Cursor
	if p.Col == 0 || p.Col >= d.Buffer.LineLen(p.Line) {
		return
	}
	from := buffer.Pos{Line: p.Line, Col: p.Col - 1}
	to := buffer.Pos{Line: p.Line, Col: p.Col + 1}
	pair := []rune(d.Buffer.Range(from, to))
	swapped := string(pair[1]) + string(pair[0])
	d.Buffer.Group(func() {
		d.Buffer.Delete(from, to)
		d.Buffer.Insert(from, swapped)
	})
	d.Cursor = buffer.Pos{Line: p.Line, Col: p.Col + 1}
	d.Sel = nil
	d.want = -1
}

// transformCase converts the selection, or else the word at or just before the
// caret, with fn. The caret ends after the converted text and any selection is
// dropped. Text that does not change leaves the caret and selection alone.
func (d *Document) transformCase(fn func(string) string) {
	from, to, ok := d.caseTarget()
	if !ok {
		return
	}
	old := d.Buffer.Range(from, to)
	converted := fn(old)
	if converted == old {
		return
	}
	var end buffer.Pos
	d.Buffer.Group(func() {
		d.Buffer.Delete(from, to)
		end = d.Buffer.Insert(from, converted)
	})
	d.Cursor = end
	d.Sel = nil
	d.want = -1
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
