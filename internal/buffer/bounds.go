package buffer

import "unicode"

// Class groups characters the way word motions and word selection do. Two
// adjacent characters belong to the same word run when they share a Class.
type Class uint8

const (
	// ClassSpace is a whitespace character, or a position with no character.
	ClassSpace Class = iota
	// ClassWord is a letter, digit, underscore or combining mark.
	ClassWord
	// ClassPunct is any other visible character.
	ClassPunct
)

// classOf returns the class of rune r. When ok is false there is no rune, as
// at the end of a line, and the result is ClassSpace.
func classOf(r rune, ok bool) Class {
	switch {
	case !ok || unicode.IsSpace(r):
		return ClassSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || unicode.IsMark(r):
		return ClassWord
	default:
		return ClassPunct
	}
}

// ClassAt returns the class of the character at p. A position with no
// character, such as the end of a line, counts as ClassSpace, so word runs
// stop at line breaks.
func (b *Buffer) ClassAt(p Pos) Class {
	r, ok := b.RuneAt(p)
	return classOf(r, ok)
}

// WordBounds returns the run of same-class characters that contains p, as a
// half-open range on one line: start is the first character of the run and
// end is the position just after its last character. The run comes from the
// character at p. If p is at the end of a line, the character before p is
// used instead, so a click past the last letter of a word selects that word.
// On an empty line, or at column 0 with no character, it returns (p, p).
// p is clamped first.
//
// Whitespace and punctuation runs are returned the same way as words. Callers
// that want only words, such as the keyboard select_word action, check the
// class of p before calling.
func WordBounds(b *Buffer, p Pos) (start, end Pos) {
	p = b.Clamp(p)
	line := b.lineRunes(p.Line)
	if p.Col >= len(line) {
		if p.Col == 0 {
			return p, p
		}
		p.Col--
	}
	cls := classOf(line[p.Col], true)
	start, end = p, Pos{Line: p.Line, Col: p.Col + 1}
	for start.Col > 0 && classOf(line[start.Col-1], true) == cls {
		start.Col--
	}
	for end.Col < len(line) && classOf(line[end.Col], true) == cls {
		end.Col++
	}
	return start, end
}

// LineBounds returns the range that selects a whole line, including its
// trailing newline: start is column 0 of line, and end is column 0 of the
// next line. On the last line, which has no newline, end is the end of the
// buffer. An out-of-range line is clamped into the document.
func LineBounds(b *Buffer, line int) (start, end Pos) {
	line = min(max(line, 0), b.Lines()-1)
	start = Pos{Line: line}
	if line+1 < b.Lines() {
		return start, Pos{Line: line + 1}
	}
	return start, b.End()
}
