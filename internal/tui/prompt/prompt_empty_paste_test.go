package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLineEmptyPasteKeepsSelection: an empty bracketed paste and an empty Ctrl+V result
// are not edits. With a selection they keep both the text and the selection, and they do
// not call onChange or silently delete the range.
func TestLineEmptyPasteKeepsSelection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		do   func(l *Line)
	}{
		{name: "empty PasteMsg", do: func(l *Line) { l.Update(tea.PasteMsg{Content: ""}) }},
		{name: "empty Ctrl+V", do: func(l *Line) {
			l.paste = func() tea.Msg { return pasteMsg("") }
			_, cmd := l.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
			if cmd == nil {
				t.Fatal("Ctrl+V started no clipboard read")
			}
			if msg := cmd(); msg != nil {
				l.Update(msg)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "old", OnChange: func(string) { called = true }})
			l.SelectAll()
			tt.do(l)
			if got := l.Value(); got != "old" {
				t.Fatalf("%s changed the value to %q", tt.name, got)
			}
			if !l.HasSelection() || l.SelectedText() != "old" {
				t.Fatalf("%s lost the selection: selected %q, has %v", tt.name, l.SelectedText(), l.HasSelection())
			}
			if called {
				t.Fatalf("%s fired onChange on an empty paste", tt.name)
			}
		})
	}
}
