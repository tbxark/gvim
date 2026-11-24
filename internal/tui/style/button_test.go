package style

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
)

// TestPaintButtonPadsAndUnderlines checks the button look: two spaces of padding
// on each side, the rune at under underlined, and the drawn-cell count returned.
func TestPaintButtonPadsAndUnderlines(t *testing.T) {
	t.Parallel()
	ov := New(theme.Theme{}).Overlay
	g := grid.New(30, 3)
	st := grid.Style{}
	n := ov.PaintButton(g, 4, 1, "Save", 0, st)
	if n != ov.ButtonWidth("Save") || n != 8 {
		t.Fatalf("PaintButton drew %d cells, want ButtonWidth 8", n)
	}
	if row := g.Plain()[1]; row[4:12] != "  Save  " {
		t.Fatalf("button row = %q, want %q", row[4:12], "  Save  ")
	}
	if c := g.At(6, 1); c.Style.Underline != true {
		t.Errorf("the hotkey rune is not underlined")
	}
	for _, x := range []int{7, 8, 9} {
		if g.At(x, 1).Style.Underline {
			t.Errorf("cell %d is underlined, want only the hotkey rune", x)
		}
	}
}

// TestPaintButtonNoUnderline checks a negative under draws the plain padded label.
func TestPaintButtonNoUnderline(t *testing.T) {
	t.Parallel()
	ov := New(theme.Theme{}).Overlay
	g := grid.New(20, 3)
	n := ov.PaintButton(g, 0, 0, "OK", -1, grid.Style{})
	if n != 6 || g.Plain()[0][:6] != "  OK  " {
		t.Fatalf("button row = %q (%d cells), want %q", g.Plain()[0][:6], n, "  OK  ")
	}
	for x := 0; x < 6; x++ {
		if g.At(x, 0).Style.Underline {
			t.Fatalf("cell %d is underlined with under = -1", x)
		}
	}
}
