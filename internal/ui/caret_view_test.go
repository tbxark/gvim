package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// wrappedLines are six lines of 40 cells. In a 24-column frame they wrap to three rows
// each.
func wrappedLines() []string {
	lines := make([]string, 6)
	for i := range lines {
		lines[i] = strings.Repeat("abcdefg ", 5)
	}
	return lines
}

// fieldOverlay stands in for a dialog with a text field: an overlay that reports a caret.
type fieldOverlay struct{}

func (fieldOverlay) Update(tea.Msg) (Overlay, tea.Cmd) { return fieldOverlay{}, nil }
func (fieldOverlay) Draw(*grid.Grid)                   {}
func (fieldOverlay) Active() bool                      { return true }
func (fieldOverlay) Cursor(w, h int) (int, int, bool)  { return 5, 7, true }

// TestResizeKeepsTheCaretOnScreenWhenLinesWrap checks that the caret stays on screen
// when the body shrinks under it. The editor keeps the caret line in view by buffer
// lines, so the root has to reveal the caret's display row.
func TestResizeKeepsTheCaretOnScreenWhenLinesWrap(t *testing.T) {
	ed := newFakeEditor(wrappedLines()...)
	m := newTestModel(t, ed, nil, 24, 30)
	ed.SetCursor(buffer.Pos{Line: 5}, false)
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	if _, _, ok := m.caret(); !ok {
		t.Errorf("caret on wrapped line 5 is off screen after the frame shrank")
	}
}

// jumpEditor is a fake editor whose "jump" action moves the caret to line 5, as a
// keyboard move can. The fake does not scroll, so the caret is only on screen when the
// root reveals its display row.
type jumpEditor struct{ *fakeEditor }

func (e jumpEditor) Do(a editor.Action, args map[string]any) editor.Result {
	if a == "jump" {
		e.doc.cursor = buffer.Pos{Line: 5}
		return editor.Result{}
	}
	return e.fakeEditor.Do(a, args)
}

// TestCaretMovedByAKeyStaysOnScreenWhenLinesWrap checks the common path: a key that
// moves the caret past the window leaves it on screen.
func TestCaretMovedByAKeyStaysOnScreenWhenLinesWrap(t *testing.T) {
	keys := testKeys()
	keys["normal|j|ctrl"] = Command{Action: "jump"}
	ed := jumpEditor{newFakeEditor(wrappedLines()...)}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	press(m, ctrl('j'))
	if got := ed.doc.cursor.Line; got != 5 {
		t.Fatalf("caret line %d after the key, want 5", got)
	}
	if _, _, ok := m.caret(); !ok {
		t.Errorf("caret on wrapped line 5 is off screen after a key moved it there")
	}
}

// TestResizeToAGatedFrameKeepsTheView checks that a frame too small to paint does not
// move the view: the caret reveal has no pane to measure then.
func TestResizeToAGatedFrameKeepsTheView(t *testing.T) {
	ed := newFakeEditor(wrappedLines()...)
	m := newTestModel(t, ed, nil, 24, 30)
	ed.ScrollTo(3)
	m.Update(tea.WindowSizeMsg{Width: 4, Height: 2})
	if got := ed.doc.top; got != 3 {
		t.Errorf("view top %d after the frame was too small to paint, want 3", got)
	}
}

// TestWheelScrollIsNotPulledBackToTheCaret keeps the wheel free: a wheel notch that
// takes the caret off screen leaves the view where the wheel put it.
func TestWheelScrollIsNotPulledBackToTheCaret(t *testing.T) {
	ed := newFakeEditor(wrappedLines()...)
	m := newTestModel(t, ed, nil, 24, 8)
	ed.SetCursor(buffer.Pos{Line: 1}, false)
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 10, Y: 4, Button: tea.MouseWheelDown}))
	if got := ed.doc.top; got != wheelRows {
		t.Errorf("view top %d after the wheel, want %d, the view the wheel chose", got, wheelRows)
	}
}

func TestOverlayCaretShowsWhileTheExplorerHasFocus(t *testing.T) {
	tests := []struct {
		name string
		open func(m *Model)
		want bool
	}{
		{"no overlay keeps the caret hidden", nil, false},
		{"the find bar shows its caret", func(m *Model) { m.openFind(false) }, true},
		{"a text dialog shows its caret", func(m *Model) { m.pushOverlay(fieldOverlay{}) }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, newFakeEditor("hello", "world"), newFakeTree(3), 80, 24)
			m.focusExplorer()
			if tc.open != nil {
				tc.open(m)
			}
			if !m.explorerFocus {
				t.Fatal("the explorer lost its focus when the overlay opened")
			}
			if _, _, ok := m.caret(); ok != tc.want {
				t.Errorf("caret shown = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestDepsTabSizeSetsTheTabStopOfThePane(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("a\tb"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, TabSize: 8})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	p := panesOf(t, m, 1)[0]
	if got := m.paint().At(p.textX+8, p.y).Text; got != "b" {
		t.Errorf("cell (%d, %d) = %q, want the b at tab stop 8", p.textX+8, p.y, got)
	}
}
