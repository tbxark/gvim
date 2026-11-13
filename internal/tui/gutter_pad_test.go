package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/space"
)

// With the line numbers hidden the text keeps PadX cells from the explorer border or the
// frame edge. The layout and the painted cells agree on the column.
func TestHiddenGutterTextStartsPadCellsIntoThePane(t *testing.T) {
	t.Parallel()
	for _, explorer := range []bool{false, true} {
		m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
		if err := m.Run("toggle_line_numbers"); err != nil {
			t.Fatalf("toggle_line_numbers = %v", err)
		}
		m.explorerOpen = explorer
		l := m.layout()
		if explorer && l.Ex == 0 {
			t.Fatal("the explorer did not open")
		}
		y := l.BodyTop
		want := l.EditorX + space.PadX
		if l.TextX != want {
			t.Errorf("explorer %v: textX %d, want editorX+PadX = %d", explorer, l.TextX, want)
		}
		row := m.paint().Plain()[y]
		if got := []rune(row)[want]; got != 'h' {
			t.Errorf("explorer %v: cell %d of row %q is %q, want the text", explorer, want, row, got)
		}
		if got := []rune(row)[l.EditorX]; got != ' ' {
			t.Errorf("explorer %v: the pad cell %d holds %q, want a blank", explorer, l.EditorX, got)
		}
	}
}

// The pad cell belongs to no pane, so a click on it places no caret.
func TestClickOnTheHiddenGutterPadPlacesNoCaret(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("hello"), nil, 80, 24)
	if err := m.Run("toggle_line_numbers"); err != nil {
		t.Fatalf("toggle_line_numbers = %v", err)
	}
	l := m.layout()
	if got := l.Hit(l.EditorX, l.BodyTop); got.Kind == frame.HitEditorText {
		t.Errorf("a click on the pad cell hit the text: %v", got.Kind)
	}
}
