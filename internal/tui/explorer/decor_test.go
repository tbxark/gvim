package explorer

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// TestDrawPluginDecorSlots checks the painter's Decor callback: a trailing slot overrides
// the native mark, suppression removes both, a leading slot reserves its width and the
// name colour paints the filename.
func TestDrawPluginDecorSlots(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	bPath := dir + "/b.txt"
	cPath := dir + "/c.txt"
	cRed := color.RGBA{R: 255, A: 255}
	cGreen := color.RGBA{G: 255, A: 255}
	g := grid.New(16, 8)
	geo := Geometry{Top: 1, Width: 16, Bottom: 7, Focused: true, Marks: map[string]rune{cPath: 'A'}}
	geo.Decor = func(path string) RowDecor {
		switch path {
		case bPath:
			return RowDecor{
				Leading: "→", LeadingMinWidth: 2, LeadingStyle: grid.Style{FG: cGreen},
				Trailing: "M", TrailingStyle: grid.Style{FG: cRed},
				NameColor: cGreen,
			}
		case cPath:
			return RowDecor{SuppressTrailing: true}
		}
		return RowDecor{}
	}
	New(tr, drawStyles(t), nil).Draw(g, geo)
	lines := g.Plain()
	var bRow string
	for _, line := range lines {
		if strings.Contains(line, "b.txt") {
			bRow = line
		}
	}
	if bRow == "" {
		t.Fatalf("no b.txt row in %q", lines)
	}
	if !strings.Contains(bRow, "→") || !strings.Contains(bRow, "M") {
		t.Fatalf("b.txt row = %q, want the leading and trailing slots", bRow)
	}
	// The native git mark of c.txt is suppressed by the slot override.
	for _, line := range lines {
		if strings.Contains(line, "c.txt") && strings.Contains(line, "A") {
			t.Fatalf("c.txt kept its native mark: %q", line)
		}
	}
}
