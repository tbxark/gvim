package search

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
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
			name: "empty regex matches are kept",
			text: "abc",
			q:    Query{Text: "x*", Regex: true},
			want: []Match{m(0, 0, 0, 0), m(0, 1, 0, 1), m(0, 2, 0, 2), m(0, 3, 0, 3)},
		},
		{
			name: "caret anchors every line",
			text: "a\nb",
			q:    Query{Text: "^", Regex: true},
			want: []Match{m(0, 0, 0, 0), m(1, 0, 1, 0)},
		},
		{
			name: "dollar anchors every line end",
			text: "ab\ncd",
			q:    Query{Text: "$", Regex: true},
			want: []Match{m(0, 2, 0, 2), m(1, 2, 1, 2)},
		},
		{
			name: "empty match next to a match is dropped",
			text: "aab",
			q:    Query{Text: "a*", Regex: true},
			want: []Match{m(0, 0, 0, 2), m(0, 3, 0, 3)},
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

func TestCompileKeepsTheCompiledQuery(t *testing.T) {
	q := Query{Text: "needle[0-9]+", Regex: true, CaseSensitive: true}
	a, err := compile(q)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := compile(q)
	if a != b {
		t.Error("a second compile of the same query built the regexp again")
	}
	other := q
	other.WholeWord = true
	if c, _ := compile(other); c == a {
		t.Error("a query that differs in a toggle shared the regexp")
	}
	// Past the size of the cache the oldest query goes, and compile still answers.
	for i := range 2 * compileCacheSize {
		if _, err := compile(Query{Text: strings.Repeat("x", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	if c, err := compile(q); err != nil || c == nil {
		t.Errorf("compile after the cache turned over = %v, %v", c, err)
	}
}

func TestCompileErrorOfALongQueryIsShort(t *testing.T) {
	tests := []struct {
		name      string
		q         Query
		quoted    bool
		maxLength int
	}{
		{"short pattern is quoted", Query{Text: "(abc", Regex: true}, true, 200},
		{"long pattern is not", Query{Text: strings.Repeat("(", 5000), Regex: true}, false, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(tc.q)
			if err == nil {
				t.Fatal("the pattern compiled")
			}
			if got := strings.Contains(err.Error(), tc.q.Text); got != tc.quoted {
				t.Errorf("error quotes the pattern = %v, want %v", got, tc.quoted)
			}
			if len(err.Error()) > tc.maxLength {
				t.Errorf("error is %d bytes, want at most %d", len(err.Error()), tc.maxLength)
			}
			if !strings.Contains(err.Error(), "invalid pattern") {
				t.Errorf("error = %q, want it to say the pattern is invalid", err)
			}
		})
	}
}
