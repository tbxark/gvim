package menubar

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// TestDropdownIsTheSharedCardFrame: the dropdown is painted on the shared overlay
// frame, like a card. It has no title, so its top border is the plain rule between
// the rounded corners, and the border takes the overlay border style.
func TestDropdownIsTheSharedCardFrame(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	box := m.geometry(80, 30).box
	g := grid.New(80, 30)
	m.Paint(g)
	border := darkStyles.Overlay.Border
	right, bottom := box.x+box.w-1, box.bottom
	for _, c := range []struct {
		x, y int
		text string
	}{
		{box.x, box.top, "╭"}, {right, box.top, "╮"},
		{box.x, bottom, "╰"}, {right, bottom, "╯"},
		{box.x, box.top + 1, "│"}, {right, box.top + 1, "│"},
		// No title: the plain rule begins right after the top-left corner.
		{box.x + 2, box.top, "─"},
	} {
		cell := g.At(c.x, c.y)
		if cell.Text != c.text {
			t.Errorf("cell (%d,%d) = %q, want %q", c.x, c.y, cell.Text, c.text)
		}
		if cell.Style != border {
			t.Errorf("cell (%d,%d) %q style = %+v, want the overlay border", c.x, c.y, cell.Text, cell.Style)
		}
	}
}

// TestDropdownKeepsTheSpaceMargin: a dropdown keeps space.Margin from the left and
// right edges of the frame, as every floating surface does.
func TestDropdownKeepsTheSpaceMargin(t *testing.T) {
	for _, w := range []int{40, 60, 80, 140} {
		m := newBar(allOn(), w, 30).Activate()
		box := m.geometry(w, 30).box
		margin := space.Margin(w)
		if box.x < margin || box.x+box.w > w-margin {
			t.Errorf("%d columns: box %d..%d breaks the margin of %d cells", w, box.x, box.x+box.w, margin)
		}
	}
	// The overflow marker's dropdown stays inside the frame too.
	narrow := newBar(allOn(), 40, 30).openTitle(moreTitle)
	if g := narrow.geometry(40, 30); !g.hasBox {
		t.Fatal("the marker dropdown was not placed")
	} else if right := g.box.x + g.box.w; right > 40-space.Margin(40) {
		t.Errorf("the marker dropdown ends at column %d, past the margin", right)
	}
}

// TestSeparatorRuleIsInsetByPadX: a separator is a rule that stops one padding cell
// before each border, as the cards and the context menus do.
func TestSeparatorRuleIsInsetByPadX(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	box := m.geometry(80, 30).box
	y := rowY(box, firstSeparator(m.Rows()))
	g := grid.New(80, 30)
	m.Paint(g)
	right := box.x + box.w - 1
	want := map[int]string{
		box.x:     "│",
		box.x + 1: " ",
		box.x + 2: "─",
		right - 2: "─",
		right - 1: " ",
		right:     "│",
	}
	for x, text := range want {
		if got := g.At(x, y).Text; got != text {
			t.Errorf("separator cell (%d,%d) = %q, want %q", x, y, got, text)
		}
	}
}

// TestSelectedRowUsesTheOverlaySelectedStyle: the highlighted row, its padding cells
// included, takes the overlay selected style.
func TestSelectedRowUsesTheOverlaySelectedStyle(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	box := m.geometry(80, 30).box
	g := grid.New(80, 30)
	m.Paint(g)
	y := rowY(box, m.Selected())
	selected := darkStyles.Overlay.Selected
	for _, x := range []int{box.x + 1, box.x + 2, box.x + 3} {
		if cell := g.At(x, y); cell.Style != selected {
			t.Errorf("selected row cell (%d,%d) %q style = %+v, want the overlay selected style", x, y, cell.Text, cell.Style)
		}
	}
	if next := g.At(box.x+2, y+1); next.Style == selected {
		t.Error("the row below the highlight takes the selected style")
	}
}

// TestEveryPaintedDropdownRowIsItsClickTarget: painting and hit testing read the same
// geometry, so the row a cell shows is the row a press there activates.
func TestEveryPaintedDropdownRowIsItsClickTarget(t *testing.T) {
	m := newBar(allOn(), 80, 30).Activate()
	box := m.geometry(80, 30).box
	plain := paintRows(m)
	x := box.x + 1 + space.PadX
	for k := range box.visible() {
		i := box.first + k
		row := box.rows[i]
		y := rowY(box, i)
		got := m.Hit(x, y)
		if row.Separator {
			if got.Kind != TargetBorder {
				t.Errorf("separator row %d: Hit = %+v, want a border", i, got)
			}
			continue
		}
		if got.Kind != TargetItem || got.Index != i {
			t.Errorf("row %d: Hit(%d,%d) = %+v, want item %d", i, x, y, got, i)
		}
		if !strings.Contains(plain[y], row.Label) {
			t.Errorf("row %d %q is not painted on row %d: %q", i, row.Label, y, plain[y])
		}
	}
}
