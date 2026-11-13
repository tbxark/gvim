package popup

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// testChrome is an overlay palette whose roles a test can tell apart: the border,
// the title, the selected row and the muted detail each have their own color.
func testChrome() style.Overlay {
	box := color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}
	return style.Overlay{
		Box:      grid.Style{BG: box},
		Border:   grid.Style{FG: color.RGBA{R: 0x30, G: 0x81, B: 0xf7, A: 0xff}, BG: box},
		Title:    grid.Style{FG: color.RGBA{R: 0xef, G: 0xf8, B: 0xff, A: 0xff}, BG: box, Bold: true},
		Selected: grid.Style{FG: color.RGBA{R: 0xef, G: 0xf8, B: 0xff, A: 0xff}, BG: color.RGBA{R: 0x2f, G: 0x81, B: 0xf7, A: 0xff}},
		Muted:    grid.Style{FG: color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}, BG: box, Faint: true},
	}
}

// checkCell fails unless the cell (x, y) holds text in style st.
func checkCell(t *testing.T, g *grid.Grid, x, y int, text string, st grid.Style) {
	t.Helper()
	c := g.At(x, y)
	if c.Text != text {
		t.Errorf("cell (%d,%d) = %q, want %q", x, y, c.Text, text)
	}
	if c.Style != st {
		t.Errorf("cell (%d,%d) %q style = %+v, want %+v", x, y, c.Text, c.Style, st)
	}
}

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
		p := New(style.Overlay{})
		p.Show(Point{5, 3}, "Hover", tt.items)
		g := grid.New(tt.fw, 20)
		p.Draw(g)
		b := p.box
		if b.W == 0 {
			t.Fatalf("%s: the popup was not drawn", tt.name)
		}
		right, bottom := b.X+b.W-1, b.Y+b.H-1
		if g.At(b.X, b.Y).Text != "╭" || g.At(right, b.Y).Text != "╮" || g.At(b.X, bottom).Text != "╰" || g.At(right, bottom).Text != "╯" {
			t.Errorf("%s: corners are %q %q %q %q", tt.name, g.At(b.X, b.Y).Text, g.At(right, b.Y).Text, g.At(b.X, bottom).Text, g.At(right, bottom).Text)
		}
		for y := b.Y + 1; y < bottom; y++ {
			if l, r := g.At(b.X, y).Text, g.At(right, y).Text; l != "│" || r != "│" {
				t.Errorf("%s: row %d borders %q %q, want bars", tt.name, y, l, r)
			}
			// The cell beside each border is padding.
			if l, r := g.At(b.X+1, y).Text, g.At(right-1, y).Text; l != " " || r != " " {
				t.Errorf("%s: row %d padding cells %q %q, want blanks", tt.name, y, l, r)
			}
		}
	}
}

func TestPopupKeepsTheMarginOfTheSpaceRule(t *testing.T) {
	t.Parallel()
	for _, fw := range []int{50, 100, 140, 160} {
		m := space.Margin(fw)
		p := New(style.Overlay{})
		g := grid.New(fw, 24)
		p.Show(Point{0, 0}, "T", items(2))
		p.Draw(g)
		if p.box.X != m {
			t.Errorf("%d columns: box at column %d, want %d (the margin from the left edge)", fw, p.box.X, m)
		}
		p.Show(Point{fw - 1, 23}, "T", items(2))
		p.Draw(g)
		if p.box.X+p.box.W != fw-m {
			t.Errorf("%d columns: box ends at column %d, want %d (the margin from the right edge)", fw, p.box.X+p.box.W, fw-m)
		}
		if p.box.Y+p.box.H > 24-m {
			t.Errorf("%d columns: box covers the last row: y=%d h=%d", fw, p.box.Y, p.box.H)
		}
	}
}

// TestPopupNeverCoversTheMenuBarRow: a tall popup flipped above a caret near the top
// of the frame stops below row 0, where the menu bar labels are.
func TestPopupNeverCoversTheMenuBarRow(t *testing.T) {
	t.Parallel()
	p := New(style.Overlay{})
	p.Show(Point{2, 1}, "Hover", items(8))
	p.Draw(grid.New(80, 10))
	if p.box.Y < 1 {
		t.Errorf("box top row %d covers the menu bar row", p.box.Y)
	}
}

func TestClicksOnTheBordersAreNotRows(t *testing.T) {
	t.Parallel()
	p := New(style.Overlay{})
	p.Show(Point{5, 3}, "Hover", items(3))
	p.Draw(grid.New(80, 24))
	b := p.box
	right, bottom := b.X+b.W-1, b.Y+b.H-1
	for _, c := range [][2]int{{b.X, b.Y + 1}, {right, b.Y + 1}, {b.X + 3, b.Y}, {b.X + 3, bottom}} {
		if i, ok := p.rowAt(c[0], c[1]); ok {
			t.Errorf("rowAt(%d, %d) = row %d, want none", c[0], c[1], i)
		}
	}
	if i, ok := p.rowAt(b.X+3, b.Y+2); !ok || i != 1 {
		t.Errorf("rowAt on the second row = %d, %v, want 1", i, ok)
	}
	// The padding cells belong to the highlighted row, so they choose it.
	if i, ok := p.rowAt(b.X+1, b.Y+1); !ok || i != 0 {
		t.Errorf("rowAt on the padding cell = %d, %v, want row 0", i, ok)
	}
	// A click on the bottom border neither chooses a row nor closes the popup.
	if cmd := p.update(tea.MouseClickMsg(tea.Mouse{X: b.X + 3, Y: bottom, Button: tea.MouseLeft})); cmd != nil || !p.Active() {
		t.Errorf("a click on the border chose or closed: cmd=%v active=%v", cmd != nil, p.Active())
	}
}

// TestDrawPaintsTheSharedCardFrame checks the frame the popup shares with the cards:
// a rounded border in the border style, the bare title on the top border with one
// padding cell on each side, the plain rule after it, and the overlay roles for the
// selected row and the detail text.
func TestDrawPaintsTheSharedCardFrame(t *testing.T) {
	t.Parallel()
	chrome := testChrome()
	p := New(chrome)
	p.Show(Point{3, 2}, "Pick", []Item{{Label: "alpha"}, {Label: "beta", Detail: "det"}})
	g := grid.New(60, 12)
	p.Draw(g)
	b := p.box
	right, bottom := b.X+b.W-1, b.Y+b.H-1

	checkCell(t, g, b.X, b.Y, "╭", chrome.Border)
	checkCell(t, g, right, b.Y, "╮", chrome.Border)
	checkCell(t, g, b.X, bottom, "╰", chrome.Border)
	checkCell(t, g, right, bottom, "╯", chrome.Border)
	checkCell(t, g, b.X, b.Y+1, "│", chrome.Border)
	checkCell(t, g, right, b.Y+1, "│", chrome.Border)

	// The title is bare: one blank, the text, one blank, then the plain rule.
	checkCell(t, g, b.X+2, b.Y, " ", chrome.Title)
	checkCell(t, g, b.X+3, b.Y, "P", chrome.Title)
	checkCell(t, g, b.X+6, b.Y, "k", chrome.Title)
	checkCell(t, g, b.X+7, b.Y, " ", chrome.Title)
	checkCell(t, g, b.X+8, b.Y, "─", chrome.Border)

	// The highlighted row and the muted detail.
	checkCell(t, g, b.X+2, b.Y+1, "a", chrome.Selected)
	if c := g.At(b.X+4, b.Y+1); c.Style != chrome.Selected {
		t.Errorf("the rest of the selected row style = %+v, want the selected style", c.Style)
	}
	checkCell(t, g, b.X+2, b.Y+2, "b", chrome.Box)
	checkCell(t, g, b.X+8, b.Y+2, "d", chrome.Muted)
}

// TestTheHitTargetOfEveryRowIsTheRowPaintedThere: the painted rows and the click
// targets come from the same box, so each row's cell maps to the row drawn there.
func TestTheHitTargetOfEveryRowIsTheRowPaintedThere(t *testing.T) {
	t.Parallel()
	p := New(testChrome())
	p.Show(Point{4, 2}, "Rows", []Item{{Label: "first"}, {Label: "second", Detail: "d"}, {Label: "third"}})
	g := grid.New(60, 12)
	p.Draw(g)
	b := p.box
	for k := range p.rows {
		i := p.top + k
		y := b.Y + 1 + k
		if !strings.Contains(g.Plain()[y], p.items[i].Label) {
			t.Errorf("row %d is not painted on row %d", i, y)
		}
		for _, x := range []int{b.X + 2, b.X + 3, b.X + b.W - 2} {
			if got, ok := p.rowAt(x, y); !ok || got != i {
				t.Errorf("rowAt(%d,%d) = %d, %v, want row %d", x, y, got, ok, i)
			}
		}
	}
	for _, c := range [][2]int{{b.X, b.Y + 1}, {b.X + b.W - 1, b.Y + 1}, {b.X + 2, b.Y}, {b.X + 2, b.Y + b.H - 1}} {
		if got, ok := p.rowAt(c[0], c[1]); ok {
			t.Errorf("the border cell (%d,%d) maps to row %d", c[0], c[1], got)
		}
	}
}

func TestTinyFramesDoNotPanic(t *testing.T) {
	t.Parallel()
	for w := 0; w <= 12; w++ {
		for h := 0; h <= 6; h++ {
			p := New(style.Overlay{})
			p.Show(Point{w / 2, h / 2}, "Title", items(5))
			p.Draw(grid.New(w, h))
			p.update(tea.MouseMotionMsg(tea.Mouse{X: w / 2, Y: h / 2}))
		}
	}
}
