package ui

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
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

func TestSelectedTextFollowsTheSelectionOrTheCursorLine(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		selected bool
		anchor   buffer.Pos
		head     buffer.Pos
		cursor   buffer.Pos
		want     string
	}{
		{
			name:     "one line",
			lines:    []string{"abcd"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 1},
			head:     buffer.Pos{Line: 0, Col: 3},
			want:     "bc",
		},
		{
			name:     "reversed ends select the same text",
			lines:    []string{"abcd"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 3},
			head:     buffer.Pos{Line: 0, Col: 1},
			want:     "bc",
		},
		{
			name:     "two lines join with a line break",
			lines:    []string{"abcd", "efgh"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 2},
			head:     buffer.Pos{Line: 1, Col: 2},
			want:     "cd\nef",
		},
		{
			name:     "an empty selection is the cursor line",
			lines:    []string{"abcd", "efgh"},
			selected: true,
			anchor:   buffer.Pos{Line: 1, Col: 1},
			head:     buffer.Pos{Line: 1, Col: 1},
			cursor:   buffer.Pos{Line: 1, Col: 1},
			want:     "efgh",
		},
		{
			name:   "no selection is the cursor line",
			lines:  []string{"abcd", "efgh"},
			cursor: buffer.Pos{Line: 0, Col: 2},
			want:   "abcd",
		},
		{
			name:     "columns count runes, not bytes",
			lines:    []string{"héllo"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 1},
			head:     buffer.Pos{Line: 0, Col: 3},
			want:     "él",
		},
		{
			name:     "a head past the end of its line is clamped",
			lines:    []string{"ab", "cd"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 1},
			head:     buffer.Pos{Line: 1, Col: 9},
			want:     "b\ncd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor(tt.lines...)
			ed.doc.cursor = tt.cursor
			doc := selectionDoc{fakeDoc: ed.doc, anchor: tt.anchor, head: tt.head, selected: tt.selected}
			if got := selectedText(doc); got != tt.want {
				t.Fatalf("selectedText() = %q, want %q", got, tt.want)
			}
		})
	}
}
