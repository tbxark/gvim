package editorpane

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// The pane under test is 20 columns wide: a 6 column gutter, 13 text columns and the
// scrollbar column; with Wrap on the wrap width is 12.
const (
	visW = 20
	visH = 6
)

func TestVisibleRowsSpans(t *testing.T) {
	tests := []struct {
		name string
		pane Pane
		want []RowSpan
	}{
		{"one row per line", Pane{Doc: newDoc("one", "two")},
			[]RowSpan{{Line: 0, To: 3}, {Line: 1, To: 3}}},
		{"a wrapped line splits at the space", Pane{Doc: newDoc("aaaa bbbb cccc dddd"), Wrap: true},
			[]RowSpan{{Line: 0, From: 0, To: 10}, {Line: 0, From: 10, To: 19, Wrapped: true}}},
		{"wrap off keeps the line in one row", Pane{Doc: newDoc("aaaa bbbb cccc dddd")},
			[]RowSpan{{Line: 0, From: 0, To: 13}}},
		{"a tab is one rune and several cells", Pane{Doc: newDoc("\tab"), TabSize: 4},
			[]RowSpan{{Line: 0, To: 3}}},
		{"wide characters count their bytes", Pane{Doc: newDoc("日本語日本語日本語"), Wrap: true},
			[]RowSpan{{Line: 0, From: 0, To: 18}, {Line: 0, From: 18, To: 27, Wrapped: true}}},
		{"a collapsed fold lists its header and skips the body", Pane{Doc: foldFixture(map[int]bool{0: true})},
			[]RowSpan{{Line: 0, To: 10}, {Line: 3, To: 1}, {Line: 4, To: 1}}},
		{"an open fold lists every line", Pane{Doc: foldFixture(nil)},
			[]RowSpan{{Line: 0, To: 10}, {Line: 1, To: 7}, {Line: 2, To: 7}, {Line: 3, To: 1}, {Line: 4, To: 1}}},
		{"the top line starts the list", Pane{Doc: newDoc("a", "b", "c"), Top: 1},
			[]RowSpan{{Line: 1, To: 1}, {Line: 2, To: 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pane.VisibleRows(visW, visH, 500)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("VisibleRows = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVisibleRowsStopAtTheBodyAndTheLimit(t *testing.T) {
	doc := newDoc("a", "b", "c", "d", "e", "f", "g", "h")
	tests := []struct {
		name         string
		h, limit     int
		wantLen      int
		wantLastLine int
	}{
		{"the body bounds the list", 3, 500, 3, 2},
		{"the limit bounds the list", 6, 2, 2, 1},
		{"a zero limit lists nothing", 6, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Pane{Doc: doc}.VisibleRows(visW, tc.h, tc.limit)
			if len(got) != tc.wantLen || (tc.wantLen > 0 && got[len(got)-1].Line != tc.wantLastLine) {
				t.Fatalf("VisibleRows = %+v, want %d rows ending at line %d", got, tc.wantLen, tc.wantLastLine)
			}
		})
	}
}

func TestVisibleRowsOfAScrolledPaneStartAtTheLeftColumn(t *testing.T) {
	line := strings.Repeat("0123456789", 6)
	tests := []struct {
		name      string
		left      int
		wantFrom  int
		wantWidth int // columns of the line in the row
	}{
		{"not scrolled", 0, 0, 13},
		{"scrolled by 20", 20, 20, 13},
		{"scrolled to the end", 100, 47, 13}, // 60 columns, a window of 13: the last column is 59
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: newDoc(line), Left: tc.left, KeepLeft: true}
			left, _ := p.HScroll(visW, visH)
			rows := p.VisibleRows(visW, visH, 500)
			if len(rows) != 1 || rows[0].From != left || rows[0].To-rows[0].From != tc.wantWidth {
				t.Fatalf("rows = %+v, HScroll left = %d, want one row of %d columns from the left column", rows, left, tc.wantWidth)
			}
			if left != tc.wantFrom {
				t.Fatalf("left = %d, want %d", left, tc.wantFrom)
			}
		})
	}
}

// TestVisibleRowsMatchRender checks the list against the grid the pane paints: row k of
// the list is the text of body row k, and the rows past the list are the "~" rows.
func TestVisibleRowsMatchRender(t *testing.T) {
	long := strings.Repeat("word ", 14)
	tests := []struct {
		name string
		pane Pane
	}{
		{"wrap", Pane{Doc: newDoc("short", long, "tail"), Wrap: true}},
		{"wrap with wide characters", Pane{Doc: &fakeDoc{lines: []string{"日本語 日本語 日本語 日本語 end", "x"}, cursor: buffer.Pos{Line: 1}}, Wrap: true}},
		{"no wrap, scrolled by the caret", Pane{Doc: &fakeDoc{lines: []string{long + long, "b"}, cursor: buffer.Pos{Col: 60}}}},
		{"a collapsed fold", Pane{Doc: foldFixture(map[int]bool{0: true}), Wrap: true}},
		{"a scrolled top", Pane{Doc: newDoc("a", "b", long, "d"), Top: 2, Wrap: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.pane.Render(visW, visH)
			rows := tc.pane.VisibleRows(visW, visH, 500)
			gutter := GutterWidth(tc.pane.Doc.LineCount())
			body := tc.pane.BodyRows(visW, visH)
			tilde := 0
			for y := range body {
				if cellAt(t, g, 0, y).Text == "~" {
					tilde++
				}
			}
			if len(rows) != body-tilde {
				t.Fatalf("%d rows listed, but %d body rows have text", len(rows), body-tilde)
			}
			for y, r := range rows {
				var painted strings.Builder
				for x := gutter; x < visW-1; x++ {
					painted.WriteString(cellAt(t, g, x, y).Text)
				}
				text := tc.pane.Doc.Line(r.Line)
				want := strings.TrimRight(text[r.From:r.To], " ")
				// A collapsed fold paints its placeholder after the text of the header.
				if got := strings.TrimRight(painted.String(), " "); !strings.HasPrefix(got, want) || (r.Wrapped && got != want) {
					t.Errorf("row %d (line %d [%d,%d)) paints %q, the list says %q", y, r.Line, r.From, r.To, got, want)
				}
				if r.Wrapped != (r.From > 0 && tc.pane.Wrap) {
					t.Errorf("row %d: Wrapped = %v with From = %d", y, r.Wrapped, r.From)
				}
			}
		})
	}
}

func TestViewIsBodyRowsAndHScroll(t *testing.T) {
	tests := []struct {
		name string
		pane Pane
	}{
		{"no horizontal bar", Pane{Doc: newDoc("short")}},
		{"a bar takes a row", Pane{Doc: newDoc(strings.Repeat("x", 80))}},
		{"scrolled by the caret", Pane{Doc: &fakeDoc{lines: []string{strings.Repeat("x", 80)}, cursor: buffer.Pos{Col: 70}}}},
		{"wrap", Pane{Doc: newDoc(strings.Repeat("x y ", 20)), Wrap: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, left := tc.pane.View(visW, visH)
			wantLeft, _ := tc.pane.HScroll(visW, visH)
			if rows != tc.pane.BodyRows(visW, visH) || left != wantLeft {
				t.Fatalf("View = (%d, %d), want (%d, %d)", rows, left, tc.pane.BodyRows(visW, visH), wantLeft)
			}
		})
	}
	if rows, left := (Pane{}).View(visW, visH); rows != visH || left != 0 {
		t.Fatalf("View of a pane with no document = (%d, %d), want (%d, 0)", rows, left, visH)
	}
}

func TestRuneStartsAndByteStarts(t *testing.T) {
	raw := []rune("a\tb\x01日")
	if got, want := runeStarts(raw, 4), []int{0, 1, 4, 5, 9, 10}; !slices.Equal(got, want) {
		t.Fatalf("runeStarts = %v, want %v", got, want)
	}
	if got, want := byteStarts(raw), []int{0, 1, 2, 3, 4, 7}; !slices.Equal(got, want) {
		t.Fatalf("byteStarts = %v, want %v", got, want)
	}
}

// TestVisibleRowsOfALongLineCountBytes checks that the columns of a row of a long line of a
// large document are byte offsets into the line, though the window of the line starts at a
// rune column: a line of 3 byte runes scrolled by 100 runes starts at byte 300.
func TestVisibleRowsOfALongLineCountBytes(t *testing.T) {
	const runes = 20_000 // 60000 bytes, past LongLineRunes
	line := strings.Repeat("日", runes)
	tests := []struct {
		name string
		left int
	}{
		{"not scrolled", 0},
		{"scrolled by 100 runes", 100},
		{"scrolled far", 15_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: newSpy(3, func(int) string { return line }), Left: tc.left, KeepLeft: true}
			left, _ := p.HScroll(visW, visH)
			if left != tc.left {
				t.Fatalf("left = %d, want %d", left, tc.left)
			}
			rows := p.VisibleRows(visW, visH, 500)
			if len(rows) == 0 {
				t.Fatal("no rows")
			}
			for _, r := range rows {
				if r.From != 3*tc.left || (r.To-r.From)%3 != 0 || r.To <= r.From {
					t.Fatalf("row %+v, want From = %d bytes and a span of whole runes", r, 3*tc.left)
				}
				if got := line[r.From:r.To]; !strings.HasPrefix(got, "日") || len(got) != r.To-r.From {
					t.Fatalf("line[%d:%d] = %q", r.From, r.To, got)
				}
			}
		})
	}
}
