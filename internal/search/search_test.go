package search

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// m is a Match from line and column pairs, for readable expectations.
func m(sl, sc, el, ec int) Match {
	return Match{Start: buffer.Pos{Line: sl, Col: sc}, End: buffer.Pos{Line: el, Col: ec}}
}

func TestFind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		q    Query
		want []Match
	}{
		{
			name: "literal escapes metacharacters",
			text: "a.b axb a.b",
			q:    Query{Text: "a.b", CaseSensitive: true},
			want: []Match{m(0, 0, 0, 3), m(0, 8, 0, 11)},
		},
		{
			name: "case insensitive by default",
			text: "Foo foo FOO",
			q:    Query{Text: "foo"},
			want: []Match{m(0, 0, 0, 3), m(0, 4, 0, 7), m(0, 8, 0, 11)},
		},
		{
			name: "case sensitive",
			text: "Foo foo FOO",
			q:    Query{Text: "foo", CaseSensitive: true},
			want: []Match{m(0, 4, 0, 7)},
		},
		{
			name: "whole word skips substrings",
			text: "cat concat cat.",
			q:    Query{Text: "cat", CaseSensitive: true, WholeWord: true},
			want: []Match{m(0, 0, 0, 3), m(0, 11, 0, 14)},
		},
		{
			name: "whole word treats underscore as a word character",
			text: "foo_bar foo",
			q:    Query{Text: "foo", WholeWord: true},
			want: []Match{m(0, 8, 0, 11)},
		},
		{
			name: "whole word treats non-ASCII letters as word characters",
			text: "café caf",
			q:    Query{Text: "caf", WholeWord: true},
			want: []Match{m(0, 5, 0, 8)},
		},
		{
			name: "whole word with punctuation at the match edge",
			text: "-foo x-foo",
			q:    Query{Text: "-foo", WholeWord: true},
			want: []Match{m(0, 6, 0, 10)},
		},
		{
			name: "whole word finds a match overlapping a rejected candidate",
			text: "Xa a a Y",
			q:    Query{Text: "a a", WholeWord: true},
			want: []Match{m(0, 3, 0, 6)},
		},
		{
			name: "regex",
			text: "foo1 foo22 bar",
			q:    Query{Text: `foo\d+`, Regex: true},
			want: []Match{m(0, 0, 0, 4), m(0, 5, 0, 10)},
		},
		{
			name: "regex whole word",
			text: "foo1 foo22 foo",
			q:    Query{Text: `foo\d+`, Regex: true, WholeWord: true},
			want: []Match{m(0, 0, 0, 4), m(0, 5, 0, 10)},
		},
		{
			name: "regex anchors match at line breaks",
			text: "a\nab",
			q:    Query{Text: "^a", Regex: true, CaseSensitive: true},
			want: []Match{m(0, 0, 0, 1), m(1, 0, 1, 1)},
		},
		{
			name: "literal across a line break",
			text: "ab\ncd",
			q:    Query{Text: "b\nc", CaseSensitive: true},
			want: []Match{m(0, 1, 1, 1)},
		},
		{
			name: "columns count runes",
			text: "héllo wörld",
			q:    Query{Text: "wörld", CaseSensitive: true},
			want: []Match{m(0, 6, 0, 11)},
		},
		{
			name: "empty query matches nothing",
			text: "abc",
			q:    Query{},
			want: nil,
		},
		{
			name: "empty regex match is skipped",
			text: "abc",
			q:    Query{Text: "x*", Regex: true},
			want: nil,
		},
		{
			name: "zero matches",
			text: "abc",
			q:    Query{Text: "xyz"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Find(buffer.New(tt.text), tt.q)
			if err != nil {
				t.Fatalf("Find() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Find() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindInvalidRegex(t *testing.T) {
	t.Parallel()
	got, err := Find(buffer.New("abc"), Query{Text: "(", Regex: true})
	if err == nil {
		t.Fatalf("Find() = %v, want an error for an unclosed group", got)
	}
	if got != nil {
		t.Fatalf("Find() matches = %v, want none on error", got)
	}
}

func TestFindLiteralParenthesis(t *testing.T) {
	t.Parallel()
	// "(" is not a valid regex, but as literal text it is an ordinary character.
	got, err := Find(buffer.New("f(x)"), Query{Text: "("})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if want := []Match{m(0, 1, 0, 2)}; !slices.Equal(got, want) {
		t.Fatalf("Find() = %v, want %v", got, want)
	}
}
