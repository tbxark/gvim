package popup

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestEveryRowHasBothBordersAndAPaddingCell(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("longlabel", 20)
	for _, tt := range []struct {
		name  string
		items []Item
		fw    int
	}{
		{"short rows", items(3), 80},
		{"a label wider than the frame", []Item{{Label: long}, {Label: "b", Detail: "detail"}}, 40},
		{"no items", nil, 40},
	} {
		p := New(Styles{})
		p.Show(Point{5, 3}, "Hover", tt.items)
		g := grid.New(tt.fw, 20)
		p.Draw(g)
		b := p.box
		if b.w == 0 {
			t.Fatalf("%s: the popup was not drawn", tt.name)
		}
		right, bottom := b.x+b.w-1, b.y+b.h-1
		if g.At(b.x, b.y).Text != "╭" || g.At(right, b.y).Text != "╮" || g.At(b.x, bottom).Text != "╰" || g.At(right, bottom).Text != "╯" {
			t.Errorf("%s: corners are %q %q %q %q", tt.name, g.At(b.x, b.y).Text, g.At(right, b.y).Text, g.At(b.x, bottom).Text, g.At(right, bottom).Text)
		}
		for y := b.y + 1; y < bottom; y++ {
			if l, r := g.At(b.x, y).Text, g.At(right, y).Text; l != "│" || r != "│" {
				t.Errorf("%s: row %d borders %q %q, want bars", tt.name, y, l, r)
			}
			// The cell beside each border is padding.
			if l, r := g.At(b.x+1, y).Text, g.At(right-1, y).Text; l != " " || r != " " {
				t.Errorf("%s: row %d padding cells %q %q, want blanks", tt.name, y, l, r)
			}
		}
	}
}

func TestPopupKeepsAMarginFromTheFrameEdges(t *testing.T) {
	t.Parallel()
	p := New(Styles{})
	p.Show(Point{0, 0}, "T", items(2))
	g := grid.New(100, 24)
	p.Draw(g)
	if p.box.x != 1 {
		t.Errorf("box at column %d, want 1 (a margin from the left edge)", p.box.x)
	}
	p.Show(Point{99, 23}, "T", items(2))
	p.Draw(g)
	if p.box.x+p.box.w != 99 {
		t.Errorf("box ends at column %d, want 99 (a margin from the right edge)", p.box.x+p.box.w)
	}
	if p.box.y+p.box.h > 23 {
		t.Errorf("box covers the last row: y=%d h=%d", p.box.y, p.box.h)
	}
}

func TestClicksOnTheBordersAreNotRows(t *testing.T) {
	t.Parallel()
	p := New(Styles{})
	p.Show(Point{5, 3}, "Hover", items(3))
	p.Draw(grid.New(80, 24))
	b := p.box
	right, bottom := b.x+b.w-1, b.y+b.h-1
	for _, c := range [][2]int{{b.x, b.y + 1}, {right, b.y + 1}, {b.x + 3, b.y}, {b.x + 3, bottom}} {
		if i, ok := p.rowAt(c[0], c[1]); ok {
			t.Errorf("rowAt(%d, %d) = row %d, want none", c[0], c[1], i)
		}
	}
	if i, ok := p.rowAt(b.x+3, b.y+2); !ok || i != 1 {
		t.Errorf("rowAt on the second row = %d, %v, want 1", i, ok)
	}
	// The padding cells belong to the highlighted row, so they choose it.
	if i, ok := p.rowAt(b.x+1, b.y+1); !ok || i != 0 {
		t.Errorf("rowAt on the padding cell = %d, %v, want row 0", i, ok)
	}
	// A click on the bottom border neither chooses a row nor closes the popup.
	if cmd := p.update(tea.MouseClickMsg(tea.Mouse{X: b.x + 3, Y: bottom, Button: tea.MouseLeft})); cmd != nil || !p.Active() {
		t.Errorf("a click on the border chose or closed: cmd=%v active=%v", cmd != nil, p.Active())
	}
}

func TestTinyFramesDoNotPanic(t *testing.T) {
	t.Parallel()
	for w := 0; w <= 12; w++ {
		for h := 0; h <= 6; h++ {
			p := New(Styles{})
			p.Show(Point{w / 2, h / 2}, "Title", items(5))
			p.Draw(grid.New(w, h))
			p.update(tea.MouseMotionMsg(tea.Mouse{X: w / 2, Y: h / 2}))
		}
	}
}
