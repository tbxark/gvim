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
	for y := range g.Height() {
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

type pageMemoDoc struct {
	*fakeDoc
	memo map[any]any
}

func (d *pageMemoDoc) MemoValue(key any, compute func() any) any {
	if val, ok := d.memo[key]; ok {
		return val
	}
	v := compute()
	d.memo[key] = v
	return v
}

func TestPageViewDualRevealInvalidatesKeyAndMapsColumns(t *testing.T) {
	t.Parallel()
	base := newDoc("**hello** world")
	base.cursor = buffer.Pos{Line: 0, Col: 10} // past "**hello**"
	doc := &pageMemoDoc{fakeDoc: base, memo: map[any]any{}}

	pConcealed := Pane{Doc: doc, PageView: PageView{On: true, Markdown: true, Measure: 30, NoCaretReveal: true}}
	pRevealed := Pane{Doc: doc, PageView: PageView{On: true, Markdown: true, Measure: 30, NoCaretReveal: false}}

	// Both share the same memoDoc and cursor; render focused (revealed) side first so
	// its memo entries populate doc.memo, then verify the concealed split does not reuse them.
	gRevealed := pRevealed.Render(40, 4).Plain()[0]
	gConcealed := pConcealed.Render(40, 4).Plain()[0]
	if strings.Contains(gConcealed, "**") || !strings.Contains(gRevealed, "**") {
		t.Fatalf("concealed=%q revealed=%q, want NoCaretReveal to conceal while focused side reveals", gConcealed, gRevealed)
	}
	f1, _ := pConcealed.newFrame(40, 4)
	f2, _ := pRevealed.newFrame(40, 4)
	k1 := layoutKey{
		line: 0, wrapW: f1.wrapW, tab: f1.tab, wrap: f1.wrap, hang: f1.fx.hang,
		pageCompose: true, pageMeasure: f1.pageMeasure, revealed: f1.revealed(0), caretOnLine: !f1.noPageReveal && f1.caret.Line == 0,
	}
	k2 := layoutKey{
		line: 0, wrapW: f2.wrapW, tab: f2.tab, wrap: f2.wrap, hang: f2.fx.hang,
		pageCompose: true, pageMeasure: f2.pageMeasure, revealed: f2.revealed(0), caretOnLine: !f2.noPageReveal && f2.caret.Line == 0,
	}
	if k1 == k2 {
		t.Fatal("layoutKey collided between NoCaretReveal true and false")
	}
	if (composeKey{line: 0, measure: f1.pageMeasure, revealed: !f1.noPageReveal}) ==
		(composeKey{line: 0, measure: f2.pageMeasure, revealed: !f2.noPageReveal}) {
		t.Fatal("composeKey collided between revealed false and true")
	}
	// Caret column on concealed row must land 4 cells left of revealed column (4 '*' markers hidden).
	cx, cy, ok := pConcealed.Caret(40, 4)
	rx, _, rok := pRevealed.Caret(40, 4)
	if !ok || !rok || cy != 0 {
		t.Fatalf("Caret concealed=(%d,%d,%v) revealed=%v", cx, cy, ok, rok)
	}
	if cx+4 != rx {
		t.Fatalf("concealed caret x = %d, want %d (rx %d - 4 concealed '*' markers)", cx, rx-4, rx)
	}
}
