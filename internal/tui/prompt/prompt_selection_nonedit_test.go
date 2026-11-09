package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLineSelectionNonEditingKeysPreserveText: with a selection and no suggestion list,
// Tab/PageUp/PageDown/Shift+Tab have no insertion semantics. They cancel the selection but
// must not delete the selected text or report an onChange.
func TestLineSelectionNonEditingKeysPreserveText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "tab", key: tea.KeyPressMsg{Code: tea.KeyTab}},
		{name: "shift+tab", key: tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}},
		{name: "pageup", key: tea.KeyPressMsg{Code: tea.KeyPgUp}},
		{name: "pagedown", key: tea.KeyPressMsg{Code: tea.KeyPgDown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "old", OnChange: func(string) { called = true }})
			l.SelectAll()
			l.Update(tt.key)
			if got := l.Value(); got != "old" {
				t.Fatalf("%s changed the selected text to %q", tt.name, got)
			}
			if called {
				t.Fatalf("%s fired onChange on an unchanged field", tt.name)
			}
			if l.HasSelection() {
				t.Fatalf("%s left the selection set", tt.name)
			}
		})
	}
}
