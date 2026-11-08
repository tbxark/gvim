package palette

import "testing"

// TestCoversIsTheCardAndNothingAbove checks which cells of a frame the palette card
// takes a press on. The card is the box, its footer and its query row, within the box
// columns. A cell above the card or beside it is not the palette's.
func TestCoversIsTheCardAndNothingAbove(t *testing.T) {
	const w, h = 60, 20
	p := New(commandOptions(">save", nil))
	p.open = false
	if p.Covers(w/2, h-1, w, h) {
		t.Fatal("a closed palette covers the query row")
	}
	p.open = true
	geo := layoutFor(w, h)
	tests := []struct {
		name string
		x, y int
		want bool
	}{
		{"the box top border", w / 2, geo.top, true},
		{"a row of the list", w / 2, geo.top + 1, true},
		{"the query row", geo.nameX, geo.inputY, true},
		{"above the card", w / 2, geo.top - 1, false},
		{"the left of the card", geo.left, geo.top + 1, false},
		{"the right of the card", geo.right, geo.top + 1, false},
		{"the frame edge beside the card", 0, h - 1, false},
	}
	for _, tt := range tests {
		if got := p.Covers(tt.x, tt.y, w, h); got != tt.want {
			t.Errorf("%s at (%d,%d): Covers = %v, want %v", tt.name, tt.x, tt.y, got, tt.want)
		}
	}
}
