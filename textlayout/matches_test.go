package textlayout

import (
	"slices"
	"testing"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name       string
		line, text string
		want       []int
	}{
		{"inside longer words", "beta bet abet", "bet", []int{0, 5, 10}},
		{"no overlap", "aaaa", "aa", []int{0, 2}},
		{"multibyte columns are runes", "é bet", "bet", []int{2}},
		{"no match", "abc", "x", nil},
		{"empty text", "abc", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.line, tc.text); !slices.Equal(got, tc.want) {
				t.Errorf("Matches(%q, %q) = %v, want %v", tc.line, tc.text, got, tc.want)
			}
		})
	}
}
