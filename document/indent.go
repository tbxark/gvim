package document

import (
	"github.com/tbxark/gvim/buffer"
	"strings"
	"unicode/utf8"
)

// IndentLines inserts unit at the start of each selected line. The caret moves
// with its text. An anchor at column 0 of an indented line moves past the new
// indent, as Fresh does.
func (d *Document) IndentLines(unit string) {
	first, last := d.selectedLines()
	width := utf8.RuneCountInString(unit)
	d.Buffer.Group(func() {
		for line := first; line <= last; line++ {
			d.Buffer.Insert(buffer.Pos{Line: line}, unit)
		}
	})
	d.Cursor = indentedPos(d.Cursor, first, last, width, false)
	if d.Sel != nil {
		d.Sel.Anchor = indentedPos(d.Sel.Anchor, first, last, width, true)
		d.Sel.Head = d.Cursor
	}
	d.Want = -1
}

// indentedPos moves p past the indent that indentLines inserted at the start
// of each line from first to last. A position at column 0 moves only when
// atStart is set, which is the case for the anchor.
func indentedPos(p buffer.Pos, first, last, width int, atStart bool) buffer.Pos {
	if p.Line < first || p.Line > last {
		return p
	}
	if p.Col > 0 || atStart {
		p.Col += width
	}
	return p
}

// indentWidth returns how many runes one dedent removes from the start of
// line: a single leading tab, or else up to tabSize leading spaces.
func indentWidth(line string, tabSize int) int {
	if strings.HasPrefix(line, "\t") {
		return 1
	}
	n := 0
	for n < tabSize && n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}
