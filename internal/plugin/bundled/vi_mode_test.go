package bundled_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// viCase is one scripted editing session: the text and the caret to start from, the keys,
// and the text, caret and vi mode that must result.
type viCase struct {
	name   string
	text   string
	at     [2]int
	keys   string
	want   string
	cursor [2]int
	mode   string // "" means vi_normal
}

func runViCases(t *testing.T, cases []viCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, tt.text).at(tt.at[0], tt.at[1])
			v.keys(tt.keys)
			if got := v.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if got, want := v.cursor(), (pluginapi.Pos{Line: tt.cursor[0], Col: tt.cursor[1]}); got != want {
				t.Errorf("caret = %+v, want %+v", got, want)
			}
			wantMode := tt.mode
			if wantMode == "" {
				wantMode = "vi_normal"
			}
			if got := v.mode(); got != wantMode {
				t.Errorf("mode = %q, want %q", got, wantMode)
			}
		})
	}
}

func TestViMotions(t *testing.T) {
	t.Parallel()
	const words = "hello world foo"
	runViCases(t, []viCase{
		{"w", words, [2]int{0, 0}, "w", words, [2]int{0, 6}, ""},
		{"2w", words, [2]int{0, 0}, "2w", words, [2]int{0, 12}, ""},
		{"w stops on the last character of the buffer", words, [2]int{0, 0}, "5w", words, [2]int{0, 14}, ""},
		{"w over a punctuation run", "foo.bar baz", [2]int{0, 0}, "w", "foo.bar baz", [2]int{0, 3}, ""},
		{"W over a punctuation run", "foo.bar baz", [2]int{0, 0}, "W", "foo.bar baz", [2]int{0, 8}, ""},
		{"w stops on an empty line", "foo\n\nbar", [2]int{0, 0}, "w", "foo\n\nbar", [2]int{1, 0}, ""},
		{"w goes to the next line", "foo\nbar", [2]int{0, 0}, "w", "foo\nbar", [2]int{1, 0}, ""},
		{"e", words, [2]int{0, 0}, "e", words, [2]int{0, 4}, ""},
		{"e from the end of a word", words, [2]int{0, 4}, "e", words, [2]int{0, 10}, ""},
		{"b", words, [2]int{0, 12}, "b", words, [2]int{0, 6}, ""},
		{"b from the middle of a word", words, [2]int{0, 8}, "b", words, [2]int{0, 6}, ""},
		{"2b", words, [2]int{0, 12}, "2b", words, [2]int{0, 0}, ""},
		{"ge", words, [2]int{0, 6}, "ge", words, [2]int{0, 4}, ""},
		{"0 and $", words, [2]int{0, 5}, "$", words, [2]int{0, 14}, ""},
		{"0", words, [2]int{0, 5}, "0", words, [2]int{0, 0}, ""},
		{"^", "   indented", [2]int{0, 8}, "^", "   indented", [2]int{0, 3}, ""},
		{"g_", "text   ", [2]int{0, 0}, "g_", "text   ", [2]int{0, 3}, ""},
		{"f", words, [2]int{0, 0}, "fo", words, [2]int{0, 4}, ""},
		{"2f", words, [2]int{0, 0}, "2fo", words, [2]int{0, 7}, ""},
		{"t", words, [2]int{0, 0}, "to", words, [2]int{0, 3}, ""},
		{"F", words, [2]int{0, 14}, "Fo", words, [2]int{0, 13}, ""},
		{"T", words, [2]int{0, 14}, "To", words, [2]int{0, 14}, ""},
		{"f that fails stays", words, [2]int{0, 0}, "fz", words, [2]int{0, 0}, ""},
		{"; repeats f", words, [2]int{0, 0}, "fo;", words, [2]int{0, 7}, ""},
		{", reverses f", words, [2]int{0, 0}, "fo;,", words, [2]int{0, 4}, ""},
		{"; after t skips the adjacent match", "a,b,c,d", [2]int{0, 0}, "t,;", "a,b,c,d", [2]int{0, 2}, ""},
		{"j keeps the column", "abcdef\nghijkl\nmn", [2]int{0, 4}, "j", "abcdef\nghijkl\nmn", [2]int{1, 4}, ""},
		{"j onto a short line then on", "abcdef\nxy\nghijkl", [2]int{0, 4}, "jj", "abcdef\nxy\nghijkl", [2]int{2, 4}, ""},
		{"k", "abc\ndef", [2]int{1, 2}, "k", "abc\ndef", [2]int{0, 2}, ""},
		{"3j", "a\nb\nc\nd\ne", [2]int{0, 0}, "3j", "a\nb\nc\nd\ne", [2]int{3, 0}, ""},
		{"j on the last line stays", "a\nb", [2]int{1, 0}, "j", "a\nb", [2]int{1, 0}, ""},
		{"h", "abc", [2]int{0, 2}, "h", "abc", [2]int{0, 1}, ""},
		{"h at the start stays", "abc", [2]int{0, 0}, "h", "abc", [2]int{0, 0}, ""},
		{"l", "abc", [2]int{0, 0}, "l", "abc", [2]int{0, 1}, ""},
		{"l stops on the last character", "abc", [2]int{0, 1}, "5l", "abc", [2]int{0, 2}, ""},
		{"space wraps to the next line", "ab\ncd", [2]int{0, 1}, "<Space>", "ab\ncd", [2]int{1, 0}, ""},
		{"backspace wraps to the previous line", "ab\ncd", [2]int{1, 0}, "<BS>", "ab\ncd", [2]int{0, 1}, ""},
		{"gg", "a\n  b\nc", [2]int{2, 0}, "gg", "a\n  b\nc", [2]int{0, 0}, ""},
		{"G", "a\n  b\n  c", [2]int{0, 0}, "G", "a\n  b\n  c", [2]int{2, 2}, ""},
		{"2G", "a\n  b\n  c", [2]int{0, 0}, "2G", "a\n  b\n  c", [2]int{1, 2}, ""},
		{"3gg", "a\nb\nc\nd", [2]int{0, 0}, "3gg", "a\nb\nc\nd", [2]int{2, 0}, ""},
		{"+ and enter", "a\n b\n c", [2]int{0, 0}, "+", "a\n b\n c", [2]int{1, 1}, ""},
		{"-", "a\n b\n c", [2]int{2, 1}, "-", "a\n b\n c", [2]int{1, 1}, ""},
		{"} to the next empty line", "a\nb\n\nc", [2]int{0, 0}, "}", "a\nb\n\nc", [2]int{2, 0}, ""},
		{"{ to the previous empty line", "a\n\nb\nc", [2]int{3, 0}, "{", "a\n\nb\nc", [2]int{1, 0}, ""},
		{"} at the end of the buffer", "a\nb", [2]int{0, 0}, "}", "a\nb", [2]int{1, 0}, ""},
		{"% from an opener", "x(a[b]c)", [2]int{0, 1}, "%", "x(a[b]c)", [2]int{0, 7}, ""},
		{"% from a closer", "x(a[b]c)", [2]int{0, 7}, "%", "x(a[b]c)", [2]int{0, 1}, ""},
		{"% finds the next bracket on the line", "x(a)", [2]int{0, 0}, "%", "x(a)", [2]int{0, 3}, ""},
		{"% across lines", "{\n  a\n}", [2]int{0, 0}, "%", "{\n  a\n}", [2]int{2, 0}, ""},
		{"| column", "abcdef", [2]int{0, 0}, "4|", "abcdef", [2]int{0, 3}, ""},
		{"multi-byte l", "héllo", [2]int{0, 0}, "ll", "héllo", [2]int{0, 3}, ""},
		{"multi-byte w", "héllo wörld", [2]int{0, 0}, "w", "héllo wörld", [2]int{0, 7}, ""},
		{"multi-byte $", "héllo", [2]int{0, 0}, "$", "héllo", [2]int{0, 5}, ""},
		{"arrow right", "abc", [2]int{0, 0}, "<Right>", "abc", [2]int{0, 1}, ""},
		{"arrow down", "abc\ndef", [2]int{0, 1}, "<Down>", "abc\ndef", [2]int{1, 1}, ""},
		{"end key", "abc", [2]int{0, 0}, "<End>", "abc", [2]int{0, 2}, ""},
		{"search finds the next match", "foo bar foo baz foo", [2]int{0, 0}, "/«foo»", "foo bar foo baz foo", [2]int{0, 8}, ""},
	})
}
