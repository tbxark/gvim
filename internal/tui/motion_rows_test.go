package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// SetRowMapper makes realEditor the rowMapped port the app adapter is.
func (r realEditor) SetRowMapper(f editor.RowMapper) { r.ed.SetRowMapper(f) }

func TestVerticalMotionFollowsTheWrappedRowsOfThePane(t *testing.T) {
	text := strings.Repeat("word ", 20) + "\nsecond\n"
	m, ed := viewModel(t, DefaultViewOptions(), text, 40, 12)
	starts := m.rowStarts(0)
	if len(starts) < 3 {
		t.Fatalf("setup: row starts %v, want a wrapped first line", starts)
	}
	runAction(t, m, "move_down")
	if got := ed.Active().Cursor; got != (buffer.Pos{Line: 0, Col: starts[1]}) {
		t.Fatalf("caret after Down = %v, want the start of the second row (col %d)", got, starts[1])
	}
	runAction(t, m, "move_line_end")
	if got := ed.Active().Cursor; got.Line != 0 || got.Col != starts[2]-1 {
		t.Fatalf("caret after End = %v, want the end of the second row (col %d)", got, starts[2]-1)
	}
	runAction(t, m, "move_up")
	if got := ed.Active().Cursor; got.Line != 0 || got.Col >= starts[1] {
		t.Fatalf("caret after Up = %v, want it back on the first row", got)
	}
}

func TestVerticalMotionIsByLineWithWrapOff(t *testing.T) {
	text := strings.Repeat("word ", 20) + "\nsecond\n"
	m, ed := viewModel(t, DefaultViewOptions(), text, 40, 12)
	runAction(t, m, "toggle_line_wrap")
	if got := m.rowStarts(0); got != nil {
		t.Fatalf("row starts with wrap off = %v, want none", got)
	}
	runAction(t, m, "move_down")
	if got := ed.Active().Cursor; got.Line != 1 {
		t.Fatalf("caret after Down = %v, want line 1", got)
	}
}

func TestFindNextCentersAMatchThatNeedsScrolling(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "text"
	}
	lines[80] = "needle"
	m, sf := newSearchModel(t, lines...)
	press(m, ctrl('f'))
	for _, r := range "needle" {
		press(m, typed(string(r)))
	}
	press(m, special(tea.KeyEscape))
	press(m, ctrl('g'))
	if got := sf.doc.cursor.Line; got != 80 {
		t.Fatalf("caret line = %d after find next, want 80", got)
	}
	if want := 80 - m.bodyRows()/2; sf.doc.top != want {
		t.Fatalf("top = %d, want %d: the match centered in the body", sf.doc.top, want)
	}
}
