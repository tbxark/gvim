package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
)

func TestFindBarOptionLabelsAreClickable(t *testing.T) {
	m, _ := newSearchModel(t, "Foo foo", "bar")
	press(m, ctrl('f'))
	m.Update(typed("f"))
	rows := screenRows(m)
	optRow := len(rows) - 2
	x := strings.Index(rows[optRow], "[ ] Case Sensitive")
	if x < 0 {
		t.Fatalf("no case toggle on row %d: %q", optRow, rows[optRow])
	}
	clickAt(m, x+3, optRow)
	if got := screenRows(m)[optRow]; !strings.Contains(got, "[x] Case Sensitive") {
		t.Fatalf("options row after the click = %q, want the case box checked", got)
	}
	if !m.findOpen() {
		t.Fatal("a click on an option closed the find bar")
	}
	x = strings.Index(screenRows(m)[optRow], "[ ] Regex")
	clickAt(m, x+2, optRow)
	if got := screenRows(m)[optRow]; !strings.Contains(got, "[x] Regex") || !strings.Contains(got, "[x] Case Sensitive") {
		t.Fatalf("options row = %q, want case and regex checked", got)
	}
}

// TestClickInTheEditorWhileTheFindBarIsOpenMovesTheCaret: the click is not swallowed,
// the bar stays open, and Esc does not take the caret back to where the bar opened.
func TestClickInTheEditorWhileTheFindBarIsOpenMovesTheCaret(t *testing.T) {
	m, sf := newSearchModel(t, "hello world", "second line", "third")
	press(m, ctrl('f'))
	l := m.layout()
	clickAt(m, l.textX+3, l.bodyTop+1)
	if got := sf.doc.cursor; got != (buffer.Pos{Line: 1, Col: 3}) {
		t.Fatalf("caret after the click = %v, want line 1 col 3", got)
	}
	if !m.findOpen() {
		t.Fatal("the click closed the find bar")
	}
	press(m, special(tea.KeyEscape))
	if got := sf.doc.cursor; got != (buffer.Pos{Line: 1, Col: 3}) {
		t.Errorf("Esc moved the caret to %v, want it left where the click put it", got)
	}
}

func TestWheelScrollsTheEditorWhileTheFindBarIsOpen(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	m, sf := newSearchModel(t, lines...)
	press(m, ctrl('f'))
	l := m.layout()
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.textX + 2, Y: l.bodyTop + 2, Button: tea.MouseWheelDown}))
	if sf.doc.top == 0 {
		t.Error("a wheel notch over the text did not scroll while the find bar was open")
	}
}

func TestFindBarSwallowsOnlyItsOwnRows(t *testing.T) {
	m, sf := newSearchModel(t, "hello world", "second line", "third")
	press(m, ctrl('f'))
	h := len(screenRows(m))
	before := sf.doc.cursor
	clickAt(m, 20, h-1) // the search row: the keys stay with it, the caret stays
	if sf.doc.cursor != before {
		t.Errorf("a click on the search row moved the caret to %v", sf.doc.cursor)
	}
	if !m.findOpen() {
		t.Error("a click on the search row closed the bar")
	}
}
