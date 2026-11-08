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
	// A centred box and an anchored menu keep the same margin: one rule for every
	// floating surface.
	for _, w := range []int{0, 20, 59, 60, 79, 80, 139, 140, 200} {
		if got, want := ListMargin(w), Margin(w); got != want {
			t.Errorf("ListMargin(%d) = %d, want Margin(%d) = %d", w, got, w, want)
		}
	}
}

func TestPadSurroundsWithPadX(t *testing.T) {
	t.Parallel()
	if got, want := Pad("ab"), strings.Repeat(" ", PadX)+"ab"+strings.Repeat(" ", PadX); got != want {
		t.Errorf("Pad(ab) = %q, want %q", got, want)
	}
}
