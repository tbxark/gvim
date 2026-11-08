package list

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
		r := Centred(tt.w, tt.h, 500, 500)
		if r.W == 0 {
			t.Fatalf("%dx%d: no box", tt.w, tt.h)
		}
		if r.X != tt.wantMargin || tt.w-(r.X+r.W) != tt.wantMargin {
			t.Errorf("%dx%d: box at %d..%d, want a margin of %d on both sides", tt.w, tt.h, r.X, r.X+r.W, tt.wantMargin)
		}
		if r.Y != 1 || r.Y+r.H != tt.h-1 {
			t.Errorf("%dx%d: box rows %d..%d, want one row of margin above and below", tt.w, tt.h, r.Y, r.Y+r.H)
		}
	}
}
