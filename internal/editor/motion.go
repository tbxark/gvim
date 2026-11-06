package editor

import (
	"unicode"

	"github.com/tbxark/gvim/internal/buffer"
)

// motion computes where the cursor goes next. A motion never changes the
// document.
type motion func(e *Editor, d *Document, extend bool) buffer.Pos

// prevPos returns the position one character (grapheme cluster) before p. A
// line break counts as one step, landing at the end of the previous line.
func prevPos(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col > 0 {
		return buffer.Pos{Line: p.Line, Col: prevBoundary(b, p.Line, p.Col)}, true
	}
	if p.Line > 0 {
		return buffer.Pos{Line: p.Line - 1, Col: b.LineLen(p.Line - 1)}, true
	}
	return p, false
}

// nextPos returns the position one character (grapheme cluster) after p. A
// line break counts as one step, landing at the start of the next line.
func nextPos(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col < b.LineLen(p.Line) {
		return buffer.Pos{Line: p.Line, Col: nextBoundary(b, p.Line, p.Col)}, true
	}
	if p.Line+1 < b.Lines() {
		return buffer.Pos{Line: p.Line + 1, Col: 0}, true
	}
	return p, false
}

// wordLeft moves back to the start of the previous word, skipping spaces first.
func wordLeft(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	for {
		prev, ok := prevPos(b, q)
		if !ok {
			return q
		}
		if b.ClassAt(prev) != buffer.ClassSpace {
			break
		}
		q = prev
	}
	cls := b.ClassAt(mustPrev(b, q))
	for {
		prev, ok := prevPos(b, q)
		if !ok || b.ClassAt(prev) != cls {
			return q
		}
		q = prev
	}
}

// wordRight moves forward to the start of the next word.
func wordRight(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	if cls := b.ClassAt(q); cls != buffer.ClassSpace {
		for b.ClassAt(q) == cls {
			next, ok := nextPos(b, q)
			if !ok {
				// Word runs to the end of the document.
				return buffer.Pos{Line: q.Line, Col: q.Col + 1}
			}
			q = next
		}
	}
	for b.ClassAt(q) == buffer.ClassSpace {
		next, ok := nextPos(b, q)
		if !ok {
			break
		}
		q = next
	}
	return q
}

// wordEnd moves forward to just after the end of the word, or the punctuation
// run, under p. From blanks it skips them (and line breaks), then takes the
// word or punctuation run it lands on.
func wordEnd(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	cls := b.ClassAt(q)
	if cls == buffer.ClassSpace {
		for b.ClassAt(q) == buffer.ClassSpace {
			next, ok := nextPos(b, q)
			if !ok {
				return q
			}
			q = next
		}
		cls = b.ClassAt(q)
	}
	for b.ClassAt(q) == cls {
		next, ok := nextPos(b, q)
		if !ok {
			break
		}
		q = next
	}
	return q
}

// mustPrev returns the position before p. The caller has already checked that
// one exists, so a missing position falls back to p itself.
func mustPrev(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	prev, _ := prevPos(b, p)
	return prev
}

// firstNonBlank returns the column of the first non-space rune on line.
func firstNonBlank(b *buffer.Buffer, line int) int {
	for col := range b.LineLen(line) {
		if r, _ := b.RuneAt(buffer.Pos{Line: line, Col: col}); !unicode.IsSpace(r) {
			return col
		}
	}
	return b.LineLen(line)
}

// motions for the cursor, keyed by the action name the keymap uses. extend is
// true when the move grows a selection (or mark mode is on); a plain move with a
// selection collapses it to the edge instead, as Fresh does.
var (
	motionLeft = func(_ *Editor, d *Document, extend bool) buffer.Pos {
		if !extend && d.HasSelection() {
			from, _ := d.Sel.Ordered()
			return from
		}
		if p, ok := prevPos(d.Buffer, d.Cursor); ok {
			return p
		}
		return d.Cursor
	}
	motionRight = func(_ *Editor, d *Document, extend bool) buffer.Pos {
		if !extend && d.HasSelection() {
			_, to := d.Sel.Ordered()
			return to
		}
		if p, ok := nextPos(d.Buffer, d.Cursor); ok {
			return p
		}
		return d.Cursor
	}
	// Up and Down start from the near edge of a collapsing selection, and on the
	// first or last line go to the start or end of the document.
	motionUp = func(e *Editor, d *Document, extend bool) buffer.Pos {
		from := d.Cursor
		if !extend && d.HasSelection() {
			from, _ = d.Sel.Ordered()
		}
		if to, ok := e.visualVertical(d, from, -1); ok {
			return to
		}
		if from.Line == 0 {
			e.wantFrom(d, from)
			return buffer.Pos{}
		}
		return e.verticalTarget(d, from, from.Line-1)
	}
	motionDown = func(e *Editor, d *Document, extend bool) buffer.Pos {
		from := d.Cursor
		if !extend && d.HasSelection() {
			_, from = d.Sel.Ordered()
		}
		if to, ok := e.visualVertical(d, from, 1); ok {
			return to
		}
		if from.Line == d.Buffer.Lines()-1 {
			e.wantFrom(d, from)
			return d.Buffer.End()
		}
		return e.verticalTarget(d, from, from.Line+1)
	}
	motionWordLeft = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		return wordLeft(d.Buffer, d.Cursor)
	}
	motionWordRight = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		return wordRight(d.Buffer, d.Cursor)
	}
	motionWordEnd = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		return wordEnd(d.Buffer, d.Cursor)
	}
	motionLineStart = func(e *Editor, d *Document, _ bool) buffer.Pos {
		if p, ok := e.visualLineStart(d.Cursor); ok {
			return p
		}
		return buffer.Pos{Line: d.Cursor.Line}
	}
	motionLineEnd = func(e *Editor, d *Document, _ bool) buffer.Pos {
		if p, ok := e.visualLineEnd(d.Buffer, d.Cursor); ok {
			return p
		}
		return buffer.Pos{Line: d.Cursor.Line, Col: d.Buffer.LineLen(d.Cursor.Line)}
	}
	// On a wrapped line, Home first goes to the start of the caret's row. On the
	// first row it toggles between the first non-blank and the line start.
	motionSmartHome = func(e *Editor, d *Document, _ bool) buffer.Pos {
		if starts := e.rowStarts(d.Cursor.Line); starts != nil && rowOf(starts, d.Cursor.Col) > 0 {
			p, _ := e.visualLineStart(d.Cursor)
			return p
		}
		first := firstNonBlank(d.Buffer, d.Cursor.Line)
		if d.Cursor.Col == first {
			return buffer.Pos{Line: d.Cursor.Line}
		}
		return buffer.Pos{Line: d.Cursor.Line, Col: first}
	}
	motionDocStart = func(*Editor, *Document, bool) buffer.Pos { return buffer.Pos{} }
	motionDocEnd   = func(_ *Editor, d *Document, _ bool) buffer.Pos { return d.Buffer.End() }
)

// wantFrom sets the wanted column from p when it is unset. The next vertical
// move then aims at the column p is in.
func (e *Editor) wantFrom(d *Document, p buffer.Pos) {
	if d.want < 0 {
		d.want = cellColumn(d.Buffer, p, e.tabSizeOf(d))
	}
}

// verticalTarget moves from p to line, aiming at the wanted column. The column
// counts display cells, so a tab or a wide character keeps the caret aligned on
// the screen. The column is taken from p when it is unset.
func (e *Editor) verticalTarget(d *Document, from buffer.Pos, line int) buffer.Pos {
	e.wantFrom(d, from)
	line = min(max(line, 0), d.Buffer.Lines()-1)
	return buffer.Pos{Line: line, Col: colAtCell(d.Buffer, line, d.want, e.tabSizeOf(d))}
}
