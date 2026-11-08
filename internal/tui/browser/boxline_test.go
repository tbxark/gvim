package browser

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestBoxLineEndsALongNameWithAnEllipsisBeforeAPaddingCell(t *testing.T) {
	t.Parallel()
	for _, reserve := range []int{0, 1} {
		g := grid.New(30, 1)
		boxLine(g, 2, 0, 20, strings.Repeat("n", 40), grid.Style{}, grid.Style{}, reserve)
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
		if g.At(right, 0).Text != "│" || g.At(2, 0).Text != "│" {
			t.Errorf("reserve %d: borders are missing in %q", reserve, row)
		}
	}
}
