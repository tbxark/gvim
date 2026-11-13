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

// TestLineMouseClickAfterTheTextShrank: the click maps through the window of the last
// Draw, so text that got shorter since then must not index past the end.
func TestLineMouseClickAfterTheTextShrank(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcdef"})
	l.Draw(grid.New(20, 3))
	l.field.SetValue("ab")
	l.Update(tea.MouseClickMsg(tea.Mouse{X: l.fieldX + 5, Y: l.fieldY, Button: tea.MouseLeft}))
	if got := l.field.Position(); got != 2 {
		t.Fatalf("caret = %d, want the end of the shortened text", got)
	}
}

// TestLineCutValueDropsASelectionMadeMeanwhile: a selection made while the clipboard write
// was in flight must not outlive the cleared text.
func TestLineCutValueDropsASelectionMadeMeanwhile(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Input: "abc"})
	l.SelectLeft()
	if got := l.CutValue(); got != "abc" {
		t.Fatalf("CutValue = %q", got)
	}
	if l.HasSelection() || l.SelectedText() != "" {
		t.Fatal("the selection survived the cut")
	}
}
