package space

import "testing"

// TestCardWidth checks the content width a card takes: the 40..64 clamp, then
// the frame's room after the chrome and the margins.
func TestCardWidth(t *testing.T) {
	for _, tc := range []struct {
		name          string
		frameW        int
		natural, want int
	}{
		{"narrow content gets the floor", 120, 10, 40},
		{"wide content gets the cap", 200, 200, 64},
		{"natural inside the clamp", 120, 50, 50},
		{"narrow frame caps the content", 50, 200, 46}, // frame 50 - chrome 4 - margins 0
		{"margins shrink the room", 68, 64, 62},        // frame 68 - chrome 4 - margin 1*2
		{"a tiny frame still fits one cell", 3, 64, 1},
	} {
		if got := CardWidth(tc.frameW, tc.natural); got != tc.want {
			t.Errorf("%s: CardWidth(%d, %d) = %d, want %d", tc.name, tc.frameW, tc.natural, got, tc.want)
		}
	}
}

// TestCardBox checks the centred box rectangle: chrome wider than the body, two
// rows taller, centred and clamped inside the frame.
func TestCardBox(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fw, fh    int
		cw, bodyH int
		want      Rect
	}{
		{"centred", 80, 24, 40, 5, Rect{X: 18, Y: 8, W: 44, H: 7}},
		{"narrow frame clamps to the edge", 10, 5, 40, 10, Rect{X: 0, Y: 0, W: 44, H: 12}},
		{"tiny frame", 0, 0, 40, 4, Rect{X: 0, Y: 0, W: 44, H: 6}},
	} {
		if got := CardBox(tc.fw, tc.fh, tc.cw, tc.bodyH); got != tc.want {
			t.Errorf("%s: CardBox = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
