package bundled_test

import (
	"strings"
	"testing"
)

func TestViUnknownExCommandsSayWhy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, keys, notice string
	}{
		{"put", ":«put»", "E492: Not an editor command: put"},
		{"normal", ":«normal x»", "E492: Not an editor command: normal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, "abc\n")
			v.keys(tt.keys)
			if got := v.notices(); !strings.Contains(got, tt.notice) {
				t.Errorf("notices = %q, want %q", got, tt.notice)
			}
		})
	}
}

func TestViGlobalCopiesToTheCaretLine(t *testing.T) {
	t.Parallel()
	v := newVi(t, "a\nb\n")
	v.keys(":«g/a/t.»")
	if got := v.text(); got != "a\na\nb\n" {
		t.Errorf("text = %q", got)
	}
}

// A :g command that does not start with a letter or < > used to run as :p and change
// nothing; it now says it is not supported. The Vim abbreviations run like the full names.
func TestViGlobalCommandForms(t *testing.T) {
	t.Parallel()
	unchanged := []string{":«g/x/+1d»", ":«g/x/-1j»", ":«g/x/.,+1d»", ":«g/x/.d»"}
	for _, keys := range unchanged {
		t.Run(keys, func(t *testing.T) {
			t.Parallel()
			const text = "a\nx\nb\n\nx\nc\n"
			v := newVi(t, text)
			v.keys(keys)
			if got := v.text(); got != text {
				t.Errorf("text = %q, want it unchanged", got)
			}
			if got := v.notices(); !strings.Contains(got, "E: :global does not run :") || strings.Contains(got, "matching") {
				t.Errorf("notices = %q, want the E: message", got)
			}
		})
	}
	runViCases(t, []viCase{
		{"co is :copy", "a\nx\nb\n", [2]int{0, 0}, ":«g/x/co$»", "a\nx\nb\nx\n", [2]int{3, 0}, ""},
		{"mo is :move", "a\nx\nb\n", [2]int{0, 0}, ":«g/x/mo0»", "x\na\nb\n", [2]int{0, 0}, ""},
		{"del is :delete", "a\nx\nb\n", [2]int{0, 0}, ":«g/x/del»", "a\nb\n", [2]int{1, 0}, ""},
		{"jo is :join", "a\nx\nb\n", [2]int{0, 0}, ":«g/x/jo»", "a\nx b\n", [2]int{1, 0}, ""},
		{"ya is :yank", "a\nx\nb\n", [2]int{0, 0}, ":«g/x/ya»", "a\nx\nb\n", [2]int{0, 0}, ""},
		{"a bare :g prints", "a\nx\nb\n", [2]int{0, 0}, ":«g/x/»", "a\nx\nb\n", [2]int{1, 0}, ""},
	})
}

// The caret after :g/x/j and the ex line commands, as Vim leaves it.
func TestViExCaretAfterLineCommands(t *testing.T) {
	t.Parallel()
	runViCases(t, []viCase{
		{":g/x/j on the last joined line", "a\nx\nb\nx\nc\n", [2]int{0, 0}, ":«g/x/j»", "a\nx b\nx c\n", [2]int{2, 0}, ""},
		{":g/x/j swallows the next match", "x\nx\nx\n", [2]int{0, 0}, ":«g/x/j»", "x x\nx\n", [2]int{1, 0}, ""},
		{":j goes to the first non-blank", "  a\nb\n  c\n", [2]int{2, 0}, ":«1,2j»", "  a b\n  c\n", [2]int{0, 2}, ""},
		{":> goes to the last line of the range", "a\nb\nc\nd\n", [2]int{0, 0}, ":«2,3>»", "a\n    b\n    c\nd\n", [2]int{2, 4}, ""},
		{":< goes to the last line of the range", "a\n\tb\n\tc\nd\n", [2]int{0, 0}, ":«2,3<»", "a\nb\nc\nd\n", [2]int{2, 0}, ""},
	})
}
