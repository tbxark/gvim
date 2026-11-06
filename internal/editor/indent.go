package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// insertTab handles Tab. With a selection it indents the selected lines.
// Without one it inserts a literal tab when UseTabs is set, and otherwise
// spaces up to the next tab stop.
func (e *Editor) insertTab(d *Document) {
	if d.HasSelection() {
		d.indentLines(e.indentUnit(d))
		return
	}
	if e.indentTabs(d) {
		d.insertText("\t")
		return
	}
	size := e.tabSizeOf(d)
	col := visualColumn(d, size)
	d.insertText(strings.Repeat(" ", size-col%size))
}

// indentUnit is the text one indent level adds at the start of a line: a tab
// when the document inserts tabs, and one tab stop of spaces otherwise.
func (e *Editor) indentUnit(d *Document) string {
	if e.indentTabs(d) {
		return "\t"
	}
	return strings.Repeat(" ", e.tabSizeOf(d))
}

// indentTabs reports whether Tab inserts a tab character in d: the document's own
// setting when it has one, else Options.UseTabs.
func (e *Editor) indentTabs(d *Document) bool {
	return OverrideOr(d.view.IndentTabs, e.opts.UseTabs)
}

// tabSizeOf is the columns per tab stop of d: the document's own TabSize when it
// is set, else Options.TabSize.
func (e *Editor) tabSizeOf(d *Document) int {
	if d.view.TabSize > 0 {
		return d.view.TabSize
	}
	return e.opts.TabSize
}

// visualColumn returns the display column of the caret on its line. A tab
// advances to the next multiple of tabSize; every other rune takes one column.
func visualColumn(d *Document, tabSize int) int {
	p := d.Buffer.Clamp(d.Cursor)
	col := 0
	for _, r := range []rune(d.Buffer.Line(p.Line))[:p.Col] {
		if r == '\t' {
			col += tabSize - col%tabSize
		} else {
			col++
		}
	}
	return col
}

// indentLines inserts unit at the start of each selected line. The caret moves
// with its text. An anchor at column 0 of an indented line moves past the new
// indent, as Fresh does.
func (d *Document) indentLines(unit string) {
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
	d.want = -1
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

// dedent removes one indent unit from the start of each selected line, or of
// the caret line when nothing is selected. A unit is one leading tab, or up to
// tabSize leading spaces. A position moves back by what was removed before it
// on its line.
func (d *Document) dedent(tabSize int) {
	first, last := d.selectedLines()
	widths := make([]int, last-first+1)
	found := false
	for i := range widths {
		widths[i] = indentWidth(d.Buffer.Line(first+i), tabSize)
		found = found || widths[i] > 0
	}
	if !found {
		return
	}
	d.Buffer.Group(func() {
		for i, n := range widths {
			if n > 0 {
				line := first + i
				d.Buffer.Delete(buffer.Pos{Line: line}, buffer.Pos{Line: line, Col: n})
			}
		}
	})
	d.Cursor = dedentedPos(d.Cursor, first, widths)
	if d.Sel != nil {
		d.Sel.Anchor = dedentedPos(d.Sel.Anchor, first, widths)
		d.Sel.Head = d.Cursor
	}
	d.want = -1
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

// dedentedPos moves p back by the width removed from the start of its line.
// widths holds one entry per line from first.
func dedentedPos(p buffer.Pos, first int, widths []int) buffer.Pos {
	i := p.Line - first
	if i < 0 || i >= len(widths) {
		return p
	}
	p.Col = max(p.Col-widths[i], 0)
	return p
}

// insertNewline breaks the line at the caret, replacing any selection. With
// autoIndent the new line starts with the blanks at the start of the caret
// line. Only the blanks before the caret are copied, because the text after the
// caret brings its own indent to the new line.
func (d *Document) insertNewline(autoIndent bool) {
	d.deleteSelection()
	text := "\n"
	if autoIndent {
		text += leadingBlanks(d.Buffer.Line(d.Cursor.Line), d.Cursor.Col)
	}
	d.insertText(text)
}

// leadingBlanks returns the spaces and tabs at the start of line, cut off at
// limit runes. Blanks are ASCII, so bytes and runes agree on them.
func leadingBlanks(line string, limit int) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:min(n, limit)]
}
