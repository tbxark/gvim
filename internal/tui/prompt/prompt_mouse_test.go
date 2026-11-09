package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// TestLineMouseClickPlacesCaret: a left press inside the painted input window moves the
// caret to the clicked rune. Fresh's prompt supports press positioning; clicks outside the
// input (the label or the row margin) leave the caret alone.
func TestLineMouseClickPlacesCaret(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcd"})
	g := grid.New(20, 3)
	l.Draw(g)

	l.Update(tea.MouseClickMsg(tea.Mouse{X: l.fieldX + 2, Y: l.fieldY, Button: tea.MouseLeft}))
	l.Update(key("X"))
	if got := l.Value(); got != "abXcd" {
		t.Fatalf("click at the third cell + X = %q, want abXcd", got)
	}

	l = NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcd"})
	g = grid.New(20, 3)
	l.Draw(g)
	l.Update(tea.MouseClickMsg(tea.Mouse{X: l.fieldX - 1, Y: l.fieldY, Button: tea.MouseLeft}))
	l.Update(key("X"))
	if got := l.Value(); got != "abcdX" {
		t.Fatalf("click outside the input + X = %q, want the caret kept at the end", got)
	}
}
