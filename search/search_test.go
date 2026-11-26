package search

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

// Find on one very long line with a match in every few runes must count each rune
// of the line once, not once per match: counting from the line's start for every
// match takes minutes on a minified file.
func TestFindManyMatchesOnOneLine(t *testing.T) {
	if testing.Short() {
		t.Skip("wall-clock budget: the non-race budget job runs it")
	}
	t.Parallel()
	const pairs = 250_000
	b := buffer.New(strings.Repeat("aé", pairs))
	start := time.Now()
	ms, err := Find(b, Query{Text: "é"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != pairs {
		t.Fatalf("matches = %d, want %d", len(ms), pairs)
	}
	if last := ms[len(ms)-1]; last != m(0, 2*pairs-1, 0, 2*pairs) {
		t.Fatalf("last match = %v", last)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Find took %v: positions are counted from the line start for every match", d)
	}
}

func TestCompiledQueryFindStringMatchesFind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		q    Query
	}{
		{"empty text zero-width regex ^", "", Query{Text: "^", Regex: true}},
		{"empty text zero-width regex $", "", Query{Text: "$", Regex: true}},
		{"empty text literal", "", Query{Text: "foo"}},
		{"case-insensitive literal", "Foo bar FOO baz", Query{Text: "foo"}},
		{"case-sensitive literal", "Foo bar FOO foo", Query{Text: "foo", CaseSensitive: true}},
		{"whole word utf8", "café café_2 Café", Query{Text: "café", WholeWord: true}},
		{"regex multiline", "alpha 1\nbeta 22\n", Query{Text: `[a-z]+\s+\d+`, Regex: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := Find(buffer.New(tc.text), tc.q)
			if err != nil {
				t.Fatal(err)
			}
			cq, err := CompileQuery(tc.q)
			if err != nil {
				t.Fatal(err)
			}
			got := cq.FindString(tc.text)
			if !slices.Equal(got, want) {
				t.Fatalf("FindString = %v, want %v", got, want)
			}
		})
	}

	cq, err := CompileQuery(Query{Text: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				ms := cq.FindString("one FOO two foo")
				if len(ms) != 2 {
					t.Errorf("concurrent FindString len = %d, want 2", len(ms))
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestCompiledQueryFindStringLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		text  string
		q     Query
		limit int
	}{
		{"zero limit", "foo foo foo", Query{Text: "foo"}, 0},
		{"negative limit", "foo foo foo", Query{Text: "foo"}, -1},
		{"literal capped", "foo FOO foo foo", Query{Text: "foo"}, 2},
		{"case sensitive capped", "foo FOO foo foo", Query{Text: "foo", CaseSensitive: true}, 2},
		{"literal whole word capped", "food foo xfoo foo foo", Query{Text: "foo", WholeWord: true}, 2},
		{"regex capped", "a1 a2 a3 a4", Query{Text: `a\d`, Regex: true}, 2},
		{"regex whole word capped after rejected matches", "xa1 ya2 a3 za4 a5 a6 a7", Query{Text: `a\d`, Regex: true, WholeWord: true}, 2},
		{"regex limit larger than matches", "a1 a2", Query{Text: `a\d`, Regex: true, WholeWord: true}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cq, err := CompileQuery(tc.q)
			if err != nil {
				t.Fatal(err)
			}
			all := cq.FindString(tc.text)
			want := all
			if tc.limit <= 0 {
				want = nil
			} else if len(want) > tc.limit {
				want = want[:tc.limit]
			}
			got := cq.FindStringLimit(tc.text, tc.limit)
			if !slices.Equal(got, want) {
				t.Fatalf("FindStringLimit(%d) = %v, want %v", tc.limit, got, want)
			}
		})
	}
	var nilCQ *CompiledQuery
	if got := nilCQ.FindStringLimit("foo", 1); got != nil {
		t.Fatalf("nil CompiledQuery FindStringLimit = %v, want nil", got)
	}
}
