package editorpane

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// fillBG is the background the test fillers are painted with.
var fillBG = grid.Style{BG: cGray}

// fillerPane is a pane over five lines "a".."e" with a filler row above line 0 and two
// after line 1:
//
//	row 0  filler (above)
//	row 1  a   (line 0)
//	row 2  b   (line 1)
//	row 3  filler
//	row 4  filler
//	row 5  c   (line 2)
//	row 6  d
//	row 7  e
func fillerPane() Pane {
	return Pane{
		Doc:     newDoc("a", "b", "c", "d", "e"),
		Fillers: NewFillers(1, map[int]int{1: 2}, fillBG),
	}
}

// bodyRows returns the rows of g without the scrollbar column and trailing blanks.
func bodyRows(g *grid.Grid) []string {
	out := make([]string, g.Height())
	for y := range out {
		var b strings.Builder
		for x := 0; x < g.Width()-1; x++ {
			b.WriteString(g.At(x, y).Text)
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

func TestFillersRowMapping(t *testing.T) {
	fl := NewFillers(1, map[int]int{1: 2, 3: 1, 4: 0}, fillBG)
	const lines = 5
	// Rows: 0 filler, 1 a, 2 b, 3 f, 4 f, 5 c, 6 d, 7 f, 8 e.
	starts := []int{0, 2, 5, 6, 8} // BlockStart of each line
	texts := []int{1, 2, 5, 6, 8}  // TextRow of each line
	blocks := []int{2, 3, 1, 2, 1} // blockRows of each line
	for line := range lines {
		if got := fl.BlockStart(line); got != starts[line] {
			t.Errorf("BlockStart(%d) = %d, want %d", line, got, starts[line])
		}
		if got := fl.TextRow(line); got != texts[line] {
			t.Errorf("TextRow(%d) = %d, want %d", line, got, texts[line])
		}
		if got := fl.blockRows(line); got != blocks[line] {
			t.Errorf("blockRows(%d) = %d, want %d", line, got, blocks[line])
		}
	}
	if got := fl.Total(lines); got != 9 {
		t.Errorf("Total = %d, want 9", got)
	}
	tests := []struct {
		row, line, in int
	}{
		{-4, 0, 0}, {0, 0, 0}, {1, 0, 1}, {2, 1, 0}, {3, 1, 1}, {4, 1, 2},
		{5, 2, 0}, {6, 3, 0}, {7, 3, 1}, {8, 4, 0}, {99, 4, 0},
	}
	for _, tt := range tests {
		if line, in := fl.LineAt(lines, tt.row); line != tt.line || in != tt.in {
			t.Errorf("LineAt(%d) = (%d, %d), want (%d, %d)", tt.row, line, in, tt.line, tt.in)
		}
	}
	// The mapping inverts: the block start of the line of a row never passes the row.
	for row := range fl.Total(lines) {
		line, in := fl.LineAt(lines, row)
		if fl.BlockStart(line)+in != row {
			t.Errorf("row %d maps to line %d row %d, which is row %d", row, line, in, fl.BlockStart(line)+in)
		}
	}
}

func TestNilFillersAreTheIdentity(t *testing.T) {
	var fl *Fillers
	if fl.Total(7) != 7 || fl.BlockStart(3) != 3 || fl.TextRow(3) != 3 || fl.blockRows(3) != 1 {
		t.Errorf("nil fillers change the rows: total %d, start %d, text %d, block %d",
			fl.Total(7), fl.BlockStart(3), fl.TextRow(3), fl.blockRows(3))
	}
	if line, in := fl.LineAt(7, 4); line != 4 || in != 0 {
		t.Errorf("LineAt(4) = (%d, %d), want (4, 0)", line, in)
	}
}

func TestPaneWithFillersPaintsThemBetweenTheLines(t *testing.T) {
	p := fillerPane()
	g := p.Render(20, 9)
	got := bodyRows(g)
	wantText := []string{"a", "b", "", "", "c", "d", "e"}
	var text []string
	for _, row := range got[:8] {
		_, after, _ := strings.Cut(row, "│ ")
		text = append(text, after) // past the gutter
	}
	// Row 0 is the filler above the first line.
	if text[0] != "" || !slices.Equal(text[1:], wantText) {
		t.Fatalf("rows = %q, want a blank row then %q", text, wantText)
	}
	// Filler rows carry the filler background in the text area and no line number.
	for _, y := range []int{0, 3, 4} {
		if bgOf(cellAt(t, g, 10, y)) != cGray {
			t.Errorf("row %d is not painted with the filler background", y)
		}
		if got[y] != "" {
			t.Errorf("filler row %d has text %q in the gutter or the text", y, got[y])
		}
	}
	// A text row has no filler background, and keeps its own line number.
	if bgOf(cellAt(t, g, 10, 1)) == cGray {
		t.Error("a text row has the filler background")
	}
	if want := "  1 │ a"; got[1] != want {
		t.Errorf("row 1 = %q, want %q", got[1], want)
	}
	if want := "  3 │ c"; got[5] != want {
		t.Errorf("row 5 = %q, want %q", got[5], want)
	}
}

func TestPaneWithFillersNeverWraps(t *testing.T) {
	p := Pane{Doc: newDoc("hello world, this is a long line"), Wrap: true, Fillers: NewFillers(0, nil, fillBG)}
	if p.wrapOn() {
		t.Fatal("a pane with fillers wraps")
	}
}

func TestFillerRowsMapToTheLineTheyFollow(t *testing.T) {
	p := fillerPane()
	const w, h = 20, 9
	tests := []struct {
		name string
		y    int
		want buffer.Pos
	}{
		{"above the first line", 0, buffer.Pos{Line: 0, Col: 0}},
		{"the first line", 1, buffer.Pos{Line: 0, Col: 0}},
		{"the line before the gap", 2, buffer.Pos{Line: 1, Col: 0}},
		{"first filler after line 1", 3, buffer.Pos{Line: 1, Col: 1}},
		{"second filler after line 1", 4, buffer.Pos{Line: 1, Col: 1}},
		{"the line after the gap", 5, buffer.Pos{Line: 2, Col: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.PosAt(w, h, 6, tt.y); got != tt.want {
				t.Errorf("PosAt(row %d) = %v, want %v", tt.y, got, tt.want)
			}
			hit := p.Hit(w, h, 6, tt.y)
			if hit.Kind != HitText || hit.Pos != tt.want {
				t.Errorf("Hit(row %d) = %+v, want text at %v", tt.y, hit, tt.want)
			}
		})
	}
	// The gutter of a filler row is nothing to click, and the gutter of a text row is.
	if hit := p.Hit(w, h, 1, 3); hit.Kind != HitNone {
		t.Errorf("gutter of a filler row = %+v, want none", hit)
	}
	if hit := p.Hit(w, h, 1, 5); hit.Kind != HitGutter || hit.Pos.Line != 2 {
		t.Errorf("gutter of line 2 = %+v, want a gutter hit on line 2", hit)
	}
}

func TestCaretRowCountsTheFillers(t *testing.T) {
	tests := []struct {
		line  int
		wantY int
	}{{0, 1}, {1, 2}, {2, 5}, {3, 6}, {4, 7}}
	for _, tt := range tests {
		p := fillerPane()
		doc := p.Doc.(*fakeDoc)
		doc.cursor = buffer.Pos{Line: tt.line}
		_, y, ok := p.Caret(20, 9)
		if !ok || y != tt.wantY {
			t.Errorf("caret on line %d: y = %d ok = %v, want %d", tt.line, y, ok, tt.wantY)
		}
	}
}

func TestTopRowScrollsIntoTheFillers(t *testing.T) {
	// Top line 1 with its text row scrolled off leaves the two fillers after it first.
	p := fillerPane()
	p.Top, p.TopRow = 1, 1
	g := p.Render(20, 4)
	got := bodyRows(g)
	if got[0] != "" || got[1] != "" || got[2] != "  3 │ c" || got[3] != "  4 │ d" {
		t.Errorf("rows = %q, want two filler rows, c, d", got)
	}
	// One more row of the top line scrolled off leaves one filler.
	p.TopRow = 2
	got = bodyRows(p.Render(20, 4))
	if got[0] != "" || got[1] != "  3 │ c" {
		t.Errorf("rows = %q, want one filler row, then c", got)
	}
	// A top row past the block is clamped to its last row.
	p.TopRow = 99
	got = bodyRows(p.Render(20, 4))
	if got[0] != "" || got[1] != "  3 │ c" {
		t.Errorf("clamped rows = %q, want one filler row, then c", got)
	}
}

func TestScrollViewStepsByRowsThroughTheFillers(t *testing.T) {
	p := fillerPane()
	type view struct{ top, row int }
	tests := []struct {
		name  string
		start view
		delta int
		want  view
	}{
		{"down one row stays in the block of line 0", view{0, 0}, 1, view{0, 1}},
		{"down onto the text of line 1", view{0, 1}, 1, view{1, 0}},
		{"down through the fillers", view{1, 0}, 2, view{1, 2}},
		{"down out of the fillers", view{1, 2}, 1, view{2, 0}},
		{"up into the fillers", view{2, 0}, -1, view{1, 2}},
		{"up to the text of line 1", view{1, 1}, -1, view{1, 0}},
		{"up to line 0", view{1, 0}, -1, view{0, 1}},
		{"up stops at the first row", view{0, 0}, -3, view{0, 0}},
		{"down stops at the last row", view{4, 0}, 5, view{4, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p.Top, p.TopRow = tt.start.top, tt.start.row
			top, row := p.ScrollView(20, 4, tt.delta)
			if (view{top, row}) != tt.want {
				t.Errorf("ScrollView(%+v, %d) = (%d, %d), want %+v", tt.start, tt.delta, top, row, tt.want)
			}
		})
	}
}

func TestRevealViewCountsTheFillers(t *testing.T) {
	// Body of 4 rows. The caret on line 4 is on row 7, so the view must start at row 4 or
	// below: the fewest rows that fit above the caret are the block of line 2 (row 5).
	p := fillerPane()
	p.Doc.(*fakeDoc).cursor = buffer.Pos{Line: 4}
	top, row := p.RevealView(20, 4, 0)
	if top != 2 || row != 0 {
		t.Errorf("RevealView = (%d, %d), want (2, 0): rows c, d, e fit", top, row)
	}
	// A caret already on screen keeps the view.
	p.Doc.(*fakeDoc).cursor = buffer.Pos{Line: 1}
	p.Top = 0
	if top, row = p.RevealView(20, 4, 0); top != 0 || row != 0 {
		t.Errorf("RevealView with the caret on screen = (%d, %d), want (0, 0)", top, row)
	}
	// A caret that is on row 5 of 4 rows needs line 1's text row at least.
	p.Doc.(*fakeDoc).cursor = buffer.Pos{Line: 2}
	if top, row = p.RevealView(20, 4, 0); top != 1 || row != 0 {
		t.Errorf("RevealView = (%d, %d), want (1, 0): rows b, f, f, c", top, row)
	}
}

func TestScrollbarThumbCountsTheFillers(t *testing.T) {
	// 5 lines and 3 filler rows are 8 rows. In a body of 4 rows the thumb is 2 rows. At the
	// top it is at 0, and with the view on line 4 (row 7) it reaches the bottom.
	p := fillerPane()
	if start, length := p.Thumb(4); start != 0 || length != 2 {
		t.Errorf("Thumb at the top = (%d, %d), want (0, 2)", start, length)
	}
	p.Top = 4
	if start, length := p.Thumb(4); start+length != 4 {
		t.Errorf("Thumb at the last line = (%d, %d), want it to reach the bottom", start, length)
	}
	// The last body row maps to the last page: rows 4.., which are the block of line 1.
	if top := p.ScrollTopFor(12, 4, 3); top != 1 {
		t.Errorf("ScrollTopFor(last row) = line %d, want 1 (the last page starts at row 4, in the block of line 1)", top)
	}
	if top := p.ScrollTopFor(12, 4, 0); top != 0 {
		t.Errorf("ScrollTopFor(first row) = line %d, want 0", top)
	}
}

func TestFilledPaneKeepsTheScrollbarOnFillerRows(t *testing.T) {
	p := fillerPane()
	g := p.Render(12, 4)
	for y := range 4 {
		got := g.At(11, y).Text
		if got != thumbGlyph && got != trackGlyph {
			t.Errorf("scrollbar cell of row %d = %q, want the thumb or the track", y, got)
		}
	}
}

func TestFilledPaneListsOnlyTextRows(t *testing.T) {
	p := fillerPane()
	rows := p.VisibleRows(20, 9, 100)
	var lines []int
	for _, r := range rows {
		lines = append(lines, r.Line)
	}
	if want := []int{0, 1, 2, 3, 4}; !slices.Equal(lines, want) {
		t.Errorf("VisibleRows lines = %v, want %v", lines, want)
	}
	// A body of 4 rows holds the filler above, a, b and one filler: two text rows.
	if got := p.VisibleRows(20, 4, 100); len(got) != 2 {
		t.Errorf("VisibleRows in 4 rows = %d rows, want 2", len(got))
	}
}

func TestSelectionAcrossFillersPaintsTheTextOnly(t *testing.T) {
	p := fillerPane()
	p.Styles.Selection = grid.Style{BG: cBlue}
	doc := p.Doc.(*fakeDoc)
	doc.selected = true
	doc.anchor, doc.head = buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 2, Col: 1}
	g := p.Render(20, 9)
	for _, y := range []int{1, 2, 5} {
		if bgOf(cellAt(t, g, 6, y)) != cBlue {
			t.Errorf("text row %d is not selected", y)
		}
	}
	for _, y := range []int{0, 3, 4} {
		if bgOf(cellAt(t, g, 6, y)) != cGray {
			t.Errorf("filler row %d lost its look under the selection", y)
		}
	}
}

func TestTrackMarksPaintOnTheTrackNotTheThumb(t *testing.T) {
	// 40 lines in a body of 10 rows: the thumb is the first row, the track the rest. Each
	// track row stands for 4 lines. A mark on lines 20 to 21 lands on track row 5, and a mark
	// under the thumb is not drawn.
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "x"
	}
	p := Pane{
		Doc: newDoc(lines...),
		TrackMarks: []TrackMark{
			{From: 20, To: 21, Style: grid.Style{FG: cRed}},
			{From: 0, To: 1, Style: grid.Style{FG: cGreen}},
		},
	}
	g := p.Render(12, 10)
	if c := g.At(11, 5); c.Text != changeScroll || c.Style.FG != cRed {
		t.Errorf("track row 5 = %q %v, want the mark in red", c.Text, c.Style.FG)
	}
	if c := g.At(11, 6); c.Text != trackGlyph {
		t.Errorf("track row 6 = %q, want the plain track", c.Text)
	}
	if c := g.At(11, 0); c.Text != thumbGlyph {
		t.Errorf("thumb row = %q, want the thumb, not the mark under it", c.Text)
	}
}
