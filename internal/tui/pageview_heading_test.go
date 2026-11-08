package tui

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

func TestPageViewHeadingScrollbarMarks(t *testing.T) {
	t.Parallel()
	m, ed := viewModelWith(t, editor.New(editor.Options{}), DefaultViewOptions(),
		"# One\ntext\n## Two\nmore text\n", 60, 12)
	ed.Active().SetLanguage("markdown")
	ed.SetCursor(buffer.Pos{Line: 1, Col: 0}, false)
	runAction(t, m, "toggle_page_view")
	p := m.viewPane(m.layout().FocusedPane(), m.paneGeometry(m.layout().FocusedPane(), m.ed.Active()), m.ed.Active())
	marks := p.TrackMarks
	if len(marks) != 2 {
		t.Fatalf("marks = %+v, want the two headings", marks)
	}
	if marks[0].From != 0 || marks[1].From != 2 || marks[0].To != 0 {
		t.Fatalf("marks = %+v, want lines 0 and 2", marks)
	}
	if marks[0].Style.FG == nil {
		t.Fatal("the heading mark has no foreground colour")
	}
	// The marks follow an edit (the memo is keyed by the text version).
	ed.SetCursor(buffer.Pos{Line: 1, Col: 0}, false)
	ed.Do(editor.Action("insert_char"), map[string]any{"text": "#"})
	p = m.viewPane(m.layout().FocusedPane(), m.paneGeometry(m.layout().FocusedPane(), m.ed.Active()), m.ed.Active())
	if len(p.TrackMarks) < 2 {
		t.Fatalf("marks after the edit = %+v", p.TrackMarks)
	}
}
