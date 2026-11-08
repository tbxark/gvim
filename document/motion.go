package document

import (
	"github.com/tbxark/gvim/buffer"
	"unicode"
)

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
