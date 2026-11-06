package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// insertTab handles Tab. With a selection it indents the selected lines.
// Without one it inserts one indent unit, as Fresh does: a literal tab when
// UseTabs is set, and otherwise tab_size spaces whatever the column.
func (e *Editor) insertTab(d *Document) {
	if d.HasSelection() {
		d.indentLines(e.indentUnit(d))
		return
	}
	d.insertText(e.indentUnit(d))
}

// indentUnit is the text one indent level adds at the start of a line: a tab
// when the document inserts tabs, and one tab stop of spaces otherwise.
func (e *Editor) indentUnit(d *Document) string {
	if e.indentTabs(d) {
		return "\t"
	}
	return strings.Repeat(" ", e.tabSizeOf(d))
}

// indentTabs reports whether Tab inserts a tab character in d: the effective
// use_tabs of its language (SettingsFor), which the document's own setting overrides.
func (e *Editor) indentTabs(d *Document) bool {
	return e.SettingsFor(d).UseTabs
}

// tabSizeOf is the columns per tab stop of d: the effective tab_size of its
// language (SettingsFor), which the document's own TabSize overrides.
func (e *Editor) tabSizeOf(d *Document) int {
	if n := e.SettingsFor(d).TabSize; n > 0 {
		return n
	}
	return e.opts.TabSize
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
