package ui

import "testing"

func TestCentredBoxKeepsTheListMarginFromTheEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		w, h       int
		wantMargin int
	}{
		{200, 40, 2}, {80, 24, 2}, {79, 24, 1}, {60, 24, 1}, {59, 24, 0}, {30, 24, 0},
	}
	for _, tt := range tests {
		r := centred(tt.w, tt.h, 500, 500)
		if r.w == 0 {
			t.Fatalf("%dx%d: no box", tt.w, tt.h)
		}
		if r.x != tt.wantMargin || tt.w-(r.x+r.w) != tt.wantMargin {
			t.Errorf("%dx%d: box at %d..%d, want a margin of %d on both sides", tt.w, tt.h, r.x, r.x+r.w, tt.wantMargin)
		}
		if r.y != 1 || r.y+r.h != tt.h-1 {
			t.Errorf("%dx%d: box rows %d..%d, want one row of margin above and below", tt.w, tt.h, r.y, r.y+r.h)
		}
	}
}
