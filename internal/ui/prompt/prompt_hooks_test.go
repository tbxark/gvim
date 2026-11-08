package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLineOnChangeSeesEveryEditedText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts LineOptions
		do   func(l *Line)
		want []string
	}{
		{"typing", LineOptions{}, func(l *Line) { typeText(l, "ab") }, []string{"a", "ab"}},
		{"backspace", LineOptions{Input: "xy"}, func(l *Line) { l.Update(key("backspace")) }, []string{"x"}},
		{"a key that changes nothing", LineOptions{Input: "xy"}, func(l *Line) { l.Update(key("left")) }, nil},
		{"enter is not a change", LineOptions{Input: "xy"}, func(l *Line) { l.Update(key("enter")) }, nil},
		{"a paste", LineOptions{}, func(l *Line) { l.Update(tea.PasteMsg{Content: "pasted"}) }, []string{"pasted"}},
		{"the first key replaces a prefill", LineOptions{Input: "old", SelectAll: true}, func(l *Line) { typeText(l, "n") }, []string{"n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			tt.opts.OnChange = func(s string) { got = append(got, s) }
			l := NewLine(testStyles(), tt.opts)
			tt.do(l)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("OnChange saw %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfirmWithLabelsAndDefault(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Replace?", "tag", ConfirmOptions{Yes: "Do it", No: "Keep", DefaultYes: true})
	rows := draw(c, 60, 3)
	if got := strings.TrimRight(rows[2], " "); got != " Replace? [ Do it ] [ Keep ]" {
		t.Fatalf("row = %q", got)
	}
	// The focus starts on Yes, so Enter answers it.
	_, cmd := c.Update(key("enter"))
	if d, ok := emitted(cmd); !ok || d.Outcome != Yes || d.Tag != "tag" {
		t.Fatalf("done = %+v %v", d, ok)
	}
	// NewConfirm is the plain form.
	plain := NewConfirm(testStyles(), "Q?", "t")
	if got := strings.TrimRight(draw(plain, 60, 3)[2], " "); got != " Q? [ Yes ] [ No ]" {
		t.Fatalf("plain row = %q", got)
	}
}
