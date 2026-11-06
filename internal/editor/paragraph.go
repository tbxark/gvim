package editor

import (
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// isBlankLine reports whether line is a paragraph break, which is a line with
// no text. A carriage return left at the end of the line is not text. A large
// buffer has already removed the carriage return of a line and knows its length
// without reading it, so the line is not read.
func isBlankLine(b *buffer.Buffer, line int) bool {
	if b.Large() {
		return b.LineBytes(line) == 0
	}
	return strings.TrimRight(b.Line(line), "\r") == ""
}

// paragraphUp returns the start of the nearest blank line above line, or the
// start of the document when there is none. In a large document the scan has a
// budget (see scanBudget); limited reports that it stopped there, with no blank
// line found.
func paragraphUp(b *buffer.Buffer, line int) (pos buffer.Pos, limited bool) {
	budget := newScanBudget(b)
	for i := line - 1; i >= 0; i-- {
		if !budget.take(b, i) {
			return buffer.Pos{}, true
		}
		if isBlankLine(b, i) {
			return buffer.Pos{Line: i}, false
		}
	}
	return buffer.Pos{}, false
}

// blankLineBelow returns the start of the nearest blank line below line. ok is
// false when there is none. The empty line after a final line break is not a
// paragraph break, because it is not a line of the text. In a large document the
// scan has a budget, and it ends at the last line the index knows; limited reports
// that it stopped at either, with no blank line found.
func blankLineBelow(b *buffer.Buffer, line int) (pos buffer.Pos, ok, limited bool) {
	last := b.Lines() - 1
	budget := newScanBudget(b)
	for i := line + 1; i <= last; i++ {
		if !budget.take(b, i) {
			return buffer.Pos{}, false, true
		}
		if i == last && b.LineLen(i) == 0 {
			break
		}
		if isBlankLine(b, i) {
			return buffer.Pos{Line: i}, true, false
		}
	}
	return buffer.Pos{}, false, indexUnfinished(b)
}

// paragraphEnd returns the last character of the text. A move down ends here
// when no blank line follows, so the caret does not run past the text. A final
// line break is not counted, and an empty document ends at its start.
func paragraphEnd(b *buffer.Buffer) buffer.Pos {
	last := b.Lines() - 1
	end := buffer.Pos{Line: last, Col: b.LineLen(last)}
	if last > 0 && b.LineLen(last) == 0 {
		end = buffer.Pos{Line: last - 1, Col: b.LineLen(last - 1)}
	}
	if end.Col > 0 {
		return buffer.Pos{Line: end.Line, Col: end.Col - 1}
	}
	if end.Line == 0 {
		return buffer.Pos{}
	}
	return buffer.Pos{Line: end.Line - 1, Col: b.LineLen(end.Line - 1)}
}
