package editor

import (
	"strings"
)

// insertTab handles Tab. With a selection it indents the selected lines.
// Without one it inserts one indent unit, as Fresh does: a literal tab when
// UseTabs is set, and otherwise tab_size spaces whatever the column.
func (e *Editor) insertTab(d *Document) {
	if d.HasSelection() {
		d.IndentLines(e.indentUnit(d))
		return
	}
	// Markdown Tab follows Fresh's markdown_source plugin: blank list items indent
	// and cycle the bullet, everything else takes four spaces.
	if e.markdownTab(d) {
		return
	}
	d.InsertText(e.indentUnit(d))
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
