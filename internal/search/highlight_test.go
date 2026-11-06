package search

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestSpans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		text        string
		matches     []Match
		first, last int
		want        []Span
	}{
		{
			name:    "single line match",
			text:    "abc foo",
			matches: []Match{m(0, 4, 0, 7)},
			first:   0, last: 0,
			want: []Span{{Line: 0, Start: 4, End: 7}},
		},
		{
			name:    "match outside the visible rows is skipped",
			text:    "foo\nbar\nfoo",
			matches: []Match{m(0, 0, 0, 3), m(2, 0, 2, 3)},
			first:   1, last: 1,
			want: nil,
		},
		{
			name:    "multi-line match is split per visible line",
			text:    "ab cd\nef gh\nij",
			matches: []Match{m(0, 3, 2, 1)},
			first:   0, last: 2,
			want: []Span{
				{Line: 0, Start: 3, End: 5},
				{Line: 1, Start: 0, End: 5},
				{Line: 2, Start: 0, End: 1},
			},
		},
		{
			name:    "multi-line match clipped to the visible rows",
			text:    "ab cd\nef gh\nij",
			matches: []Match{m(0, 3, 2, 1)},
			first:   1, last: 1,
			want: []Span{{Line: 1, Start: 0, End: 5}},
		},
		{
			name:    "match ending at column zero of the next line has no span there",
			text:    "xx\nyy",
			matches: []Match{m(0, 0, 1, 0)},
			first:   0, last: 1,
			want: []Span{{Line: 0, Start: 0, End: 2}},
		},
		{
			name:    "rows outside the buffer are clipped",
			text:    "foo",
			matches: []Match{m(0, 0, 0, 3)},
			first:   -5, last: 40,
			want: []Span{{Line: 0, Start: 0, End: 3}},
		},
		{
			name:    "reversed rows give nothing",
			text:    "foo",
			matches: []Match{m(0, 0, 0, 3)},
			first:   2, last: 1,
			want: nil,
		},
		{
			name:    "columns count runes",
			text:    "héllo wörld",
			matches: []Match{m(0, 6, 0, 11)},
			first:   0, last: 0,
			want: []Span{{Line: 0, Start: 6, End: 11}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New(tt.text)
			got := Spans(b, tt.matches, tt.first, tt.last)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Spans() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpansFromFind(t *testing.T) {
	t.Parallel()
	b := buffer.New("cat dog\ncat\ndog cat")
	matches, err := Find(b, Query{Text: "cat", CaseSensitive: true})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	got := Spans(b, matches, 1, 2)
	want := []Span{
		{Line: 1, Start: 0, End: 3},
		{Line: 2, Start: 4, End: 7},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Spans() = %v, want %v", got, want)
	}
}

func TestHighlights(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		matches []Match
		line    int
		current *Match
		want    []Highlight
	}{
		{
			name:    "no current match marks nothing",
			text:    "foo bar foo",
			matches: []Match{m(0, 0, 0, 3), m(0, 8, 0, 11)},
			line:    0,
			want: []Highlight{
				{Line: 0, Start: 0, End: 3},
				{Line: 0, Start: 8, End: 11},
			},
		},
		{
			name:    "the current match is marked and the others are not",
			text:    "foo bar foo",
			matches: []Match{m(0, 0, 0, 3), m(0, 8, 0, 11)},
			line:    0,
			current: new(m(0, 8, 0, 11)),
			want: []Highlight{
				{Line: 0, Start: 0, End: 3},
				{Line: 0, Start: 8, End: 11, Current: true},
			},
		},
		{
			name:    "a current match on another line marks nothing here",
			text:    "foo\nfoo",
			matches: []Match{m(0, 0, 0, 3), m(1, 0, 1, 3)},
			line:    0,
			current: new(m(1, 0, 1, 3)),
			want:    []Highlight{{Line: 0, Start: 0, End: 3}},
		},
		{
			name:    "a multi-line current match marks its piece on each line",
			text:    "ab cd\nef gh\nij",
			matches: []Match{m(0, 3, 2, 1)},
			line:    1,
			current: new(m(0, 3, 2, 1)),
			want:    []Highlight{{Line: 1, Start: 0, End: 5, Current: true}},
		},
		{
			name:    "a line without matches gives nothing",
			text:    "foo\nbar",
			matches: []Match{m(0, 0, 0, 3)},
			line:    1,
			want:    nil,
		},
		{
			name:    "a line outside the buffer gives nothing",
			text:    "foo",
			matches: []Match{m(0, 0, 0, 3)},
			line:    5,
			want:    nil,
		},
		{
			name:    "columns count runes",
			text:    "héllo wörld",
			matches: []Match{m(0, 6, 0, 11)},
			line:    0,
			current: new(m(0, 6, 0, 11)),
			want:    []Highlight{{Line: 0, Start: 6, End: 11, Current: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New(tt.text)
			got := Highlights(b, tt.matches, tt.line, tt.current)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Highlights() = %v, want %v", got, tt.want)
			}
		})
	}
}
