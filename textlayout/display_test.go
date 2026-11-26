package textlayout

import (
	"slices"
	"testing"
)

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
