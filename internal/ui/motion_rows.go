package ui

import "github.com/tbxark/gvim/internal/editor"

// rowMapped is the optional editor port that takes the function reporting where a
// line wraps. With it the editor moves Up, Down, Home and End by display row. The
// app adapter implements it.
type rowMapped interface {
	SetRowMapper(f editor.RowMapper)
}

// installRowMapper gives the editor the wrapped rows of the focused pane.
func (m *Model) installRowMapper() {
	if s, ok := m.ed.(rowMapped); ok {
		s.SetRowMapper(m.rowStarts)
	}
}

// rowStarts returns the columns at which the display rows of a line of the active
// document start in the focused pane, or nil when the line is not wrapped there.
func (m *Model) rowStarts(line int) []int {
	doc := m.ed.Active()
	if doc == nil {
		return nil
	}
	l := m.layout()
	if l.gated {
		return nil
	}
	fp := l.focusedPane()
	return m.paneGeometry(fp, doc).RowStarts(fp.w, fp.h, line)
}

// revealCaretCentered scrolls the editor body so the caret line is on screen after a
// jump, such as the next search match. A jump that has to scroll puts the caret line
// in the middle of the body, so the context above and below it shows. A caret already
// on screen does not scroll the view.
func (m *Model) revealCaretCentered() {
	doc := m.ed.Active()
	rows := m.bodyRows()
	if doc == nil || rows <= 0 {
		return
	}
	if line := doc.Cursor().Line; line < doc.Top() || line >= doc.Top()+rows {
		m.ed.ScrollTo(max(line-rows/2, 0))
	}
}
