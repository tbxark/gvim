package document

import (
	"github.com/tbxark/gvim/buffer"
	"unicode"
)

// lastCharCol returns the column of the last character of line, or 0 for an empty line.
func lastCharCol(b *buffer.Buffer, line int) int {
	n := b.LineLen(line)
	if n == 0 {
		return 0
	}
	return prevBoundary(b, line, n)
}

// onLastChar moves p back to the last character of its line when it is past it, which
// is where a vertical move leaves the caret on a shorter line.
func onLastChar(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	if last := lastCharCol(b, p.Line); p.Col > last {
		p.Col = last
	}
	return p
}

// viLastLine is the last line vi can be on. The empty line after the final line break of
// the text is not a line of the file.
func viLastLine(b *buffer.Buffer) int {
	last := b.Lines() - 1
	if last > 0 && b.LineLen(last) == 0 {
		last--
	}
	return last
}

// toggleRunes swaps the case of every letter of s, one rune for one rune, so the text
// keeps its length and its line breaks.
func toggleRunes(s string) string {
	out := []rune(s)
	for i, r := range out {
		if unicode.IsUpper(r) {
			out[i] = unicode.ToLower(r)
		} else {
			out[i] = unicode.ToUpper(r)
		}
	}
	return string(out)
}

// ToggleCase swaps the case of the selection of each caret. A caret without a selection
// swaps the character at it and moves past that character, as vi's ~ does; at the end of
// a line there is nothing to swap and the caret stays. A selection is replaced by the
// swapped text and the caret ends after it.
func (d *Document) ToggleCase() {
	cs := d.AllCarets()
	plans := make([]caretPlan, len(cs))
	for i, c := range cs {
		if lo, hi := c.Bounds(); lo != hi {
			old := d.PlanRange(lo, hi)
			if swapped := toggleRunes(old); swapped != old {
				plans[i].Edit = &edit{From: lo, To: hi, Text: swapped}
			}
			continue
		}
		p := c.Head
		if p.Col >= d.Buffer.LineLen(p.Line) {
			continue
		}
		next := buffer.Pos{Line: p.Line, Col: nextBoundary(d.Buffer, p.Line, p.Col)}
		old := d.PlanRange(p, next)
		if swapped := toggleRunes(old); swapped != old {
			plans[i].Edit = &edit{From: p, To: next, Text: swapped}
			continue
		}
		// Nothing to change (a digit, a symbol), but the caret still moves on. An empty
		// edit at the caret carries it through the other carets' edits.
		width := next.Col - p.Col
		plans[i].Edit = &edit{From: p, To: p}
		plans[i].Place = func(end buffer.Pos) buffer.Pos { return buffer.Pos{Line: end.Line, Col: end.Col + width} }
	}
	d.applyPlans(cs, plans)
}
