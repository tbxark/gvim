package prompt

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLineSelectAllBackspaceAndDeleteClear: Ctrl+A selects the whole text; a
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
			if l.HasSelection() {
				t.Fatalf("selection still set after %s", tt.name)
			}
		})
	}
}

// pasteMsg stands in for textinput's unexported Ctrl+V paste result. The prompt
// recognizes it by type name and string kind, exactly as it recognizes the real one, so
// this test drives Update(Ctrl+V) -> command -> result -> Update without the system
// clipboard.
type pasteMsg string

// TestLineSelectAllCtrlVPasteReplaces: Ctrl+V keeps the selection while the clipboard
// read command is in flight. Executing that command yields textinput's pasteMsg; feeding
// it back clears the selected range and inserts the pasted text, so Ctrl+A Ctrl+V replaces
// rather than appends. The selection is gone afterwards, so a Backspace edits the new text
// instead of clearing it.
func TestLineSelectAllCtrlVPasteReplaces(t *testing.T) {
	t.Parallel()
	var changed []string
	l := NewLine(testStyles(), LineOptions{Label: "Value:", Input: "old", OnChange: func(s string) { changed = append(changed, s) }})
	l.paste = func() tea.Msg { return pasteMsg("new") }
	l.SelectAll()
	_, cmd := l.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+V started no clipboard read")
	}
	if l.Value() != "old" || !l.HasSelection() || l.SelectedText() != "old" {
		t.Fatalf("Ctrl+V lost the selection: value %q selected %q", l.Value(), l.SelectedText())
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("the Ctrl+V command returned no paste result")
	}
	l.Update(msg)
	if got := l.Value(); got != "new" {
		t.Fatalf("Ctrl+A Ctrl+V paste = %q, want new", got)
	}
	if l.HasSelection() {
		t.Fatal("selection stayed set after the replacement paste")
	}
	if len(changed) != 1 || changed[0] != "new" {
		t.Fatalf("onChange = %v, want only the pasted text", changed)
	}
	l.Update(key("backspace"))
	if got := l.Value(); got != "ne" {
		t.Fatalf("Backspace after replacement = %q, want ne (not a selection clear)", got)
	}
}

// TestLineSelectAllEmptyNoOnChange: selecting an empty field and pressing Backspace or
// Delete clears the selection without reporting a text change.
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
			if l.Value() != "" || l.HasSelection() {
				t.Fatalf("%s left value %q selection %v", tt.name, l.Value(), l.HasSelection())
			}
			if called {
				t.Fatalf("%s on an empty selected prompt called onChange", tt.name)
			}
		})
	}
}

// TestLineSelectAllBackspaceClearsValidationError: clearing a selected range with
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
	if l.HasSelection() {
		t.Fatal("selection stayed set after an arrow cancelled it")
	}
}
