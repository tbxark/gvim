package browser

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// TestBoxLineEndsALongNameWithAnEllipsisBeforeAPaddingCell checks the inside of a
// browser row: the text stops with an ellipsis before the padding, the scrollbar
// reserve, and the borders.
func TestBoxLineEndsALongNameWithAnEllipsisBeforeAPaddingCell(t *testing.T) {
	t.Parallel()
	for _, reserve := range []int{0, 1} {
		g := grid.New(30, 1)
		// The border cells belong to PaintBox: boxLine must not paint over them.
		g.Set(2, 0, "L", grid.Style{})
		g.Set(21, 0, "R", grid.Style{})
		boxLine(g, 2, 0, 20, strings.Repeat("n", 40), grid.Style{}, reserve)
		row := g.Plain()[0]
		// The box spans columns 2..21: the border, a padding cell, the text, a padding
		// cell (and the scrollbar column when reserved), the border.
		right := 21
		for x := right - 1 - reserve; x < right; x++ {
			if c := g.At(x, 0).Text; c != " " {
				t.Errorf("reserve %d: cell %d holds %q, want padding in %q", reserve, x, c, row)
			}
		}
		if c := g.At(right-2-reserve, 0).Text; c != "…" {
			t.Errorf("reserve %d: the last text cell holds %q, want an ellipsis in %q", reserve, c, row)
		}
		if g.At(2, 0).Text != "L" || g.At(21, 0).Text != "R" {
			t.Errorf("reserve %d: boxLine painted over the border cells in %q", reserve, row)
		}
	}
}
