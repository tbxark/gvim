package display

import (
	"slices"
	"testing"
)

func TestIndentGuides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		tabWidth int
		want     []int
	}{
		{"empty line", "", 4, nil},
		{"no indent", "x", 4, nil},
		{"indent shorter than one level", "  x", 4, nil},
		{"one level of spaces", "    x", 4, []int{0}},
		{"two levels of spaces", "        x", 4, []int{0, 4}},
		{"one tab", "\tx", 4, []int{0}},
		{"two tabs", "\t\tx", 4, []int{0, 4}},
		{"tab counts to next tab stop after spaces", "    \tx", 4, []int{0, 4}},
		{"space then tab reaches one level", " \tx", 4, []int{0}},
		{"wide tab stop", "\tx", 8, []int{0}},
		{"whitespace-only line uses its width", "    ", 4, []int{0}},
		{"multi-byte text after indent", "    日本", 4, []int{0}},
		{"multi-byte text is not indent", "日    本", 4, nil},
		{"tab width below one is treated as one", "  x", 0, []int{0, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IndentGuides(tc.line, tc.tabWidth)
			if !slices.Equal(got, tc.want) {
				t.Errorf("IndentGuides(%q, %d) = %v, want %v", tc.line, tc.tabWidth, got, tc.want)
			}
		})
	}
}

func TestIndentWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		tabWidth int
		want     int
	}{
		{"empty line", "", 4, 0},
		{"no indent", "x", 4, 0},
		{"spaces", "   x", 4, 3},
		{"tab", "\tx", 4, 4},
		{"two tabs", "\t\tx", 4, 8},
		{"tab after spaces stops at next tab stop", "  \tx", 4, 4},
		{"wide tab stop", "\tx", 8, 8},
		{"width stops at first non-indent rune", "  日本  ", 4, 2},
		{"whitespace-only line", "  \t", 4, 4},
		{"tab width below one is treated as one", "\tx", 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IndentWidth(tc.line, tc.tabWidth); got != tc.want {
				t.Errorf("IndentWidth(%q, %d) = %d, want %d", tc.line, tc.tabWidth, got, tc.want)
			}
		})
	}
}

func TestVisibleWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		tabWidth int
		want     string
	}{
		{"empty line", "", 4, ""},
		{"plain text unchanged", "abc", 4, "abc"},
		{"space becomes middle dot", "a b", 4, "a·b"},
		{"run of spaces", "  ", 4, "··"},
		{"tab at column zero", "\t", 4, "→   "},
		{"tab moves to next stop", "a\tb", 4, "a→  b"},
		{"tab on a stop takes a full tab width", "abcd\te", 4, "abcd→   e"},
		{"two tabs", "\t\t", 4, "→   →   "},
		{"wide tab stop", "\t", 8, "→       "},
		{"tab width one is only the arrow", "\t", 1, "→"},
		{"tab width below one is treated as one", "\t", 0, "→"},
		{"wide character moves tab stop by two", "日\tx", 4, "日→ x"},
		{"wide character before space", "日 本", 4, "日·本"},
		{"precomposed letter is one column", "é\t", 4, "é→  "},
		{"non-breaking space is unchanged", "a\u00a0b", 4, "a\u00a0b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := VisibleWhitespace(tc.line, tc.tabWidth); got != tc.want {
				t.Errorf("VisibleWhitespace(%q, %d) = %q, want %q", tc.line, tc.tabWidth, got, tc.want)
			}
		})
	}
}

func TestOccurrences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		word  string
		want  []Span
	}{
		{"no lines", nil, "foo", nil},
		{"empty word matches nothing", []string{"foo foo"}, "", nil},
		{"whole line is a match", []string{"foo"}, "foo", []Span{{0, 0, 3}}},
		{"two matches on a line", []string{"foo foo"}, "foo", []Span{{0, 0, 3}, {0, 4, 7}}},
		{"part of a longer word is skipped", []string{"foobar foo", "foo_bar", "_foo", "a foo."}, "foo",
			[]Span{{0, 7, 10}, {3, 2, 5}}},
		{"digit next to word is not a boundary", []string{"1foo foo2"}, "foo", nil},
		{"letter next to word is not a boundary", []string{"éfoo"}, "foo", nil},
		{"hyphen is a boundary", []string{"a-foo"}, "foo", []Span{{0, 2, 5}}},
		{"case must match", []string{"Foo"}, "foo", nil},
		{"columns are runes not bytes", []string{"日本 foo"}, "foo", []Span{{0, 3, 6}}},
		{"multi-byte whole word", []string{"日本語 日本"}, "日本", []Span{{0, 4, 6}}},
		{"overlapping candidates are rejected", []string{"aaaa"}, "aa", nil},
		{"adjacent words are separate matches", []string{"aa aa"}, "aa", []Span{{0, 0, 2}, {0, 3, 5}}},
		{"word with a space", []string{"x a b y"}, "a b", []Span{{0, 2, 5}}},
		{"matches on several lines", []string{"", "foo", "x foo"}, "foo", []Span{{1, 0, 3}, {2, 2, 5}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Occurrences(tc.lines, tc.word)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Occurrences(%q, %q) = %v, want %v", tc.lines, tc.word, got, tc.want)
			}
		})
	}
}
