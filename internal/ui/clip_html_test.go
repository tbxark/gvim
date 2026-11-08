package ui

import (
	"errors"
	"strings"
	"testing"
)

// htmlClipboard is a system clipboard that also takes rich text.
type htmlClipboard struct {
	recClipboard
	html *[]string
	err  error
}

func (c htmlClipboard) WriteHTML(html, text string) error {
	*c.html = append(*c.html, html)
	return c.err
}

func TestCopyWithFormattingWritesHTMLAndTellsTheTheme(t *testing.T) {
	var native, rich []string
	ed := &copyEditor{docEditor: newDocEditor("/ws/a.txt", false), text: "a<b"}
	m := clipModel(t, ed, htmlClipboard{recClipboard: recClipboard{writes: &native}, html: &rich})
	cmd := m.dispatch(Command{Action: "copy_with_theme", Args: map[string]any{"theme": "dracula"}})
	msgs := collect(cmd)
	if len(rich) != 1 || !strings.HasPrefix(rich[0], "<pre ") || !strings.Contains(rich[0], "a&lt;b") {
		t.Fatalf("html writes = %q, want one <pre> block holding the escaped text", rich)
	}
	if m.notice != "Copied with formatting (dracula)" {
		t.Errorf("notice = %q", m.notice)
	}
	if len(native) != 0 {
		t.Errorf("the plain native write %q runs besides the html one, which would replace it", native)
	}
	for _, msg := range msgs {
		if h, ok := msg.(htmlCopiedMsg); ok && h.err != nil {
			t.Errorf("write error %v", h.err)
		}
	}
}

func TestCopyWithFormattingFallsBackToPlainText(t *testing.T) {
	tests := []struct {
		name string
		cb   func(native, rich *[]string) ClipboardReader
		args map[string]any
		want string // a substring of the notice
	}{
		{"no rich clipboard", func(n, _ *[]string) ClipboardReader { return recClipboard{writes: n} }, nil, "plain text"},
		{"unknown theme", func(n, r *[]string) ClipboardReader {
			return htmlClipboard{recClipboard: recClipboard{writes: n}, html: r}
		}, map[string]any{"theme": "no-such-theme"}, "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var native, rich []string
			ed := &copyEditor{docEditor: newDocEditor("/ws/a.txt", false), text: "abc"}
			m := clipModel(t, ed, tt.cb(&native, &rich))
			m.dispatch(Command{Action: "copy_with_theme", Args: tt.args})
			if !strings.Contains(m.notice, tt.want) {
				t.Errorf("notice = %q, want it to mention %q", m.notice, tt.want)
			}
			if len(rich) != 0 {
				t.Errorf("html written: %q", rich)
			}
		})
	}
}

func TestFailedHTMLWriteSaysPlainText(t *testing.T) {
	var native, rich []string
	ed := &copyEditor{docEditor: newDocEditor("/ws/a.txt", false), text: "abc"}
	m := clipModel(t, ed, htmlClipboard{recClipboard: recClipboard{writes: &native}, html: &rich, err: errors.New("no tool")})
	cmd := m.dispatch(Command{Action: "copy_with_theme"})
	for _, msg := range collect(cmd) {
		m.Update(msg)
	}
	if !strings.Contains(m.notice, "plain text") || !strings.Contains(m.notice, "no tool") {
		t.Errorf("notice = %q, want the failure and plain text", m.notice)
	}
}
