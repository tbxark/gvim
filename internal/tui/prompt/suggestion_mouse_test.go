package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestSuggestionMouseSelect(t *testing.T) {
	var changed string
	l := NewLine(testStyles(), LineOptions{OnChange: func(s string) { changed = s }})
	l.SetSuggestions([]Suggestion{{Text: "first"}, {Text: "disabled", Disabled: true}, {Text: "third", Value: "value"}}, -1)
	l.Draw(grid.New(30, 8))
	l.Update(tea.MouseClickMsg{X: 2, Y: 5, Button: tea.MouseLeft})
	if l.Value() != "" {
		t.Fatal("disabled suggestion selected")
	}
	l.Update(tea.MouseClickMsg{X: 2, Y: 4, Button: tea.MouseLeft})
	if l.Value() != "value" || changed != "value" {
		t.Fatalf("click: value=%q changed=%q", l.Value(), changed)
	}
}
