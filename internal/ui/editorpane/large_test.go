package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// spyDoc is a large document that makes its lines up as they are asked for, and
// counts what the pane asks. It holds no text, so a document of a million lines
// costs nothing.
type spyDoc struct {
	n      int
	approx int
	text   func(i int) string
	dirty  map[int]bool
	cursor buffer.Pos

	lineCalls map[int]int
	saved     int
	asked     map[int]bool // the lines LineDirty was asked about
	decoded   int          // the runes LineSlice had to walk
	slices    int
}

func newSpy(n int, text func(i int) string) *spyDoc {
	return &spyDoc{n: n, approx: n, text: text, lineCalls: map[int]int{}, asked: map[int]bool{}}
}

var (
	_ Document     = (*spyDoc)(nil)
	_ LargeDoc     = (*spyDoc)(nil)
	_ LineCounter  = (*spyDoc)(nil)
	_ DirtyLiner   = (*spyDoc)(nil)
	_ LineWindower = (*spyDoc)(nil)
)

func (d *spyDoc) Large() bool                 { return true }
func (d *spyDoc) Modified() bool              { return len(d.dirty) > 0 }
func (d *spyDoc) LineCount() int              { return d.n }
func (d *spyDoc) ApproxLineCount() int        { return d.approx }
func (d *spyDoc) Cursor() buffer.Pos          { return d.cursor }
func (d *spyDoc) LineSpans(int) []syntax.Span { return nil }
func (d *spyDoc) Selection() (anchor, head buffer.Pos, ok bool) {
	return buffer.Pos{}, buffer.Pos{}, false
}
func (d *spyDoc) SavedLines() []string {
	d.saved++
	return nil
}
func (d *spyDoc) Line(i int) string {
	d.lineCalls[i]++
	return d.text(i)
}
func (d *spyDoc) LineBytes(i int) int { return len(d.text(i)) }
func (d *spyDoc) LineSlice(i, col, n int) string {
	d.slices++
	r := []rune(d.text(i))
	d.decoded += min(col+n, len(r))
	col = min(col, len(r))
	return string(r[col:min(col+n, len(r))])
}
func (d *spyDoc) LineDirty(i int) bool {
	d.asked[i] = true
	return d.dirty[i]
}

// numbered is a line source of short distinct lines.
func numbered(i int) string { return "line " + strings.Repeat("x", i%7) }

// notLarge is a spy that says it is not in large file mode.
type notLarge struct{ *spyDoc }

func (notLarge) Large() bool { return false }

func TestDisplayLinesNeverBelowLineCount(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		approx   int
		want     int
		notLarge bool
	}{
		{"estimate larger", 500, 1_200_000, 1_200_000, false},
		{"estimate smaller", 5000, 10, 5000, false},
		{"equal", 70, 70, 70, false},
		{"not large ignores the estimate", 500, 1_200_000, 500, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpy(tc.n, numbered)
			spy.approx = tc.approx
			var doc Document = spy
			if tc.notLarge {
				doc = notLarge{spy}
			}
			if got := DisplayLines(doc); got != tc.want {
				t.Fatalf("DisplayLines = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGutterWidthFollowsTheEstimate(t *testing.T) {
	tests := []struct {
		name   string
		n      int
		approx int
	}{
		{"estimate adds digits", 500, 1_200_000},
		{"exact count wins", 120_000, 100},
		{"same digits", 500, 600},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpy(tc.n, numbered)
			spy.approx = tc.approx
			g := Pane{Doc: spy}.Render(60, 5)
			w := GutterWidth(max(tc.n, tc.approx))
			if got := cellAt(t, g, w-2, 0).Text; got != "│" {
				t.Errorf("separator at column %d = %q, want │ (gutter %d)", w-2, got, w)
			}
			if got := cellAt(t, g, w, 0).Text; got != "l" {
				t.Errorf("first text cell = %q, want l", got)
			}
		})
	}
}

func TestGutterIsSteadyWhileTheIndexGrows(t *testing.T) {
	spy := newSpy(900, numbered)
	spy.approx = 1_500_000
	w := GutterWidth(1_500_000)
	for _, n := range []int{900, 100_000, 1_400_000} {
		spy.n = n
		g := Pane{Doc: spy}.Render(60, 4)
		if got := cellAt(t, g, w-2, 0).Text; got != "│" {
			t.Errorf("%d lines known: separator moved, cell %d = %q", n, w-2, got)
		}
		if got := cellAt(t, g, w, 1).Text; got != "l" {
			t.Errorf("%d lines known: text moved, cell %d = %q", n, w, got)
		}
	}
}

func TestThumbSizeComesFromTheEstimate(t *testing.T) {
	small := newSpy(40, numbered)
	big := newSpy(40, numbered)
	big.approx = 2_000_000
	_, lenSmall := Pane{Doc: small}.Thumb(20)
	startBig, lenBig := Pane{Doc: big, Top: 20}.Thumb(20)
	if lenBig != 1 || lenSmall <= lenBig {
		t.Errorf("thumb lengths = %d (exact), %d (estimate); want the estimate's to be one row", lenSmall, lenBig)
	}
	if startBig != 0 {
		t.Errorf("thumb start = %d, want 0: the top is a millionth of the estimate", startBig)
	}
	// A track press at the middle asks for a line in the estimate's scale, which the
	// exact lines clamp.
	if top := (Pane{Doc: big}).ScrollTopFor(20, 10); top != 40-20 {
		t.Errorf("ScrollTopFor = %d, want the last full page %d", top, 40-20)
	}
}

func TestPaneLargeDocNeverCallsSavedLines(t *testing.T) {
	spy := newSpy(1_000_000, numbered)
	spy.dirty = map[int]bool{500_000: true, 3: true}
	spy.cursor = buffer.Pos{Line: 400_000}
	const top, h = 400_000, 12
	g := Pane{Doc: spy, Top: top}.Render(50, h)
	if spy.saved != 0 {
		t.Fatalf("SavedLines called %d times", spy.saved)
	}
	for i := range spy.lineCalls {
		if i < top-1 || i > top+h+1 {
			t.Errorf("Line(%d) was read; the window is %d..%d", i, top, top+h-1)
		}
	}
	if got := cellAt(t, g, GutterWidth(1_000_000), 0).Text; got != "l" {
		t.Errorf("first text cell = %q", got)
	}
}

func TestPaneLargeDocMarksOnlyVisibleDirtyLines(t *testing.T) {
	spy := newSpy(1_000_000, numbered)
	spy.dirty = map[int]bool{10_002: true, 11: true, 900_000: true}
	g := Pane{Doc: spy, Top: 10_000}.Render(50, 6)
	if got := cellAt(t, g, 0, 2).Text; got != changeBar {
		t.Errorf("margin of the dirty line = %q, want %q", got, changeBar)
	}
	if got := cellAt(t, g, 0, 1).Text; got == changeBar {
		t.Errorf("margin of a clean line is marked")
	}
	if got := cellAt(t, g, 49, 2).Text; got == changeScroll {
		t.Errorf("a large document has no change marks in the scrollbar column")
	}
	for i := range spy.asked {
		if i < 10_000 || i > 10_000+6 {
			t.Errorf("LineDirty(%d) asked outside the window", i)
		}
	}
}

func TestPaneLargeDocNeverWraps(t *testing.T) {
	long := strings.Repeat("word ", 40)
	spy := newSpy(30, func(int) string { return long })
	g := Pane{Doc: spy, Wrap: true}.Render(40, 6)
	// A wrapped line would take several rows: the gutter shows consecutive numbers
	// on consecutive rows only if every line is one row.
	for y := range 5 {
		if got := strings.Fields(g.Plain()[y])[0]; got != string(rune('1'+y)) {
			t.Errorf("row %d number = %q, want %q", y, got, string(rune('1'+y)))
		}
	}
	if got := (Pane{Doc: spy, Wrap: true}).BodyRows(40, 6); got != 5 {
		t.Errorf("BodyRows = %d, want 5: the horizontal bar takes a row and nothing wraps", got)
	}
}

// longLine is a line of n runes whose rune k is a letter by k.
func longLine(n int) string {
	var b strings.Builder
	for k := range n {
		b.WriteRune(rune('a' + k%26))
	}
	return b.String()
}

func TestLongLinesAreLaidOutFromAWindowOnly(t *testing.T) {
	const rows, line, left = 20, 300_000, 5000
	spy := newSpy(100, func(int) string { return longLine(line) })
	spy.cursor = buffer.Pos{Line: 0, Col: left + 10}
	p := Pane{Doc: spy, Left: left, KeepLeft: true}
	var plain []string
	for range 3 {
		plain = p.Render(60, rows+1).Plain()
	}
	gw := GutterWidth(100)
	want := string([]rune(longLine(line))[left : left+(59-gw)])
	if got := strings.TrimRight(string([]rune(plain[3])[gw:59]), " "); got != want {
		t.Errorf("row 3 = %q, want the line from column %d: %q", got, left, want)
	}
	if len(spy.lineCalls) != 0 {
		t.Errorf("Line was called for %d long lines; windows read through LineSlice", len(spy.lineCalls))
	}
	// Three frames, each laying out every row once through the frame's own cache,
	// with a few calls of slack for the caret line.
	if limit := 3 * (rows + 2) * (longWindow + left); spy.decoded > limit {
		t.Errorf("LineSlice walked %d runes, want at most %d", spy.decoded, limit)
	}
	if spy.saved != 0 {
		t.Errorf("SavedLines called")
	}
}

func TestLongLineCaretAndMouseAgree(t *testing.T) {
	const line = 200_000
	spy := newSpy(10, func(int) string { return longLine(line) })
	spy.cursor = buffer.Pos{Line: 2, Col: 150_000}
	p := Pane{Doc: spy}
	x, y, ok := p.Caret(60, 6)
	if !ok || y != 2 {
		t.Fatalf("Caret = (%d,%d,%v), want a cell on row 2", x, y, ok)
	}
	gw := GutterWidth(10)
	if x < gw || x >= 59 {
		t.Fatalf("caret column %d is outside the text area %d..58: the window must follow the caret", x, gw)
	}
	if got := p.PosAt(60, 6, x, y); got != spy.cursor {
		t.Errorf("PosAt(caret cell) = %+v, want %+v", got, spy.cursor)
	}
	cell := cellAt(t, p.Render(60, 6), x, y)
	if want := string(rune('a' + 150_000%26)); cell.Text != want {
		t.Errorf("caret cell text = %q, want %q", cell.Text, want)
	}
	if !cell.Style.Reverse {
		t.Errorf("the caret cell is not drawn")
	}
	if h := p.Hit(60, 6, x, y); h.Kind != HitText || h.Pos != spy.cursor {
		t.Errorf("Hit = %+v, want text at the caret", h)
	}
}

func TestLongLineHorizontalBarUsesTheStoredLength(t *testing.T) {
	spy := newSpy(5, func(int) string { return longLine(100_000) })
	left, most := Pane{Doc: spy}.HScroll(60, 6)
	if want := 100_000 - (60 - 1 - GutterWidth(5)); left != 0 || most != want {
		t.Errorf("HScroll = (%d, %d), want (0, %d)", left, most, want)
	}
}

func TestShortLinesOfALargeDocumentAreReadWhole(t *testing.T) {
	spy := newSpy(10, func(i int) string { return "short line " + string(rune('0'+i)) })
	g := Pane{Doc: spy}.Render(40, 4)
	assertTextRows(t, g, []string{"  1 │ short line 0", "  2 │ short line 1", "  3 │ short line 2", "  4 │ short line 3"})
	if spy.slices != 0 {
		t.Errorf("LineSlice called %d times for lines that are not long", spy.slices)
	}
}

func TestBracketPairScanOfALargeDocumentIsBounded(t *testing.T) {
	// The partner of the bracket is never found: every line after it is 12 KB with no
	// bracket. The scan may read about scanBytes of them, not 2000 lines of them.
	filler := strings.Repeat("x", 12_000)
	spy := newSpy(5000, func(i int) string {
		if i == 0 {
			return "f("
		}
		return filler
	})
	spy.cursor = buffer.Pos{Line: 0, Col: 2}
	Pane{Doc: spy, Opts: Options{HighlightBrackets: true}}.Render(60, 5)
	read := 0
	for i, n := range spy.lineCalls {
		if i > 0 {
			read += n * len(filler)
		}
	}
	if read > 3*scanBytes {
		t.Errorf("the scan read %d bytes of other lines, want about %d at most", read, scanBytes)
	}
}

// caretSpy is a spy with extra carets, the way *editor.Document reports them.
type caretSpy struct {
	*spyDoc
	extra []editor.Caret
}

func (d caretSpy) Cursors() []editor.Caret { return d.extra }

// TestExtraCaretsOffScreenCostNoLayout is the regression test for a frame whose time
// grew with the number of carets, on screen or not: each extra caret laid out the rows
// of the window to find its cell, and each selection was tested for every cell.
func TestExtraCaretsOffScreenCostNoLayout(t *testing.T) {
	const rows = 10
	spy := newSpy(20000, numbered)
	var extra []editor.Caret
	for i := 100; i < 9100; i++ {
		p := buffer.Pos{Line: i, Col: 2}
		extra = append(extra, editor.Caret{
			Cursor: p, Sel: &buffer.Selection{Anchor: buffer.Pos{Line: i}, Head: p},
		})
	}
	// Two carets on screen, and one just above the window.
	extra = append(extra,
		editor.Caret{Cursor: buffer.Pos{Line: 1, Col: 3}},
		editor.Caret{Cursor: buffer.Pos{Line: 4, Col: 1}},
	)
	spy.cursor = buffer.Pos{Line: 50}
	doc := caretSpy{spyDoc: spy, extra: extra}
	g := Pane{Doc: doc, Top: 1, Styles: Styles{SecondaryCaret: grid.Style{BG: cRed}}}.Render(60, rows)
	calls := 0
	for _, n := range spy.lineCalls {
		calls += n
	}
	if limit := rows * 8; calls > limit {
		t.Errorf("the pane asked for %d lines with %d extra carets, want at most %d", calls, len(extra), limit)
	}
	w := GutterWidth(20000)
	for _, c := range []struct{ x, y int }{{w + 3, 0}, {w + 1, 3}} {
		if got := bgOf(cellAt(t, g, c.x, c.y)); got != cRed {
			t.Errorf("extra caret cell (%d,%d) background = %v, want the secondary caret style", c.x, c.y, got)
		}
	}
}
