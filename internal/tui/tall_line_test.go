package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// tallText wraps to far more rows than a small frame holds.
var tallText = strings.Repeat("abcdefghi ", 60)

// endEditor is a fake editor whose "jump" action moves the caret to the end of line 0, as
// ctrl+end or typing at the end of the paragraph does. The fake does not scroll.
type endEditor struct{ *fakeEditor }

func (e endEditor) Do(a editor.Action, args map[string]any) editor.Result {
	if a == "jump" {
		e.doc.cursor = buffer.Pos{Col: len([]rune(tallText))}
		return editor.Result{}
	}
	return e.fakeEditor.Do(a, args)
}

// A caret at the end of a paragraph taller than the window is shown: the view scrolls by
// display rows inside the line, and the caret keeps to the last row of the body.
func TestCaretAtTheEndOfATallWrappedLineIsOnScreen(t *testing.T) {
	keys := testKeys()
	keys["normal|j|ctrl"] = Command{Action: "jump"}
	ed := endEditor{newFakeEditor(tallText, "END")}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 12})
	press(m, ctrl('j'))
	x, y, ok := m.caret()
	if !ok {
		t.Fatal("the caret at the end of a tall wrapped line is off screen")
	}
	if rows := m.bodyRows(); y != m.layout().focusedPane().Y+rows-1 {
		t.Errorf("caret on frame row %d, want the last body row (pane row %d of %d), x=%d", y, rows-1, rows, x)
	}
	if ed.doc.top != 0 {
		t.Errorf("view top %d, want the paragraph's line 0", ed.doc.top)
	}
	if m.topRow(ed.doc.top) == 0 {
		t.Error("the view starts at the first row of the paragraph, so the caret row is below it")
	}
}

// The wheel moves through the rows of a tall paragraph, three per notch, and the view
// does not skip to the next line.
func TestWheelScrollsInsideATallWrappedLine(t *testing.T) {
	ed := newFakeEditor(tallText, "END")
	m := newTestModel(t, ed, nil, 30, 12)
	wheel := func(b tea.MouseButton) {
		m.Update(tea.MouseWheelMsg(tea.Mouse{X: 10, Y: 4, Button: b}))
	}
	wheel(tea.MouseWheelDown)
	wheel(tea.MouseWheelDown)
	if ed.doc.top != 0 || m.topRow(0) != 2*wheelRows {
		t.Fatalf("after two notches down the view is at line %d row %d, want line 0 row %d", ed.doc.top, m.topRow(ed.doc.top), 2*wheelRows)
	}
	wheel(tea.MouseWheelUp)
	if ed.doc.top != 0 || m.topRow(0) != wheelRows {
		t.Errorf("after a notch up the view is at line %d row %d, want line 0 row %d", ed.doc.top, m.topRow(ed.doc.top), wheelRows)
	}
	// A view that the editor moved to another line has no row offset.
	ed.ScrollTo(1)
	if got := m.topRow(1); got != 0 {
		t.Errorf("row offset %d on a line the view was moved to, want 0", got)
	}
}
