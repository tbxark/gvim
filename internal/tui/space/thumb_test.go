package space

import "testing"

func TestThumbMax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		lines, h, top, maxTop int
		wantStart, wantLen    int
		startRange            [2]int // when wantStart is -1: inclusive bounds
	}{
		{name: "empty", lines: 0, h: 6, top: 0, maxTop: 0, wantStart: 0, wantLen: 0},
		{name: "fits", lines: 4, h: 6, top: 0, maxTop: -2, wantStart: 0, wantLen: 6},
		{name: "top", lines: 11, h: 4, top: 0, maxTop: 7, wantStart: 0, wantLen: 1},
		{name: "last page reaches the bottom", lines: 11, h: 4, top: 7, maxTop: 7, wantStart: 3, wantLen: 1},
		{name: "past the last page is clamped", lines: 11, h: 4, top: 10, maxTop: 7, wantStart: 3, wantLen: 1},
		{name: "wrapped view with a smaller maxTop", lines: 40, h: 6, top: 5, maxTop: 5, wantStart: 5, wantLen: 1},
		{name: "huge counts do not overflow", lines: 10_000_000, h: 1_000_000, top: 5_000_000, maxTop: 9_000_000, wantStart: -1, wantLen: -1, startRange: [2]int{400_000, 600_000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, length := ThumbMax(tt.lines, tt.h, tt.top, tt.maxTop)
			if tt.wantStart == -1 {
				if start < tt.startRange[0] || start > tt.startRange[1] || length < 1 {
					t.Fatalf("ThumbMax = %d,%d, want a start in %v", start, length, tt.startRange)
				}
				return
			}
			if start != tt.wantStart || length != tt.wantLen {
				t.Fatalf("ThumbMax(%d,%d,%d,%d) = %d,%d, want %d,%d", tt.lines, tt.h, tt.top, tt.maxTop, start, length, tt.wantStart, tt.wantLen)
			}
		})
	}
}

// TestThumbReachesTheBottomOnTheLastPage checks Thumb ends flush with the track when the
// last page (top = lines-h) is shown, for every document taller than the window.
func TestThumbReachesTheBottomOnTheLastPage(t *testing.T) {
	t.Parallel()
	for lines := 5; lines <= 60; lines++ {
		for h := 1; h < lines && h <= 12; h++ {
			start, length := Thumb(lines, h, lines-h)
			if start+length != h {
				t.Fatalf("Thumb(%d,%d,%d) = %d,%d, want the thumb at the bottom of %d rows", lines, h, lines-h, start, length, h)
			}
		}
	}
}
