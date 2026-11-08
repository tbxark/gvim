package diff

import (
	"slices"
	"testing"
	"unicode/utf8"
)

func TestLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b []string
		want []Hunk
	}{
		{"both empty", nil, nil, nil},
		{"equal texts", []string{"x", "y"}, []string{"x", "y"}, nil},
		{"insert into empty", nil, []string{"x"},
			[]Hunk{{Kind: Insert, OldStart: 0, OldLines: 0, NewStart: 0, NewLines: 1}}},
		{"insert at end", []string{"x"}, []string{"x", "y"},
			[]Hunk{{Kind: Insert, OldStart: 1, OldLines: 0, NewStart: 1, NewLines: 1}}},
		{"delete everything", []string{"x", "y"}, nil,
			[]Hunk{{Kind: Delete, OldStart: 0, OldLines: 2, NewStart: 0, NewLines: 0}}},
		{"delete from middle", []string{"a", "b", "c"}, []string{"a", "c"},
			[]Hunk{{Kind: Delete, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 0}}},
		{"insert into middle", []string{"a", "c"}, []string{"a", "b", "c"},
			[]Hunk{{Kind: Insert, OldStart: 1, OldLines: 0, NewStart: 1, NewLines: 1}}},
		{"change one line", []string{"a", "b", "c"}, []string{"a", "X", "c"},
			[]Hunk{{Kind: Change, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1}}},
		{"change two lines", []string{"a", "b", "c", "d"}, []string{"a", "X", "Y", "d"},
			[]Hunk{{Kind: Change, OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 2}}},
		{"two lines replaced by one", []string{"A", "B"}, []string{"C"},
			[]Hunk{{Kind: Change, OldStart: 0, OldLines: 2, NewStart: 0, NewLines: 1}}},
		{"two separate hunks", []string{"1", "2", "3", "4", "5"}, []string{"1", "X", "3", "4", "Y"},
			[]Hunk{
				{Kind: Change, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1},
				{Kind: Change, OldStart: 4, OldLines: 1, NewStart: 4, NewLines: 1},
			}},
		{"empty line is a line", []string{"", "x"}, []string{"x"},
			[]Hunk{{Kind: Delete, OldStart: 0, OldLines: 1, NewStart: 0, NewLines: 0}}},
		{"empty line against no line", []string{""}, nil,
			[]Hunk{{Kind: Delete, OldStart: 0, OldLines: 1, NewStart: 0, NewLines: 0}}},
		{"no line against empty line", nil, []string{""},
			[]Hunk{{Kind: Insert, OldStart: 0, OldLines: 0, NewStart: 0, NewLines: 1}}},
		{"tab and spaces differ", []string{"\tx"}, []string{"    x"},
			[]Hunk{{Kind: Change, OldStart: 0, OldLines: 1, NewStart: 0, NewLines: 1}}},
		{"multi-byte change", []string{"café", "日本"}, []string{"café", "日本語"},
			[]Hunk{{Kind: Change, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1}}},
		{"duplicate lines delete the last one", []string{"x", "y", "x"}, []string{"x", "y"},
			[]Hunk{{Kind: Delete, OldStart: 2, OldLines: 1, NewStart: 2, NewLines: 0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Lines(tc.a, tc.b)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Lines(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// hunks used by the navigation tests: a change on new line 1, an insert on new
// lines 4 and 5, and a delete that leaves new line 7 as its position.
var navHunks = []Hunk{
	{Kind: Change, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1},
	{Kind: Insert, OldStart: 4, OldLines: 0, NewStart: 4, NewLines: 2},
	{Kind: Delete, OldStart: 7, OldLines: 1, NewStart: 7, NewLines: 0},
}

func TestNextHunk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		hunks []Hunk
		line  int
		want  int
		ok    bool
	}{
		{"no hunks", nil, 0, 0, false},
		{"before the first hunk", navHunks, -1, 1, true},
		{"line zero", navHunks, 0, 1, true},
		{"on a hunk start moves past it", navHunks, 1, 4, true},
		{"inside a multi-line hunk moves past it", navHunks, 5, 7, true},
		{"between hunks", navHunks, 2, 4, true},
		{"on the last hunk start", navHunks, 7, 0, false},
		{"after the last hunk", navHunks, 100, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := NextHunk(tc.hunks, tc.line)
			if got != tc.want || ok != tc.ok {
				t.Errorf("NextHunk(hunks, %d) = (%d, %v), want (%d, %v)", tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestPrevHunk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		hunks []Hunk
		line  int
		want  int
		ok    bool
	}{
		{"no hunks", nil, 5, 0, false},
		{"line zero", navHunks, 0, 0, false},
		{"on the first hunk start", navHunks, 1, 0, false},
		{"just after the first hunk start", navHunks, 2, 1, true},
		{"on a hunk start moves to the one before", navHunks, 4, 1, true},
		{"inside a hunk moves to its start", navHunks, 5, 4, true},
		{"between hunks", navHunks, 6, 4, true},
		{"after the last hunk", navHunks, 100, 7, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := PrevHunk(tc.hunks, tc.line)
			if got != tc.want || ok != tc.ok {
				t.Errorf("PrevHunk(hunks, %d) = (%d, %v), want (%d, %v)", tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSymbol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		n    int
		want rune
	}{
		{"first line", 0, 0},
		{"last rune before surrogates", 0xd7ff, 0xd7ff},
		{"first rune after surrogates", 0xd800, 0xe000},
		{"past surrogates", 0xffff, 0x107ff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := symbol(tc.n)
			if got != tc.want {
				t.Errorf("symbol(%#x) = %#x, want %#x", tc.n, got, tc.want)
			}
			if !utf8.ValidRune(got) {
				t.Errorf("symbol(%#x) = %#x is not a valid rune", tc.n, got)
			}
		})
	}
}
