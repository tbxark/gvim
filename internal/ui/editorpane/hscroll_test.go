package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// barRow returns the text of body row y between the gutter and the last column.
func barRow(t *testing.T, g *grid.Grid, y int) string {
	t.Helper()
	var b strings.Builder
	for x := 6; x < g.Width()-1; x++ {
		b.WriteString(cellAt(t, g, x, y).Text)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestHorizontalScrollbarTakesTheLastBodyRow(t *testing.T) {
	// A 12-column pane with the gutter 6 has 5 text columns. The line is 16 wide.
	p := Pane{Doc: newDoc("0123456789abcdef", "x")}
	g := p.Render(12, 3)
	if got := barRow(t, g, 0); got != "01234" {
		t.Errorf("row 0 = %q, want the first five columns", got)
	}
	if got := barRow(t, g, 1); got != "x" {
		t.Errorf("row 1 = %q, want the second line", got)
	}
	// The thumb is at the start of the line, and the track runs on to the end.
	if got := cellAt(t, g, 6, 2).Text; got != "━" {
		t.Errorf("bar thumb = %q, want the thumb at the start", got)
	}
	if got := cellAt(t, g, 10, 2).Text; got != "─" {
		t.Errorf("bar track = %q, want a horizontal track", got)
	}
}

func TestNoHorizontalScrollbarWhenTheLinesFit(t *testing.T) {
	g := Pane{Doc: newDoc("abc", "de")}.Render(12, 3)
	if got := cellAt(t, g, 0, 2).Text; got != "~" {
		t.Errorf("last row starts with %q, want the tilde row", got)
	}
	if got := cellAt(t, g, 6, 2).Text; got == "─" || got == "━" {
		t.Errorf("last row shows a horizontal bar: %q", got)
	}
}

func TestCaretPastTheRightEdgeScrollsTheLineLeft(t *testing.T) {
	d := newDoc("0123456789abcdef")
	d.cursor = buffer.Pos{Col: 10}
	g := Pane{Doc: d}.Render(12, 3)
	// The window keeps two columns (the margin of a five-column window) right of
	// the caret at column 10, so it starts at column 8.
	if got := barRow(t, g, 0); got != "89abc" {
		t.Errorf("row 0 = %q, want columns 8 to 12", got)
	}
	if c := cellAt(t, g, 8, 0); !c.Style.Reverse || c.Text != "a" {
		t.Errorf("caret cell = %q reversed %v, want a reversed", c.Text, c.Style.Reverse)
	}
}

func TestCaretAtTheStartScrollsBack(t *testing.T) {
	d := newDoc("0123456789abcdef")
	d.cursor = buffer.Pos{Col: 0}
	p := Pane{Doc: d, Left: 9}
	if got := barRow(t, p.Render(12, 3), 0); got != "01234" {
		t.Errorf("row 0 = %q, want the window back at column 0", got)
	}
}

func TestThumbMovesRightWithTheWindow(t *testing.T) {
	thumbAt := func(col int) int {
		d := newDoc("0123456789abcdefghijklmnopqrstuvwxyz")
		d.cursor = buffer.Pos{Col: col}
		g := Pane{Doc: d}.Render(12, 3)
		for x := 6; x < 11; x++ {
			if cellAt(t, g, x, 2).Text == "━" {
				return x
			}
		}
		t.Fatal("no thumb on the bar")
		return -1
	}
	if start, end := thumbAt(0), thumbAt(35); end <= start {
		t.Errorf("thumb at the start column %d, at the end column %d, want it to move right", start, end)
	}
}

func TestWrapOnKeepsTheWholeLineAndNoBar(t *testing.T) {
	// The wrap width is the text area less two: 12 - 6 - 2 = 4 cells.
	g := Pane{Doc: newDoc("0123456789abcdef"), Wrap: true}.Render(12, 4)
	if got := cellAt(t, g, 6, 0).Text + cellAt(t, g, 7, 0).Text + cellAt(t, g, 8, 0).Text + cellAt(t, g, 9, 0).Text; got != "0123" {
		t.Errorf("row 0 = %q, want the first wrapped row", got)
	}
	if got := cellAt(t, g, 6, 1).Text; got != "4" {
		t.Errorf("row 1 starts with %q, want the next wrapped row", got)
	}
	for y := range 4 {
		if got := cellAt(t, g, 6, y).Text; got == "─" || got == "━" {
			t.Errorf("wrapped pane shows a horizontal bar on row %d", y)
		}
	}
}

func TestHitOnTheHorizontalBarIsTheBar(t *testing.T) {
	p := Pane{Doc: newDoc("0123456789abcdef")}
	if h := p.Hit(12, 3, 7, 2); h.Kind != HitHScrollbar || h.Col != 7 {
		t.Errorf("hit on the bar = %+v, want the horizontal scrollbar at column 7", h)
	}
	// The gutter cells of the bar row are not the bar.
	if h := p.Hit(12, 3, 2, 2); h.Kind != HitNone {
		t.Errorf("hit on the gutter of the bar row = %+v, want none", h)
	}
	// Without a bar the last row is text.
	if h := (Pane{Doc: newDoc("0123")}).Hit(12, 3, 7, 2); h.Kind == HitHScrollbar {
		t.Errorf("hit on a row with no bar = %+v", h)
	}
}

// TestHScrollForPutsTheThumbUnderThePointer: the ends of the track are the ends of the
// longest line, a press in the middle lands between, and the answer grows with x.
func TestHScrollForPutsTheThumbUnderThePointer(t *testing.T) {
	long := strings.Repeat("x", 300)
	p := Pane{Doc: newDoc(long)}
	const w, h, gutter = 60, 5, 6
	_, most := p.HScroll(w, h)
	if most != 300-(w-1-gutter) {
		t.Fatalf("setup: most = %d", most)
	}
	if got := p.HScrollFor(w, h, gutter-4); got != 0 {
		t.Errorf("left of the track gave %d, want 0", got)
	}
	if got := p.HScrollFor(w, h, w-1); got != most {
		t.Errorf("right end gave %d, want %d", got, most)
	}
	prev := -1
	for x := gutter; x < w; x++ {
		got := p.HScrollFor(w, h, x)
		if got < prev || got < 0 || got > most {
			t.Fatalf("x=%d gave %d after %d (most %d)", x, got, prev, most)
		}
		prev = got
	}
	mid := p.HScrollFor(w, h, gutter+(w-1-gutter)/2)
	if mid <= 0 || mid >= most {
		t.Errorf("the middle of the track gave %d, want it between 0 and %d", mid, most)
	}
	if got := (Pane{Doc: newDoc("short")}).HScrollFor(w, h, 30); got != 0 {
		t.Errorf("a pane with no bar gave %d, want 0", got)
	}
}

func TestCaretAndMouseFollowTheScrolledWindow(t *testing.T) {
	d := newDoc("0123456789abcdef")
	d.cursor = buffer.Pos{Col: 10}
	p := Pane{Doc: d}
	// The window starts at column 8 (see TestCaretPastTheRightEdgeScrollsTheLineLeft),
	// so the caret at column 10 is shown at x = 6 + (10 - 8).
	if x, y, ok := p.Caret(12, 3); !ok || x != 8 || y != 0 {
		t.Errorf("Caret = (%d,%d,%v), want (8,0,true)", x, y, ok)
	}
	// The first text cell is column 8 of the line.
	if pos := p.PosAt(12, 3, 6, 0); pos != (buffer.Pos{Col: 8}) {
		t.Errorf("PosAt first text cell = %+v, want column 8", pos)
	}
	if h := p.Hit(12, 3, 8, 0); h.Kind != HitText || h.Pos != (buffer.Pos{Col: 10}) {
		t.Errorf("Hit on x=8 = %+v, want text at column 10", h)
	}
}

func TestRulerFollowsTheScrolledWindow(t *testing.T) {
	d := newDoc("0123456789abcdef")
	d.cursor = buffer.Pos{Col: 10}
	// Ruler 10 is text column 9, which the window at column 8 shows at x = 6 + 1.
	p := Pane{Doc: d, Rulers: []int{10}, Styles: Styles{Ruler: grid.Style{BG: cGray}}}
	if c := cellAt(t, p.Render(12, 3), 7, 1); bgOf(c) != cGray {
		t.Errorf("ruler cell bg = %v, want the ruler on the scrolled window", bgOf(c))
	}
}

func TestDropColumnsSkipsWholeCells(t *testing.T) {
	cells := []cell{{r: 'a'}, {r: 'b'}, {r: 'c'}, {r: 'd'}}
	tests := []struct {
		n    int
		want string
	}{
		{0, "abcd"},
		{1, "bcd"},
		{3, "d"},
		{4, ""},
		{9, ""},
	}
	for _, tc := range tests {
		var got []rune
		for _, c := range dropColumns(cells, tc.n) {
			got = append(got, c.r)
		}
		if string(got) != tc.want {
			t.Errorf("dropColumns(%d) = %q, want %q", tc.n, string(got), tc.want)
		}
	}
}
