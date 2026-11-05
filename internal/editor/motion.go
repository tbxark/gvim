package editor

import (
	"unicode"

	"github.com/tbxark/gvim/internal/buffer"
)

// motion computes where the cursor goes next. rows is the editor height, used
// by page motions. A motion never changes the document.
type motion func(d *Document, rows int) buffer.Pos

// prevPos returns the position one character before p. A line break counts
// as one step, landing at the end of the previous line.
func prevPos(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col > 0 {
		return buffer.Pos{Line: p.Line, Col: p.Col - 1}, true
	}
	if p.Line > 0 {
		return buffer.Pos{Line: p.Line - 1, Col: b.LineLen(p.Line - 1)}, true
	}
	return p, false
}

// nextPos returns the position one character after p. A line break counts as
// one step, landing at the start of the next line.
func nextPos(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col < b.LineLen(p.Line) {
		return buffer.Pos{Line: p.Line, Col: p.Col + 1}, true
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

// wordEnd moves forward to just after the end of the current or next word.
func wordEnd(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	for {
		next, ok := nextPos(b, q)
		if !ok {
			break
		}
		q = next
		if b.ClassAt(q) != buffer.ClassSpace {
			break
		}
	}
	cls := b.ClassAt(q)
	if cls == buffer.ClassSpace {
		return q
	}
	for {
		next, ok := nextPos(b, q)
		if !ok || b.ClassAt(next) != cls {
			break
		}
		q = next
	}
	// q is on the last character; the cursor goes after it.
	return buffer.Pos{Line: q.Line, Col: q.Col + 1}
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

// motions for the cursor, keyed by the action name the keymap uses.
var (
	motionLeft = func(d *Document, _ int) buffer.Pos {
		if p, ok := prevPos(d.Buffer, d.Cursor); ok {
			return p
		}
		return d.Cursor
	}
	motionRight = func(d *Document, _ int) buffer.Pos {
		if p, ok := nextPos(d.Buffer, d.Cursor); ok {
			return p
		}
		return d.Cursor
	}
	motionUp = func(d *Document, _ int) buffer.Pos {
		return verticalTarget(d, d.Cursor.Line-1)
	}
	motionDown = func(d *Document, _ int) buffer.Pos {
		return verticalTarget(d, d.Cursor.Line+1)
	}
	motionWordLeft = func(d *Document, _ int) buffer.Pos {
		return wordLeft(d.Buffer, d.Cursor)
	}
	motionWordRight = func(d *Document, _ int) buffer.Pos {
		return wordRight(d.Buffer, d.Cursor)
	}
	motionWordEnd = func(d *Document, _ int) buffer.Pos {
		return wordEnd(d.Buffer, d.Cursor)
	}
	motionLineStart = func(d *Document, _ int) buffer.Pos {
		return buffer.Pos{Line: d.Cursor.Line}
	}
	motionLineEnd = func(d *Document, _ int) buffer.Pos {
		return buffer.Pos{Line: d.Cursor.Line, Col: d.Buffer.LineLen(d.Cursor.Line)}
	}
	motionSmartHome = func(d *Document, _ int) buffer.Pos {
		first := firstNonBlank(d.Buffer, d.Cursor.Line)
		if d.Cursor.Col == first {
			return buffer.Pos{Line: d.Cursor.Line}
		}
		return buffer.Pos{Line: d.Cursor.Line, Col: first}
	}
	motionDocStart = func(*Document, int) buffer.Pos { return buffer.Pos{} }
	motionDocEnd   = func(d *Document, _ int) buffer.Pos { return d.Buffer.End() }
)

// verticalTarget moves to line, keeping the wanted column where the line is long enough.
func verticalTarget(d *Document, line int) buffer.Pos {
	if d.want < 0 {
		d.want = d.Cursor.Col
	}
	line = min(max(line, 0), d.Buffer.Lines()-1)
	return buffer.Pos{Line: line, Col: min(d.want, d.Buffer.LineLen(line))}
}
