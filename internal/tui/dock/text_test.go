package dock

import (
	"testing"
)

func TestLineBreaksBecomeLF(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{name: "LF is kept", in: "a\nb", want: "a\nb"},
		{name: "CRLF becomes LF", in: "a\r\nb", want: "a\nb"},
		{name: "CR becomes LF", in: "a\rb", want: "a\nb"},
		{name: "ESC is kept", in: "a\x1bb", want: "a\x1bb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lineBreaks(tt.in); got != tt.want {
				t.Fatalf("lineBreaks(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPasteTextDropsEscapeSequences(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{name: "plain text is unchanged", in: "hello", want: "hello"},
		{name: "line breaks are LF", in: "a\r\nb\rc", want: "a\nb\nc"},
		{name: "ESC is removed so the text cannot end a bracketed paste", in: "a\x1b[201~b", want: "a[201~b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pasteText(tt.in); got != tt.want {
				t.Fatalf("pasteText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
