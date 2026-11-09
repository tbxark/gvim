package prompt

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// TestLineSelectionRanges: Shift+Left/Right/Home/End and Ctrl+Shift+Left/Right maintain
// the anchor/head range, and an unshifted arrow cancels the selection while keeping the
// text.
func TestLineSelectionRanges(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcd"})
	l.field.SetCursor(0)
	l.SelectRight()
	l.SelectRight()
	if got, want := l.SelectedText(), "ab"; got != want {
		t.Fatalf("Shift+Right twice selected %q, want %q", got, want)
	}
	l.SelectLeft()
	if got, want := l.SelectedText(), "a"; got != want {
		t.Fatalf("Shift+Left selected %q, want %q", got, want)
	}

	l = NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcd"})
	l.field.SetCursor(2)
	l.SelectHome()
	if got, want := l.SelectedText(), "ab"; got != want {
		t.Fatalf("Shift+Home selected %q, want %q", got, want)
	}
	l.SelectEnd()
	if got, want := l.SelectedText(), "cd"; got != want {
		t.Fatalf("Shift+End from the Home anchor selected %q, want %q", got, want)
	}

	l = NewLine(testStyles(), LineOptions{Label: "Value:", Input: "one two three"})
	l.field.CursorEnd()
	l.SelectWordLeft()
	if got, want := l.SelectedText(), "three"; got != want {
		t.Fatalf("Ctrl+Shift+Left selected %q, want %q", got, want)
	}
	l.SelectWordLeft()
	if got, want := l.SelectedText(), "two three"; got != want {
		t.Fatalf("second Ctrl+Shift+Left selected %q, want %q", got, want)
	}

	l.clearSelection()
	l.field.SetCursor(2)
	l.SelectRight()
	l.Update(key("left"))
	if l.HasSelection() {
		t.Fatal("an unshifted arrow kept the selection")
	}
	if got := l.Value(); got != "one two three" {
		t.Fatalf("arrow cancel changed the text to %q", got)
	}
}

// TestLineSelectionReplacesAndDeletes: typing over a selection replaces it with one
// onChange, Backspace/Delete remove exactly the selected range, and a cut-confirmed
// DeleteSelection reports the remaining text.
func TestLineSelectionReplacesAndDeletes(t *testing.T) {
	t.Parallel()
	var changed []string
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcd", OnChange: func(s string) { changed = append(changed, s) }})
	l.SelectAll()
	l.Update(key("x"))
	if got := l.Value(); got != "x" {
		t.Fatalf("typing over selection = %q, want x", got)
	}
	if l.HasSelection() {
		t.Fatal("selection survived the replacement")
	}
	if len(changed) != 1 || changed[0] != "x" {
		t.Fatalf("onChange = %q, want only x", changed)
	}

	changed = nil
	l = NewLine(testStyles(), LineOptions{Label: "Value:", Input: "abcd", OnChange: func(s string) { changed = append(changed, s) }})
	l.SelectLeft()
	l.Update(key("backspace"))
	if got := l.Value(); got != "abc" {
		t.Fatalf("Backspace over last rune = %q, want abc", got)
	}
	if len(changed) != 1 || changed[0] != "abc" {
		t.Fatalf("onChange = %q, want abc", changed)
	}
	l.Update(key("backspace"))
	if got := l.Value(); got != "ab" {
		t.Fatalf("the second Backspace did not edit the remaining text: %q", got)
	}
}

// TestLineSelectionDrawsReverseVideo: Draw paints selected runes with the overlay's
// selected style, not the normal field style.
func TestLineSelectionDrawsReverseVideo(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "", Input: "abcd"})
	l.SelectAll()
	g := grid.New(12, 3)
	l.Draw(g)
	selected := g.At(2, 2)
	if selected.Text != "a" || selected.Style != l.styles.selected {
		t.Fatalf("selected cell = %+v, want 'a' with the selected style", selected)
	}
	l.clearSelection()
	g = grid.New(12, 3)
	l.Draw(g)
	plain := g.At(2, 2)
	if plain.Text != "a" || plain.Style != l.styles.field {
		t.Fatalf("unselected cell = %+v, want 'a' with the field style", plain)
	}
	if selected.Style == plain.Style {
		t.Fatal("selection and plain field styles are identical")
	}
}
