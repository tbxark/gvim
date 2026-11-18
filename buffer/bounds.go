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
	n := b.LineLen(p.Line)
	if n == 0 {
		return p, p
	}
	if p.Col >= n {
		p.Col = n - 1
	}
	line := b.lineRunes(p.Line)
	if n > len(line) {
		return hugeWordBounds(b, p, n)
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

// hugeWordBounds finds the word run around p on a huge line whose decoded prefix
// stops before the full line length n, reading in stride-sized slices.
func hugeWordBounds(b *Buffer, p Pos, n int) (start, end Pos) {
	r, ok := b.RuneAt(p)
	if !ok {
		return p, p
	}
	cls := classOf(r, true)
	start, end = p, Pos{Line: p.Line, Col: p.Col + 1}
	const chunk = 4096
	for start.Col > 0 {
		from := max(start.Col-chunk, 0)
		seg := []rune(b.LineSlice(p.Line, from, start.Col-from))
		if len(seg) == 0 {
			break
		}
		i := len(seg)
		for i > 0 && classOf(seg[i-1], true) == cls {
			i--
		}
		start.Col = from + i
		if i > 0 {
			break
		}
	}
	for end.Col < n {
		take := min(chunk, n-end.Col)
		seg := []rune(b.LineSlice(p.Line, end.Col, take))
		if len(seg) == 0 {
			break
		}
		i := 0
		for i < len(seg) && classOf(seg[i], true) == cls {
			i++
		}
		end.Col += i
		if i < len(seg) {
			break
		}
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
