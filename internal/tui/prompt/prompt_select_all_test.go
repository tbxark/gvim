package prompt

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLineSelectAllBackspaceAndDeleteClear: Ctrl+A marks the text prefill-selected; a
// Backspace or Delete immediately clears the whole field and reports one empty onChange.
func TestLineSelectAllBackspaceAndDeleteClear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "backspace", key: key("backspace")},
		{name: "delete", key: tea.KeyPressMsg{Code: tea.KeyDelete}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var changed []string
			l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "old", OnChange: func(s string) { changed = append(changed, s) }})
			l.SelectAll()
			l.Update(tt.key)
			if got := l.Value(); got != "" {
				t.Fatalf("value after %s = %q, want empty", tt.name, got)
			}
			if len(changed) != 1 || changed[0] != "" {
				t.Fatalf("onChange after %s = %q, want one empty value", tt.name, changed)
			}
			if l.prefill {
				t.Fatalf("prefill still set after %s", tt.name)
			}
		})
	}
}

// TestLineSelectAllCtrlVPasteReplaces: Ctrl+V keeps the prefill selected while the
// clipboard read command is in flight; the PasteMsg then clears the old text and inserts
// the pasted text, so Ctrl+A Ctrl+V replaces rather than appends.
func TestLineSelectAllCtrlVPasteReplaces(t *testing.T) {
	t.Parallel()
	var changed []string
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "old", OnChange: func(s string) { changed = append(changed, s) }})
	l.SelectAll()
	_, cmd := l.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+V started no clipboard read")
	}
	if l.Value() != "old" || !l.prefill {
		t.Fatalf("Ctrl+V lost the selected prefill: value %q prefill %v", l.Value(), l.prefill)
	}
	l.Update(tea.PasteMsg{Content: "new"})
	if got := l.Value(); got != "new" {
		t.Fatalf("Ctrl+A Ctrl+V paste = %q, want new", got)
	}
	if len(changed) != 1 || changed[0] != "new" {
		t.Fatalf("onChange = %v, want only the pasted text", changed)
	}
}

// TestLineSelectAllEmptyNoOnChange: selecting an empty field and pressing Backspace or
// Delete clears the invisible prefill without reporting a text change.
func TestLineSelectAllEmptyNoOnChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "backspace", key: key("backspace")},
		{name: "delete", key: tea.KeyPressMsg{Code: tea.KeyDelete}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			l := NewLine(testStyles(), LineOptions{Label: "Value:", OnChange: func(string) { called = true }})
			l.SelectAll()
			l.Update(tt.key)
			if l.Value() != "" || l.prefill {
				t.Fatalf("%s left value %q prefill %v", tt.name, l.Value(), l.prefill)
			}
			if called {
				t.Fatalf("%s on an empty selected prompt called onChange", tt.name)
			}
		})
	}
}

// TestLineSelectAllBackspaceClearsValidationError: clearing a selected prefill with
// Backspace/Delete also clears the rejection message the failed submit left behind.
func TestLineSelectAllBackspaceClearsValidationError(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{
		Label: "Value:", Input: "old",
		Validate: func(string) error { return errors.New("bad value") },
	})
	l.Update(key("enter"))
	if l.errText == "" {
		t.Fatal("the rejected Enter left no validation error")
	}
	l.SelectAll()
	l.Update(key("backspace"))
	if got := l.Value(); got != "" {
		t.Fatalf("value after selected Backspace = %q", got)
	}
	if l.errText != "" {
		t.Fatalf("validation error after clearing = %q, want empty", l.errText)
	}
}

// TestLineSelectAllTypedKeyReplacesAndArrowCancels: typing after Ctrl+A replaces the
// whole text; moving the caret cancels the selection but keeps the text for editing.
func TestLineSelectAllTypedKeyReplacesAndArrowCancels(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "old"})
	l.SelectAll()
	typeText(l, "new")
	if got := l.Value(); got != "new" {
		t.Fatalf("typed replacement = %q, want new", got)
	}

	l = NewLine(testStyles(), LineOptions{Label: "Value:", Input: "ab"})
	l.SelectAll()
	l.Update(key("left"))
	typeText(l, "x")
	if got := l.Value(); got != "axb" {
		t.Fatalf("arrow + typed = %q, want axb", got)
	}
	if l.prefill {
		t.Fatal("prefill stayed set after an arrow cancelled the selection")
	}
}
