package document

import (
	"github.com/tbxark/gvim/buffer"
	"unicode"
)

// prevPos returns the position one character (grapheme cluster) before p. A
// line break counts as one step, landing at the end of the previous line.
func PrevPos(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col > 0 {
		return buffer.Pos{Line: p.Line, Col: PrevBoundary(b, p.Line, p.Col)}, true
	}
	if p.Line > 0 {
		return buffer.Pos{Line: p.Line - 1, Col: b.LineLen(p.Line - 1)}, true
	}
	return p, false
}

// nextPos returns the position one character (grapheme cluster) after p. A
// line break counts as one step, landing at the start of the next line.
func NextPos(b *buffer.Buffer, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col < b.LineLen(p.Line) {
		return buffer.Pos{Line: p.Line, Col: NextBoundary(b, p.Line, p.Col)}, true
	}
	if p.Line+1 < b.Lines() {
		return buffer.Pos{Line: p.Line + 1, Col: 0}, true
	}
	return p, false
}

// wordLeft moves back to the start of the previous word, skipping spaces first.
func WordLeft(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	for {
		prev, ok := PrevPos(b, q)
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
		prev, ok := PrevPos(b, q)
		if !ok || b.ClassAt(prev) != cls {
			return q
		}
		q = prev
	}
}

// wordRight moves forward to the start of the next word.
func WordRight(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	if cls := b.ClassAt(q); cls != buffer.ClassSpace {
		// A character is under q while the class matches, so nextPos always advances; the
		// end of the document reads as a space and stops the run.
		for b.ClassAt(q) == cls {
			q, _ = NextPos(b, q)
		}
	}
	for b.ClassAt(q) == buffer.ClassSpace {
		next, ok := NextPos(b, q)
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
func WordEnd(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	q := p
	cls := b.ClassAt(q)
	if cls == buffer.ClassSpace {
		for b.ClassAt(q) == buffer.ClassSpace {
			next, ok := NextPos(b, q)
			if !ok {
				return q
			}
			q = next
		}
		cls = b.ClassAt(q)
	}
	for b.ClassAt(q) == cls {
		next, ok := NextPos(b, q)
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
	prev, _ := PrevPos(b, p)
	return prev
}

// firstNonBlank returns the column of the first non-space rune on line.
func FirstNonBlank(b *buffer.Buffer, line int) int {
	for col := range b.LineLen(line) {
		if r, _ := b.RuneAt(buffer.Pos{Line: line, Col: col}); !unicode.IsSpace(r) {
			return col
		}
	}
	return b.LineLen(line)
}
