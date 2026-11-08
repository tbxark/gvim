package picker

import (
	"testing"

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

func TestBoxKeepsAMarginAndBothBorders(t *testing.T) {
	t.Parallel()
	for _, w := range []int{40, 80, 100, 160} {
		p := New(style.New(theme.Theme{}), Options{Title: "Pick", Items: items("alpha", "beta"), Fuzzy: true})
		g := grid.New(w, 20)
		p.Draw(g)
		geo := p.box
		m := (w - geo.bw) / 2
		if geo.left != m || geo.bw != w-2*m {
			t.Fatalf("width %d: box at %d, %d wide", w, geo.left, geo.bw)
		}
		if w >= 60 && m == 0 {
			t.Errorf("width %d: no margin", w)
		}
		if w < 60 && m != 0 {
			t.Errorf("width %d: margin %d in a narrow frame", w, m)
		}
		for y := geo.top + 1; y < geo.top+geo.boxH-1; y++ {
			if l, r := g.At(geo.left, y).Text, g.At(geo.left+geo.bw-1, y).Text; l != "│" || r != "│" {
				t.Errorf("width %d row %d: borders %q %q", w, y, l, r)
			}
			if m > 0 {
				if c := g.At(0, y).Text; c != " " {
					t.Errorf("width %d row %d: margin cell holds %q", w, y, c)
				}
			}
		}
	}
}

func TestClicksOnTheMarginAndBordersAreNotRows(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Items: items("alpha", "beta"), Fuzzy: true})
	p.Draw(grid.New(100, 20))
	y := p.box.top + 1
	for _, x := range []int{0, p.box.left, p.box.left + p.box.bw - 1, 99} {
		if i, ok := p.rowAt(x, y); ok {
			t.Errorf("rowAt(%d, %d) = row %d, want none", x, y, i)
		}
	}
	if _, ok := p.rowAt(p.box.left+3, y); !ok {
		t.Error("a cell inside the box found no row")
	}
}

func TestTinyFramesDoNotPanic(t *testing.T) {
	t.Parallel()
	for w := 0; w <= 14; w++ {
		for h := 0; h <= 6; h++ {
			p := New(style.New(theme.Theme{}), Options{Title: "Pick", Items: items("alpha", "beta"), Fuzzy: true, Placeholder: "type"})
			p.Draw(grid.New(w, h))
			p.rowAt(w/2, h/2)
		}
	}
}
