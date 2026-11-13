package space

import (
	"strings"
	"testing"
)

func TestMargin(t *testing.T) {
	t.Parallel()
	tests := []struct{ w, want int }{
		{0, 0}, {20, 0}, {59, 0}, {60, 1}, {80, 1}, {139, 1}, {140, 2}, {200, 2},
	}
	for _, tt := range tests {
		if got := Margin(tt.w); got != tt.want {
			t.Errorf("Margin(%d) = %d, want %d", tt.w, got, tt.want)
		}
	}
}

func TestPadSurroundsWithPadX(t *testing.T) {
	t.Parallel()
	if got, want := Pad("ab"), strings.Repeat(" ", PadX)+"ab"+strings.Repeat(" ", PadX); got != want {
		t.Errorf("Pad(ab) = %q, want %q", got, want)
	}
}

func TestClamp(t *testing.T) {
	t.Parallel()
	tests := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-3, 0, 10, 0},
		{12, 0, 10, 10},
		{0, 0, 0, 0},
		{7, 3, 3, 3},
		{5, 4, 2, 4},  // an empty range gives lo
		{-9, 4, 2, 4}, // ... whatever v is
		{9, 0, -1, 0}, // the "last index of an empty list" case
	}
	for _, tt := range tests {
		if got := Clamp(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Errorf("Clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

// TestStripBidi removes exactly the bidirectional controls and nothing else.
func TestStripBidi(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "report.txt", "report.txt"},
		{"override", "txt.\u202egpj", "txt.gpj"},
		{"embeddings", "\u202aa\u202bb\u202cc\u202dd\u202ee", "abcde"},
		{"isolates", "\u2066a\u2067b\u2068c\u2069", "abc"},
		{"marks", "a\u200eb\u200fc\u061cd", "abcd"},
		{"joiner and emoji kept", "👨‍👩", "👨‍👩"},
		{"controls untouched", "a\tb", "a\tb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripBidi(tt.in); got != tt.want {
				t.Fatalf("StripBidi(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
