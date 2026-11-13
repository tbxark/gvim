package editorpane

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// fakeDoc is an in-memory Document. saved nil means the saved text equals the
// current lines.
type fakeDoc struct {
	lines        []string
	saved        []string
	modified     bool
	cursor       buffer.Pos
	anchor, head buffer.Pos
	selected     bool
	spans        map[int][]syntax.Span
}

var _ Document = (*fakeDoc)(nil)

func (d *fakeDoc) Modified() bool { return d.modified }
func (d *fakeDoc) LineCount() int { return len(d.lines) }
func (d *fakeDoc) Line(i int) string {
	return d.lines[i]
}
func (d *fakeDoc) SavedLines() []string {
	if d.saved == nil {
		return d.lines
	}
	return d.saved
}
func (d *fakeDoc) Cursor() buffer.Pos { return d.cursor }
func (d *fakeDoc) Selection() (anchor, head buffer.Pos, ok bool) {
	return d.anchor, d.head, d.selected
}
func (d *fakeDoc) LineSpans(i int) []syntax.Span { return d.spans[i] }

// newDoc returns a document with the given lines and the caret at the origin.
func newDoc(lines ...string) *fakeDoc {
	return &fakeDoc{lines: lines}
}

// rowsOf returns the plain rows of g with trailing blanks removed, so fixtures
// read as the text on screen.
func rowsOf(g *grid.Grid) []string {
	rows := g.Plain()
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

// assertTextRows compares the rows of g with want, leaving out the last column,
// which holds the scrollbar or the change marks.
func assertTextRows(t *testing.T, g *grid.Grid, want []string) {
	t.Helper()
	got := make([]string, g.Height())
	for y := range got {
		var b strings.Builder
		for x := range g.Width() - 1 {
			b.WriteString(g.At(x, y).Text)
		}
		got[y] = strings.TrimRight(b.String(), " ")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// assertRows compares the plain rows of g with want.
func assertRows(t *testing.T, g *grid.Grid, want []string) {
	t.Helper()
	got := rowsOf(g)
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRenderPlainText(t *testing.T) {
	p := Pane{Doc: newDoc("hello", "world")}
	g := p.Render(20, 4)
	assertRows(t, g, []string{
		"  1 │ hello",
		"  2 │ world",
		"~",
		"~",
	})
}

func TestRenderEmptyPaneHasNoDocument(t *testing.T) {
	g := Pane{}.Render(10, 2)
	assertRows(t, g, []string{"", ""})
	if g.Width() != 10 || g.Height() != 2 {
		t.Errorf("size = %dx%d, want 10x2", g.Width(), g.Height())
	}
}

func TestRenderNumberedGutterWidensWithLineCount(t *testing.T) {
	lines := make([]string, 120)
	for i := range lines {
		lines[i] = fmt.Sprintf("l%d", i+1)
	}
	// Three digits of line count give a four-wide number field.
	if got := GutterWidth(120); got != 7 {
		t.Fatalf("GutterWidth(120) = %d, want 7", got)
	}
	if got := GutterWidth(2); got != 6 {
		t.Fatalf("GutterWidth(2) = %d, want 6", got)
	}
	p := Pane{Doc: &fakeDoc{lines: lines}, Top: 98}
	// The document overflows the three rows, so the last column is the scrollbar.
	assertTextRows(t, p.Render(20, 3), []string{
		"  99 │ l99",
		" 100 │ l100",
		" 101 │ l101",
	})
}

func TestRenderTopClampsToDocument(t *testing.T) {
	p := Pane{Doc: newDoc("a", "b"), Top: 50}
	assertRows(t, p.Render(10, 2), []string{"  2 │ b", "~"})
}

func TestRenderWrapsLongLineAtTextWidthMinusTwo(t *testing.T) {
	// W=14 (legacy layout): the gutter is 6, so the wrap width is 14-6-2 = 6.
	p := Pane{Wrap: true, Doc: newDoc("hello world", "second line", "third")}
	assertRows(t, p.Render(14, 8), []string{
		"  1 │ hello",
		"    │ world",
		"  2 │ second",
		"    │  line",
		"  3 │ third",
		"~",
		"~",
		"~",
	})
}

func TestRenderTabsExpandToStops(t *testing.T) {
	p := Pane{Doc: newDoc("a\tb|")}
	assertRows(t, p.Render(14, 1), []string{"  1 │ a   b|"})
}

func TestRenderSelectionShowsDotsAndBackground(t *testing.T) {
	bg := color.RGBA{R: 50, G: 50, B: 60, A: 255}
	d := newDoc("a b c")
	d.selected = true
	d.anchor, d.head = buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 0, Col: 4}
	p := Pane{Doc: d, Styles: Styles{Selection: grid.Style{BG: bg}}}
	g := p.Render(14, 1)
	assertRows(t, g, []string{"  1 │ a·b·c"})
	// The gutter is six cells, so the text starts at column 6.
	if got := g.At(6, 0).Style.BG; got != nil {
		t.Errorf("unselected 'a' has background %v", got)
	}
	for _, x := range []int{7, 8, 9} {
		if got := g.At(x, 0).Style.BG; got != bg {
			t.Errorf("selected cell %d background = %v, want %v", x, got, bg)
		}
	}
}

func TestRenderSelectionWithoutBackgroundReverses(t *testing.T) {
	d := newDoc("ab")
	d.selected = true
	d.anchor, d.head = buffer.Pos{}, buffer.Pos{Line: 0, Col: 1}
	g := Pane{Doc: d}.Render(12, 1)
	if !g.At(6, 0).Style.Reverse {
		t.Errorf("selected cell is not reversed when the theme has no selection background")
	}
}

func TestCaretTracksRowsAcrossWrap(t *testing.T) {
	d := newDoc("hello world", "second line", "third")
	p := Pane{Wrap: true, Doc: d}
	cases := []struct {
		name       string
		line, col  int
		x, y       int
		wantInBody bool
	}{
		{name: "start", line: 0, col: 0, x: 6, y: 0, wantInBody: true},
		{name: "end of first row", line: 0, col: 5, x: 11, y: 0, wantInBody: true},
		{name: "start of wrapped row", line: 0, col: 6, x: 6, y: 1, wantInBody: true},
		{name: "wrapped row, third cell", line: 0, col: 8, x: 8, y: 1, wantInBody: true},
		{name: "end of document line", line: 0, col: 11, x: 11, y: 1, wantInBody: true},
		{name: "second line starts below the wrapped rows", line: 1, col: 0, x: 6, y: 2, wantInBody: true},
		{name: "second line, wrapped continuation", line: 1, col: 7, x: 7, y: 3, wantInBody: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d.cursor = buffer.Pos{Line: tc.line, Col: tc.col}
			x, y, ok := p.Caret(14, 8)
			if !ok {
				t.Fatalf("caret not in body")
			}
			if x != tc.x || y != tc.y {
				t.Errorf("caret = (%d, %d), want (%d, %d)", x, y, tc.x, tc.y)
			}
		})
	}
}

func TestCaretOffTheWindowIsNotInBody(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "x"
	}
	d := &fakeDoc{lines: lines, cursor: buffer.Pos{Line: 15}}
	if _, _, ok := (Pane{Doc: d}).Caret(10, 4); ok {
		t.Errorf("caret below the window reported in body")
	}
	if _, _, ok := (Pane{Doc: d, Top: 16}).Caret(10, 4); ok {
		t.Errorf("caret above the window reported in body")
	}
	if _, _, ok := (Pane{Doc: d, Top: 14}).Caret(10, 4); !ok {
		t.Errorf("caret in the window reported outside body")
	}
}

func TestScrollbarPresentWhenDocumentOverflows(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "x"
	}
	thumb, track := grid.Style{FG: color.RGBA{R: 1, A: 255}}, grid.Style{FG: color.RGBA{G: 1, A: 255}}
	st := Styles{Thumb: thumb, Scrollbar: track}
	g := Pane{Doc: &fakeDoc{lines: lines}, Styles: st}.Render(20, 10)
	// 30 lines in 10 rows: the thumb is (10*10+15)/30 = 3 rows, at the top. The
	// thumb is a "┃" in the thumb color, the track a "│" in the track color.
	for y := range 10 {
		want, glyph := track, "│"
		if y < 3 {
			want, glyph = thumb, "┃"
		}
		if got := g.At(19, y); got.Text != glyph || got.Style.FG != want.FG {
			t.Errorf("scrollbar row %d = %q fg %v, want %q fg %v", y, got.Text, got.Style.FG, glyph, want.FG)
		}
	}
	if got := g.At(6, 0).Text; got != "x" {
		t.Errorf("first text cell = %q, want x", got)
	}
	// The scrollbar sits in the last column, so the text rows carry no glyph.
	assertTextRows(t, g, []string{
		"  1 │ x", "  2 │ x", "  3 │ x", "  4 │ x", "  5 │ x",
		"  6 │ x", "  7 │ x", "  8 │ x", "  9 │ x", " 10 │ x",
	})
}

func TestScrollbarAbsentWhenDocumentFits(t *testing.T) {
	g := Pane{Doc: newDoc("a", "b", "c")}.Render(20, 10)
	for y := range 10 {
		if got := g.At(19, y).Text; got != " " {
			t.Errorf("last column row %d = %q, want blank", y, got)
		}
	}
}

func TestScrollbarThumbFollowsTop(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "x"
	}
	thumb, track := grid.Style{FG: color.RGBA{R: 1, A: 255}}, grid.Style{FG: color.RGBA{G: 1, A: 255}}
	st := Styles{Thumb: thumb, Scrollbar: track}
	g := Pane{Doc: &fakeDoc{lines: lines}, Top: 20, Styles: st}.Render(20, 10)
	// Top 20 is the last page, so the three-row thumb sits at the bottom: rows 7 to 9.
	for y := range 10 {
		want := track.FG
		if y >= 7 && y < 10 {
			want = thumb.FG
		}
		if got := g.At(19, y).Style.FG; got != want {
			t.Errorf("scrollbar row %d fg = %v, want %v", y, got, want)
		}
	}
}

func TestCaretCellIsReversed(t *testing.T) {
	d := newDoc("abc", "de")
	d.cursor = buffer.Pos{Line: 1, Col: 1}
	g := Pane{Doc: d}.Render(20, 4)
	// Line 1 is row 1, and its text starts after the gutter.
	cell := g.At(GutterWidth(2)+1, 1)
	if cell.Text != "e" || !cell.Style.Reverse {
		t.Fatalf("caret cell = %q reverse %v, want \"e\" reversed", cell.Text, cell.Style.Reverse)
	}
	if g.At(GutterWidth(2), 1).Style.Reverse {
		t.Error("a cell off the caret is reversed")
	}
}

func TestCaretPastTheTextIsABlankReversedCell(t *testing.T) {
	d := newDoc("ab")
	d.cursor = buffer.Pos{Line: 0, Col: 2}
	g := Pane{Doc: d}.Render(20, 2)
	cell := g.At(GutterWidth(1)+2, 0)
	if cell.Text != " " || !cell.Style.Reverse {
		t.Fatalf("caret after the end = %q reverse %v, want a reversed blank", cell.Text, cell.Style.Reverse)
	}
}

func TestChangedLineMarksBarAndLastColumn(t *testing.T) {
	d := &fakeDoc{
		lines:    []string{"one", "TWO", "three"},
		saved:    []string{"one", "two", "three"},
		modified: true,
	}
	g := Pane{Doc: d}.Render(20, 3)
	assertTextRows(t, g, []string{
		"  1 │ one",
		"│ 2 │ TWO",
		"  3 │ three",
	})
	if got := g.At(19, 1).Text; got != "▌" {
		t.Errorf("changed line last column = %q, want ▌", got)
	}
	if got := g.At(19, 0).Text; got != " " {
		t.Errorf("unchanged line last column = %q, want blank", got)
	}
}

func TestChangedLineMarksOnlyWhenModified(t *testing.T) {
	d := &fakeDoc{lines: []string{"one", "TWO"}, saved: []string{"one", "two"}}
	g := Pane{Doc: d}.Render(20, 2)
	if got := g.At(19, 1).Text; got != " " {
		t.Errorf("unmodified document shows a change mark %q", got)
	}
}

func TestWrappedRowsTakeTheLineMarks(t *testing.T) {
	d := &fakeDoc{
		lines:    []string{"hello world"},
		saved:    []string{""},
		modified: true,
	}
	g := Pane{Wrap: true, Doc: d}.Render(14, 3)
	// Both rows of the changed line take the margin bar and the last-column mark.
	for _, y := range []int{0, 1} {
		if got := g.At(0, y).Text; got != "│" {
			t.Errorf("row %d bar = %q, want │", y, got)
		}
		if got := g.At(13, y).Text; got != "▌" {
			t.Errorf("row %d last column = %q, want ▌", y, got)
		}
	}
}

func TestSyntaxColorsStyleTheTextButNotChangeIt(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDoc{
		lines: []string{"func x"},
		spans: map[int][]syntax.Span{0: {{Start: 0, End: 4, Kind: syntax.KindKeyword}}},
	}
	st := NewStyles(th)
	g := Pane{Doc: d, Styles: st}.Render(20, 1)
	assertRows(t, g, []string{"  1 │ func x"})

	keyword := st.Syntax[syntax.KindKeyword].FG
	if keyword != (color.RGBA{R: 255, G: 123, B: 114, A: 255}) {
		t.Errorf("keyword fg = %v, want the dark theme keyword color", keyword)
	}
	if got := g.At(6, 0).Style.FG; got != keyword {
		t.Errorf("'f' fg = %v, want keyword color", got)
	}
	if got := g.At(11, 0).Style.FG; got != st.Text.FG {
		t.Errorf("'x' fg = %v, want the editor foreground", got)
	}
}

func TestHitMapsCellsToPositions(t *testing.T) {
	p := Pane{Doc: newDoc("hello", "world")}
	cases := []struct {
		name string
		x, y int
		want buffer.Pos
		kind HitKind
	}{
		{name: "text on first line", x: 8, y: 0, want: buffer.Pos{Line: 0, Col: 2}, kind: HitText},
		{name: "past the end of a line", x: 19, y: 1, want: buffer.Pos{Line: 1, Col: 5}, kind: HitText},
		{name: "gutter maps to line start", x: 2, y: 1, want: buffer.Pos{Line: 1, Col: 0}, kind: HitGutter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := p.Hit(20, 4, tc.x, tc.y)
			if h.Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", h.Kind, tc.kind)
			}
			if h.Pos != tc.want {
				t.Errorf("pos = %+v, want %+v", h.Pos, tc.want)
			}
		})
	}
}

func TestHitOutsideTheBodyIsNone(t *testing.T) {
	p := Pane{Doc: newDoc("a")}
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {20, 0}, {0, 4}} {
		if h := p.Hit(20, 4, c[0], c[1]); h.Kind != HitNone {
			t.Errorf("hit at %v = %v, want none", c, h.Kind)
		}
	}
	if h := (Pane{}).Hit(20, 4, 8, 0); h.Kind != HitNone {
		t.Errorf("hit with no document = %v, want none", h.Kind)
	}
}

func TestHitWrappedRowMapsWithinTheLine(t *testing.T) {
	p := Pane{Wrap: true, Doc: newDoc("hello world", "second line", "third")}
	// Row 1 is "world", which starts at column 6 of the line.
	if h := p.Hit(14, 8, 8, 1); h.Pos != (buffer.Pos{Line: 0, Col: 8}) {
		t.Errorf("click on 'world' gave %+v, want column 8", h.Pos)
	}
	// Past the text of a row that is not the last, the click lands on the row's
	// last character, not on the next row.
	if h := p.Hit(14, 8, 13, 0); h.Pos != (buffer.Pos{Line: 0, Col: 5}) {
		t.Errorf("click past the first row gave %+v, want column 5", h.Pos)
	}
}

func TestHitScrollbarKinds(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "x"
	}
	p := Pane{Doc: &fakeDoc{lines: lines}}
	if h := p.Hit(20, 10, 19, 1); h.Kind != HitScrollbarThumb || h.Row != 1 {
		t.Errorf("press on the thumb = %+v, want thumb at row 1", h)
	}
	if h := p.Hit(20, 10, 19, 5); h.Kind != HitScrollbarTrack || h.Row != 5 {
		t.Errorf("press on the track = %+v, want track at row 5", h)
	}
	// Without overflow the last column is text.
	if h := (Pane{Doc: newDoc("a")}).Hit(20, 10, 19, 1); h.Kind != HitText {
		t.Errorf("last column without overflow = %v, want text", h.Kind)
	}
}

func TestPosAtClampsDragPastTheEdges(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "row"
	}
	p := Pane{Doc: &fakeDoc{lines: lines}, Top: 10}
	if got := p.PosAt(20, 5, 8, -2); got.Line != 8 {
		t.Errorf("drag above the body gave line %d, want 8", got.Line)
	}
	if got := p.PosAt(20, 5, 8, 9); got.Line != 19 {
		t.Errorf("drag below the body gave line %d, want 19", got.Line)
	}
	if got := p.PosAt(20, 5, 8, 500); got.Line != 49 {
		t.Errorf("drag far below gave line %d, want the last line 49", got.Line)
	}
}

func TestScrollTopForPlacesThumbNearPointer(t *testing.T) {
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "x"
	}
	const h = 27
	for row := range h {
		top := Pane{Doc: &fakeDoc{lines: lines}}.ScrollTopFor(40, h, row)
		start, length := Pane{Doc: &fakeDoc{lines: lines}, Top: top}.Thumb(h)
		centre := start + length/2
		if d := centre - row; d < -2 || d > 2 {
			t.Errorf("row %d: top %d puts the thumb centre at %d", row, top, centre)
		}
	}
	if got := (Pane{Doc: &fakeDoc{lines: lines}}).ScrollTopFor(40, h, 0); got != 0 {
		t.Errorf("track press at the top gave top %d, want 0", got)
	}
	if got := (Pane{Doc: &fakeDoc{lines: lines}}).ScrollTopFor(40, h, 500); got != 200-h {
		t.Errorf("track press at the bottom gave top %d, want %d", got, 200-h)
	}
}

// TestScrollbarReachesTheEndOfALongFile checks that the bottom of the track is the
// last page, and that the thumb of the last page rests on the bottom of the track.
func TestScrollbarReachesTheEndOfALongFile(t *testing.T) {
	tests := []struct{ lines, h int }{{2695, 37}, {200, 27}, {38, 37}, {10000, 5}}
	for _, tc := range tests {
		doc := &fakeDoc{lines: make([]string, tc.lines)}
		last := tc.lines - tc.h
		if got := (Pane{Doc: doc}).ScrollTopFor(40, tc.h, tc.h-1); got != last {
			t.Errorf("%d lines, h %d: a press on the last track row gave top %d, want %d", tc.lines, tc.h, got, last)
		}
		if start, length := (Pane{Doc: doc, Top: last}).Thumb(tc.h); start+length != tc.h {
			t.Errorf("%d lines, h %d: the thumb of the last page ends at row %d, want %d", tc.lines, tc.h, start+length, tc.h)
		}
	}
}

func TestBlitCopiesCellsAtOffset(t *testing.T) {
	src := grid.New(2, 1)
	src.Set(0, 0, "x", grid.Style{Bold: true})
	src.Set(1, 0, "y", grid.Style{})
	dst := grid.New(5, 2)
	Blit(dst, 2, 1, src)
	if got := dst.At(2, 1); got.Text != "x" || !got.Style.Bold {
		t.Errorf("blitted cell = %+v", got)
	}
	if got := dst.At(3, 1).Text; got != "y" {
		t.Errorf("second blitted cell = %q", got)
	}
	if got := dst.At(0, 0).Text; got != " " {
		t.Errorf("cell outside the blit = %q", got)
	}
}

// memoDoc is a fakeDoc that keeps the change marks, as editor.Document does.
type memoDoc struct {
	fakeDoc
	memoCalls int
}

func (d *memoDoc) MemoChanges(compute func() (bar, scroll map[int]bool)) (bar, scroll map[int]bool) {
	d.memoCalls++
	return compute()
}

func TestChangeMarksAskTheDocumentThatKeepsThem(t *testing.T) {
	d := &memoDoc{fakeDoc: fakeDoc{
		lines:    []string{"one", "TWO", "three"},
		saved:    []string{"one", "two", "three"},
		modified: true,
	}}
	g := Pane{Doc: d}.Render(20, 3)
	if d.memoCalls != 1 {
		t.Fatalf("MemoChanges called %d times, want 1", d.memoCalls)
	}
	if got := g.At(0, 1).Text; got != "│" {
		t.Errorf("changed line bar = %q, want │", got)
	}
	d.modified = false
	Pane{Doc: d}.Render(20, 3)
	if d.memoCalls != 1 {
		t.Errorf("an unmodified document asked for the change marks")
	}
}

// TestScrollbarReachesTheEndOfAWrappedFile checks that with Line Wrap on, the bottom
// of the track is the last page: the rows of the last lines are all on screen, and the
// thumb of that page rests on the bottom of the track.
func TestScrollbarReachesTheEndOfAWrappedFile(t *testing.T) {
	const w, h, n = 40, 21, 200
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d of the file lorem ipsum dolor sit", i+1) // wraps to 2 rows in 40 columns
	}
	doc := &fakeDoc{lines: lines}
	p := Pane{Doc: doc, Wrap: true}
	top := p.ScrollTopFor(w, h, h-1)
	p.Top = top
	frame, ok := p.newFrame(w, h)
	if !ok {
		t.Fatal("no frame")
	}
	if want := frame.lastPageTop(); top != want {
		t.Errorf("a press on the last track row gave top %d, want the last page %d", top, want)
	}
	rows := p.Render(w, h).Plain()
	if !strings.Contains(strings.Join(rows, "\n"), "line 200 of") {
		t.Errorf("the last line is not on screen after a press on the last track row (top %d)", top)
	}
	if start, length := frame.thumb(); start+length != h {
		t.Errorf("the thumb of the last page ends at row %d, want %d", start+length, h)
	}
	if got := (Pane{Doc: doc, Wrap: true}).ScrollTopFor(w, h, 0); got != 0 {
		t.Errorf("a press on the first track row gave top %d, want 0", got)
	}
}

// RevealTop returns the top line that puts the caret's display row inside a w by h
// pane, counting the wrapped rows of each line. A caret already on screen keeps
// the pane's top. A caret above the window takes its own line. A caret below takes
// the fewest lines that fit above it, so the scroll is as short as it can be.
func (p Pane) RevealTop(w, h int) int {
	return p.RevealTopMargin(w, h, 0)
}

// RevealTopMargin is RevealTop that also keeps margin display rows between the caret
// row and the top or bottom of the body, as the scroll-off setting asks. The margin
// is at most half the body, and it gives way at the start and the end of the
// document, where there is no text to show beyond the caret. The view moves by whole
// lines, so a wrapped line at the edge can leave a little more than margin rows.
func (p Pane) RevealTopMargin(w, h, margin int) int {
	top, _ := p.RevealView(w, h, margin)
	return top
}

// Thumb returns the first body row and the length in rows of the scrollbar
// thumb for the current top line in a pane h rows tall. It is meaningful when
// the document is taller than the pane.
func (p Pane) Thumb(h int) (start, length int) {
	if p.Doc == nil || h <= 0 {
		return 0, 0
	}
	lines := max(p.Doc.LineCount(), 1)
	display := max(DisplayLines(p.Doc), lines)
	top := min(max(p.Top, 0), lines-1)
	if p.Fillers != nil {
		return thumbOf(p.Fillers.Total(lines), h, p.Fillers.BlockStart(top)+max(p.TopRow, 0))
	}
	return thumbOf(display, h, top)
}
