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

func TestListMargin(t *testing.T) {
	t.Parallel()
	tests := []struct{ w, want int }{{0, 0}, {59, 0}, {60, 1}, {79, 1}, {80, 2}, {200, 2}}
	for _, tt := range tests {
		if got := ListMargin(tt.w); got != tt.want {
			t.Errorf("ListMargin(%d) = %d, want %d", tt.w, got, tt.want)
		}
	}
}

func TestPadSurroundsWithPadX(t *testing.T) {
	t.Parallel()
	if got, want := Pad("ab"), strings.Repeat(" ", PadX)+"ab"+strings.Repeat(" ", PadX); got != want {
		t.Errorf("Pad(ab) = %q, want %q", got, want)
	}
}
