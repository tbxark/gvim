package uiadapter

import "github.com/tbxark/gvim/editor"

// The adapter satisfies the optional ui ports of this file.
var _ interface {
	ToggleFoldAt(line int) bool
	SetRowMapper(f editor.RowMapper)
	SetRowWindowMapper(f editor.RowWindowMapper)
	ScrollMargin() int
} = &Editor{}

// ToggleFoldAt toggles the fold that starts at line without moving the caret, for a
// click on the fold marker in the gutter. It is the editor's optional foldToggler port.
func (a *Editor) ToggleFoldAt(line int) bool {
	return a.ed.ToggleFoldAt(line)
}

// SetRowMapper gives the editor the function that reports where a line wraps, so that
// Up, Down, Home and End move by display row. It is the ui's rowMapped port.
func (a *Editor) SetRowMapper(f editor.RowMapper) {
	a.ed.SetRowMapper(f)
}

// SetRowWindowMapper gives the editor the function that reports the rows around a column of a
// line too long to list, so that Up, Down, Home and End move by display row there too. It is
// the ui's rowWindowMapped port.
func (a *Editor) SetRowWindowMapper(f editor.RowWindowMapper) {
	a.ed.SetRowWindowMapper(f)
}

// ScrollMargin is the scroll-off margin in rows. It is the ui's scrollMargined port.
func (a *Editor) ScrollMargin() int {
	return a.ed.ScrollMargin()
}
