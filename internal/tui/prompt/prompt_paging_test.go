package prompt

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLinePageSuggestions: Fresh's prompt_page_up/prompt_page_down page the suggestion
// list by the visible rows (maxSuggestionRows); the highlight clamps at both ends.
func TestLinePageSuggestions(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{})
	items := make([]Suggestion, 10)
	for i := range items {
		items[i] = Suggestion{Text: fmt.Sprintf("s%d", i)}
	}
	l.SetSuggestions(items, 0)
	press := func(code rune) {
		l.Update(tea.KeyPressMsg{Code: code})
	}
	press(tea.KeyPgDown)
	if l.selSuggestion != 4 || l.Value() != "s4" {
		t.Fatalf("PageDown selection = %d value %q, want 4/s4", l.selSuggestion, l.Value())
	}
	press(tea.KeyPgDown)
	press(tea.KeyPgDown)
	if l.selSuggestion != 9 {
		t.Fatalf("PageDown clamp selection = %d, want 9", l.selSuggestion)
	}
	press(tea.KeyPgUp)
	if l.selSuggestion != 5 || l.Value() != "s5" {
		t.Fatalf("PageUp selection = %d value %q, want 5/s5", l.selSuggestion, l.Value())
	}
	press(tea.KeyPgUp)
	press(tea.KeyPgUp)
	if l.selSuggestion != 0 {
		t.Fatalf("PageUp clamp selection = %d, want 0", l.selSuggestion)
	}
}
