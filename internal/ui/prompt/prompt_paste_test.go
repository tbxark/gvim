package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLinePasteInsertsAtTheCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts LineOptions
		keys string
		msg  string
		want string
	}{
		{name: "into an empty prompt", msg: "main.go", want: "main.go"},
		{name: "after typed text", keys: "ab", msg: "cd", want: "abcd"},
		{name: "line breaks become spaces", msg: "a\r\nb\nc", want: "a b c"},
		{name: "the first paste replaces a selected prefill", opts: LineOptions{Input: "old.txt", SelectAll: true}, msg: "new", want: "new"},
		{name: "a paste keeps a prefill that is not selected", opts: LineOptions{Input: "dir/"}, msg: "x", want: "dir/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := NewLine(testStyles(), tt.opts)
			typeText(l, tt.keys)
			_, cmd := l.Update(tea.PasteMsg{Content: tt.msg})
			if cmd != nil {
				t.Fatal("a paste started a command")
			}
			if got := l.Value(); got != tt.want {
				t.Fatalf("value = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineCtrlVReturnsTheClipboardRead(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{})
	_, cmd := l.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+V dropped the clipboard read")
	}
}

func TestLinePasteClearsTheRejection(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Validate: func(s string) error {
		if s == "" {
			return errEmpty
		}
		return nil
	}})
	l.Update(key("enter"))
	if l.errText == "" {
		t.Fatal("Enter on an empty input was not rejected")
	}
	l.Update(tea.PasteMsg{Content: "x"})
	if l.errText != "" {
		t.Fatalf("errText = %q after a paste, want none", l.errText)
	}
}

type emptyError struct{}

func (emptyError) Error() string { return "empty" }

var errEmpty error = emptyError{}
