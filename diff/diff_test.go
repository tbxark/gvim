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
		{"before replacement char", 0xfffc - 0x800, 0xfffc},
		{"skips replacement char", 0xfffd - 0x800, 0xfffe},
		{"past surrogates and replacement", 0xffff, 0x10800},
		{"last valid symbol", utf8.MaxRune - 0x800 - 1, utf8.MaxRune},
		{"past max rune clamps", utf8.MaxRune - 0x800, utf8.RuneError},
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
