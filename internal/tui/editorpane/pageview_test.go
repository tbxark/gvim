package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// pagePane returns a page-view pane over doc, measure cells wide, no line numbers.
func pagePane(doc Document, measure int) Pane {
	return Pane{Doc: doc, PageView: PageView{On: true, Measure: measure}}
}

func TestPageViewCentersTheText(t *testing.T) {
	t.Parallel()
	doc := newDoc("abc")
	p := pagePane(doc, 20)
	g := p.Render(40, 4)
	rows := g.Plain()
	if !strings.HasPrefix(rows[0], strings.Repeat(" ", 9)+"abc") {
		t.Fatalf("row = %q, want the text centered in a 20-cell page (margin 9)", rows[0])
	}
	if left, most := p.HScroll(40, 4); left != 0 || most != 0 {
		t.Fatalf("page view has a horizontal scroll: left %d, most %d", left, most)
	}
	if x, y, ok := p.Caret(40, 4); !ok || x != 9 || y != 0 {
		t.Fatalf("Caret = (%d,%d,%v), want (9,0,true)", x, y, ok)
	}
	// The text area is the page: a click right of the page maps to the line end.
	if hit := p.Hit(40, 4, 35, 0); hit.Kind != HitText {
		t.Fatalf("hit right of the page = %+v, want a text hit", hit)
	}
}

func TestPageViewHidesTheGutter(t *testing.T) {
	t.Parallel()
	doc := newDoc("a", "b")
	p := pagePane(doc, 30)
	g := p.Render(40, 4)
	for _, row := range g.Plain()[:2] {
		if strings.Contains(row, "1 │") || strings.Contains(row, "│") {
			t.Fatalf("row = %q, want no line number or separator", row)
		}
	}
}

func TestPageViewWrapsAtTheMeasure(t *testing.T) {
	t.Parallel()
	doc := newDoc("aaaa bbbb cccc dddd eeee ffff")
	p := pagePane(doc, 20)
	rowsOf := p.Render(44, 4).Plain()
	// Margin = (43-20)/2 = 11, and the measure is 20 cells.
	if got := strings.TrimRight(rowsOf[0], " "); !strings.HasSuffix(got, "aaaa bbbb cccc dddd") {
		t.Fatalf("first row = %q, want the page's first wrap row", got)
	}
	if !strings.HasPrefix(rowsOf[0], strings.Repeat(" ", 11)) {
		t.Fatalf("first row = %q, want the left margin 11", rowsOf[0])
	}
	joined := ""
	for _, row := range rowsOf[:3] {
		joined += strings.TrimSpace(row) + " "
	}
	if !strings.Contains(joined, "eeee") || !strings.Contains(joined, "ffff") {
		t.Fatalf("wrapped rows = %q, want the tail on the next rows", rowsOf[:3])
	}
	if _, most := p.HScroll(44, 4); most != 0 {
		t.Fatalf("page view allows horizontal scrolling to %d", most)
	}
}

func TestPageViewKeepsTheVerticalScrollbar(t *testing.T) {
	t.Parallel()
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line"
	}
	p := pagePane(newDoc(lines...), 20)
	g := p.Render(30, 6)
	last := g.Width() - 1
	thumb := false
	for y := 0; y < g.Height(); y++ {
		if g.At(last, y).Text == thumbGlyph {
			thumb = true
		}
	}
	if !thumb {
		t.Fatal("the page view dropped the vertical scrollbar")
	}
}

func TestPageViewCaretPastThePageClampsIntoIt(t *testing.T) {
	t.Parallel()
	doc := newDoc(strings.Repeat("x", 60))
	doc.cursor = buffer.Pos{Line: 0, Col: 40}
	p := pagePane(doc, 20)
	// The caret line is wrapped at the measure; the caret keeps its display row
	// and a column inside the text area.
	x, y, ok := p.Caret(40, 6)
	if !ok {
		t.Fatal("no caret in the page view")
	}
	if x < 9 || x >= 29 {
		t.Fatalf("caret x = %d, want it inside the 20-cell page at column 9", x)
	}
	if y < 0 || y >= 6 {
		t.Fatalf("caret y = %d, want a body row", y)
	}
}
