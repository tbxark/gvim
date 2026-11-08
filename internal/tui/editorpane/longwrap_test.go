package editorpane

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/syntax"
)

// segDoc is a document in memory with a line that is longer than LongLineRunes. It reads
// lines through windows, as the editor's adapter does, and counts the runes its windows
// walk, so a test can tell that a frame did not build the whole line.
type segDoc struct {
	lines   []string
	runes   [][]rune
	cursor  buffer.Pos
	decoded int
	whole   int // the times a tall line was asked for as a string
}

var (
	_ Document     = (*segDoc)(nil)
	_ LineWindower = (*segDoc)(nil)
	_ LongLiner    = (*segDoc)(nil)
	_ lineLener    = (*segDoc)(nil)
)

func newSegDoc(lines ...string) *segDoc {
	d := &segDoc{lines: lines}
	for _, l := range lines {
		d.runes = append(d.runes, []rune(l))
	}
	return d
}

func (d *segDoc) Modified() bool              { return false }
func (d *segDoc) LineCount() int              { return len(d.lines) }
func (d *segDoc) SavedLines() []string        { return nil }
func (d *segDoc) Cursor() buffer.Pos          { return d.cursor }
func (d *segDoc) LineSpans(int) []syntax.Span { return nil }
func (d *segDoc) HasLongLine() bool           { return true }
func (d *segDoc) LineLen(i int) int           { return len(d.runes[i]) }
func (d *segDoc) LineBytes(i int) int         { return len(d.runes[i]) }
func (d *segDoc) Line(i int) string {
	if len(d.runes[i]) > LongLineRunes {
		d.whole++
	}
	return d.lines[i]
}
func (d *segDoc) Selection() (a, h buffer.Pos, ok bool) { return buffer.Pos{}, buffer.Pos{}, false }
func (d *segDoc) LineSlice(i, col, n int) string {
	r := d.runes[i]
	col = min(max(col, 0), len(r))
	end := min(col+n, len(r))
	d.decoded += end - col
	return string(r[col:end])
}

// words is text of n runes made of words of mixed lengths, so rows break at spaces.
func words(n int) string {
	var b strings.Builder
	for k := 0; b.Len() < n; k++ {
		b.WriteString(strings.Repeat(string(rune('a'+k%26)), 1+k%9))
		b.WriteByte(' ')
	}
	return b.String()[:n]
}

// allRows returns every row of tall line i, by walking from its start.
func allRows(f frame, i int) []rowDesc {
	return f.tallRows(i, false, 0, 1<<30)
}

// The rows of a tall line cover the line without a gap or an overlap, none is wider than the
// wrap width, and a segment edge falls after a space where there is one to fall after.
func TestTallLineRowsCoverTheLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		w    int
	}{
		{"words", words(20_000), 60},
		{"narrow", words(9_000), 30},
		{"one long word", strings.Repeat("x", 9_000), 50},
		{"wide characters", strings.Repeat("日本語 ", 3_000), 40},
		{"tabs", strings.Repeat("a\tb ", 2_500), 40},
		{"blank run", strings.Repeat(" ", 9_000) + "end", 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := newSegDoc("head", tt.text, "tail")
			f, ok := Pane{Doc: d, Wrap: true}.newFrame(tt.w, 10)
			if !ok || !f.isTall(1) || f.isTall(0) {
				t.Fatalf("line 1 tall = %v, line 0 tall = %v", f.isTall(1), f.isTall(0))
			}
			rows := allRows(f, 1)
			if len(rows) < 2 {
				t.Fatalf("%d rows", len(rows))
			}
			var joined strings.Builder
			col := 0
			for k, r := range rows {
				if r.start != col {
					t.Fatalf("row %d starts at %d, the row before ended at %d", k, r.start, col)
				}
				end := len([]rune(tt.text))
				if k+1 < len(rows) {
					end = rows[k+1].start
				}
				if end <= r.start {
					t.Fatalf("row %d is empty: [%d, %d)", k, r.start, end)
				}
				joined.WriteString(string([]rune(tt.text)[r.start:end]))
				if w := ansi.StringWidth(string(r.ln.geom[r.sp.from:r.sp.to])); w > f.wrapW {
					t.Fatalf("row %d is %d cells wide, wrap width %d", k, w, f.wrapW)
				}
				col = end
				if r.first != (k == 0) || r.last != (k == len(rows)-1) {
					t.Fatalf("row %d first = %v last = %v", k, r.first, r.last)
				}
			}
			if joined.String() != tt.text {
				t.Fatal("the rows do not join to the text of the line")
			}
		})
	}
}

// Segments start at fixed places of the line and the layout depends on the text around them
// only, so a row start is the same whether the rows are found from the line start or from
// the middle: that is what lets a frame show the middle of a line without the rows above it.
func TestTallLineRowsFromTheMiddleMatchRowsFromTheStart(t *testing.T) {
	t.Parallel()
	d := newSegDoc(words(30_000))
	f, _ := Pane{Doc: d, Wrap: true}.newFrame(70, 12)
	all := allRows(f, 0)
	starts := make(map[int]int, len(all))
	for k, r := range all {
		starts[r.start] = k
	}
	for col := 0; col < 30_000; col += 997 {
		rows := f.tallRows(0, false, col, 3)
		k, ok := starts[rows[0].start]
		if !ok {
			t.Fatalf("a row from column %d starts at %d, which is no row start from the line start", col, rows[0].start)
		}
		for j, r := range rows {
			if all[k+j].start != r.start {
				t.Fatalf("from column %d row %d starts at %d, want %d", col, j, r.start, all[k+j].start)
			}
		}
		if rows[0].start > col {
			t.Fatalf("the row of column %d starts after it, at %d", col, rows[0].start)
		}
		if k+1 < len(all) && col >= all[k+1].start {
			t.Fatalf("the row of column %d ends at %d, before it", col, all[k+1].start)
		}
	}
}

// The next and the previous row are the neighbours in the list of all rows.
func TestTallLineRowNavigation(t *testing.T) {
	t.Parallel()
	d := newSegDoc(words(15_000))
	f, _ := Pane{Doc: d, Wrap: true}.newFrame(50, 10)
	all := allRows(f, 0)
	for k := 0; k < len(all); k += 7 {
		start := all[k].start
		next, ok := f.tallNextRow(0, start)
		if k+1 < len(all) {
			if !ok || next != all[k+1].start {
				t.Fatalf("next row of row %d = %d, %v; want %d", k, next, ok, all[k+1].start)
			}
		} else if ok {
			t.Fatalf("the last row has a next row at %d", next)
		}
		prev, ok := f.tallPrevRow(0, start)
		if k > 0 {
			if !ok || prev != all[k-1].start {
				t.Fatalf("previous row of row %d = %d, %v; want %d", k, prev, ok, all[k-1].start)
			}
		} else if ok {
			t.Fatalf("the first row has a previous row at %d", prev)
		}
		if got := f.tallRowStart(0, start+1); got != start && k+1 < len(all) && start+1 < all[k+1].start {
			t.Fatalf("row start of column %d = %d, want %d", start+1, got, start)
		}
	}
	if got := f.tallBack(0, all[len(all)-1].start, 5); got != all[len(all)-6].start {
		t.Fatalf("tallBack 5 rows from the last = %d, want %d", got, all[len(all)-6].start)
	}
	if got := f.tallBack(0, all[2].start, 50); got != 0 {
		t.Fatalf("tallBack past the start = %d, want 0", got)
	}
}

// RowWindow lists the rows around a column from the row before the one that holds it.
func TestRowWindowAroundAColumn(t *testing.T) {
	t.Parallel()
	d := newSegDoc("short", words(12_000))
	p := Pane{Doc: d, Wrap: true}
	f, _ := p.newFrame(60, 10)
	all := allRows(f, 1)
	tests := []struct {
		name string
		col  int
		want []int
	}{
		{"first row", 3, []int{all[0].start, all[1].start, all[2].start}},
		{"second row", all[1].start + 2, []int{all[0].start, all[1].start, all[2].start, all[3].start}},
		{"middle", all[40].start, []int{all[39].start, all[40].start, all[41].start, all[42].start}},
		{"last row", 12_000, []int{all[len(all)-2].start, all[len(all)-1].start}},
		{"second last", all[len(all)-2].start + 1, []int{all[len(all)-3].start, all[len(all)-2].start, all[len(all)-1].start}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, tall := p.RowWindow(60, 10, 1, tt.col)
			if !tall || fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("RowWindow(col %d) = %v, %v; want %v", tt.col, got, tall, tt.want)
			}
		})
	}
	if got, tall := p.RowWindow(60, 10, 0, 0); tall || got != nil {
		t.Errorf("RowWindow of a short line = %v, %v; want nil, false", got, tall)
	}
}

// Painting the start, the middle and the end of a tall line lays out a few segments and
// never builds the line as a string.
func TestTallLineFramesReadOnlyTheirSegments(t *testing.T) {
	t.Parallel()
	const n = 3_000_000
	d := newSegDoc("first", words(n), "last")
	f, _ := Pane{Doc: d, Wrap: true}.newFrame(80, 24)
	end := f.tallRowStart(1, n)
	middle := f.tallRowStart(1, n/2)
	for name, anchor := range map[string]int{"start": 0, "middle": middle, "end": end} {
		before := d.decoded
		d.cursor = buffer.Pos{Line: 1, Col: anchor}
		p := Pane{Doc: d, Wrap: true, Top: 1, TopRow: anchor}
		g := p.Render(80, 24)
		p.Caret(80, 24)
		p.RevealView(80, 24, 0)
		p.PosAt(80, 24, 20, 10)
		if got := d.decoded - before; got > 40*segmentRunes {
			t.Errorf("%s: a frame walked %d runes of the line, want a few segments of %d", name, got, segmentRunes)
		}
		if d.whole != 0 {
			t.Fatalf("%s: the whole line was asked for %d times", name, d.whole)
		}
		if row := strings.TrimSpace(g.Plain()[0]); !strings.Contains(row, "│") {
			t.Errorf("%s: first row %q has no gutter", name, row)
		}
	}
}

// A view that starts inside a tall line shows rows from the row of its column, the gutter
// number only on the first row of the line, and the lines after the tall one when it ends.
func TestTallLineViewFromAnAnchor(t *testing.T) {
	t.Parallel()
	text := words(2_000) + "END"
	d := newSegDoc("one", strings.Repeat("a b ", 1_500)+text, "after")
	p := Pane{Doc: d, Wrap: true}
	f, _ := p.newFrame(50, 8)
	rows := allRows(f, 1)
	// The view starts a few rows before the last row of the line, so the line ends in it and
	// the next line follows.
	from := rows[len(rows)-4].start
	p = Pane{Doc: d, Wrap: true, Top: 1, TopRow: from + 1}
	g := p.Render(50, 8)
	plain := rowsOf(g)
	for y := range 4 {
		if strings.HasPrefix(strings.TrimLeft(plain[y], " "), "2 ") {
			t.Errorf("row %d shows the line number of a row that continues the line: %q", y, plain[y])
		}
	}
	if !strings.Contains(plain[3], "END") {
		t.Errorf("row 3 = %q, want the end of the tall line", plain[3])
	}
	if !strings.Contains(plain[4], "3") || !strings.Contains(plain[4], "after") {
		t.Errorf("row 4 = %q, want line 3", plain[4])
	}
	// The same rows map back to the line.
	for y := range 4 {
		pos := p.PosAt(50, 8, 30, y)
		if pos.Line != 1 || pos.Col < from || pos.Col >= len([]rune(d.lines[1]))+1 {
			t.Errorf("PosAt row %d = %+v, want a column of line 1 from %d", y, pos, from)
		}
	}
	if pos := p.PosAt(50, 8, 3, 4); pos.Line != 2 {
		t.Errorf("PosAt row 4 = %+v, want line 2", pos)
	}
}

// The caret on a tall line, anywhere on it, can be shown: RevealView puts its row on the
// screen, the caret's cell is there, and the row it names is the row of the column.
func TestTallLineCaretIsRevealed(t *testing.T) {
	t.Parallel()
	d := newSegDoc("one", words(40_000), "three")
	probe, _ := Pane{Doc: d, Wrap: true}.newFrame(60, 10)
	for _, col := range []int{0, 5, 700, 20_000, 39_999, 40_000} {
		d.cursor = buffer.Pos{Line: 1, Col: col}
		p := Pane{Doc: d, Wrap: true, Top: 0}
		top, row := p.RevealView(60, 10, 0)
		p.Top, p.TopRow = top, row
		x, y, ok := p.Caret(60, 10)
		if !ok {
			t.Fatalf("col %d: the caret is not on screen after RevealView(top %d, row %d)", col, top, row)
		}
		// The cell holds the rune at the caret, or is the cell after the line's last rune.
		g := p.Render(60, 10)
		got := cellAt(t, g, x, y).Text
		want := string([]rune(d.lines[1] + " ")[col])
		if col < 40_000 && got != want && x >= probe.gutter {
			// A caret on a space or on the edge of the row shows a blank.
			if want != " " || (got != " " && got != "") {
				t.Errorf("col %d: caret cell %q, want %q", col, got, want)
			}
		}
		if hit := p.PosAt(60, 10, x, y); hit.Line != 1 || hit.Col != col {
			if col != 40_000 || hit.Line != 1 {
				t.Errorf("col %d: the caret cell (%d, %d) maps to %+v", col, x, y, hit)
			}
		}
	}
}

// The wheel and the caret move a tall line by rows, up and down, without skipping any.
func TestTallLineScrollsByRows(t *testing.T) {
	t.Parallel()
	d := newSegDoc("one", words(10_000), "three")
	p := Pane{Doc: d, Wrap: true, Top: 1}
	f, _ := p.newFrame(50, 6)
	all := allRows(f, 1)
	top, row := p.ScrollView(50, 6, 0)
	if top != 1 || row != 0 {
		t.Fatalf("ScrollView(0) = (%d, %d), want the pane's view", top, row)
	}
	p.TopRow = 0
	for k := 1; k <= 12; k++ {
		top, row = p.ScrollView(50, 6, k)
		if top != 1 || row != all[k].start {
			t.Fatalf("ScrollView(%d) = (%d, %d), want (1, %d)", k, top, row, all[k].start)
		}
	}
	p.TopRow = all[30].start
	if top, row = p.ScrollView(50, 6, -4); top != 1 || row != all[26].start {
		t.Errorf("ScrollView(-4) = (%d, %d), want (1, %d)", top, row, all[26].start)
	}
	// Up from the first row goes to the line above, and down past the last row to the next line.
	p.TopRow = 0
	if top, _ = p.ScrollView(50, 6, -1); top != 0 {
		t.Errorf("ScrollView(-1) from the first row: top = %d, want 0", top)
	}
	p.TopRow = all[len(all)-1].start
	if top, row = p.ScrollView(50, 6, 1); top != 2 || row != 0 {
		t.Errorf("ScrollView(1) from the last row = (%d, %d), want (2, 0)", top, row)
	}
	if top, row = (Pane{Doc: d, Wrap: true, Top: 2}).ScrollView(50, 6, -1); top != 1 || row != all[len(all)-1].start {
		t.Errorf("ScrollView(-1) into the tall line = (%d, %d), want (1, %d)", top, row, all[len(all)-1].start)
	}
}

// The hanging indent of a line applies to every continuation row of a tall line, those that
// start a segment included.
func TestTallLineKeepsTheHangingIndent(t *testing.T) {
	t.Parallel()
	text := "    " + words(12_000)
	d := newSegDoc(text)
	opts := Options{WrapIndent: true}
	p := Pane{Doc: d, Wrap: true, Opts: opts}
	f, _ := p.newFrame(60, 10)
	rows := allRows(f, 0)
	if len(rows) < 3 {
		t.Fatal("too few rows")
	}
	segEdge := -1
	for k, r := range rows {
		if r.ln.cont && r.k == 0 {
			segEdge = k
			break
		}
	}
	if segEdge < 0 {
		t.Fatal("no row starts a segment")
	}
	// Show the row at the edge of a segment, and the row before it.
	p.Top, p.TopRow = 0, rows[segEdge-1].start
	plain := rowsOf(p.Render(60, 10))
	for y := 1; y < 3; y++ {
		text := strings.SplitN(plain[y], "│ ", 2)[1]
		if !strings.HasPrefix(text, "    ") || strings.HasPrefix(text, "     ") {
			t.Errorf("row %d = %q, want exactly the hanging indent of 4 blanks", y, plain[y])
		}
	}
}
