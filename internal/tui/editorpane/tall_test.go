package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// tallDoc is a document whose first line wraps to more rows than a pane of tallH rows
// holds, with the caret at the end of that line.
func tallDoc() (d *fakeDoc, rows int) {
	text := strings.Repeat("abcdefghi ", 40)
	d = newDoc(text, "END")
	d.cursor = buffer.Pos{Col: len([]rune(text))}
	return d, len(wrapSpans([]rune(text), tallW-6-2))
}

const (
	tallW = 26
	tallH = 8
)

// A caret in the last row of a line taller than the body can be revealed: the view
// starts inside the line, and the caret, the paint and the hit map agree on its row.
func TestRevealViewScrollsInsideATallLine(t *testing.T) {
	t.Parallel()
	d, rows := tallDoc()
	if rows <= tallH {
		t.Fatalf("the line wraps to %d rows, want more than %d", rows, tallH)
	}
	p := Pane{Doc: d, Wrap: true}
	if _, _, ok := p.Caret(tallW, tallH); ok {
		t.Fatal("the caret is on screen before the reveal, so the test shows nothing")
	}
	top, row := p.RevealView(tallW, tallH, 0)
	if top != 0 || row != rows-tallH {
		t.Fatalf("RevealView = (%d, %d), want (0, %d)", top, row, rows-tallH)
	}
	p.TopRow = row
	x, y, ok := p.Caret(tallW, tallH)
	if !ok || y != tallH-1 {
		t.Fatalf("caret at (%d, %d), ok %v; want the last body row", x, y, ok)
	}
	// The row the caret is on is the one the painter draws there, with no line number.
	g := p.Render(tallW, tallH)
	last := strings.TrimRight(g.Plain()[tallH-1], " ")
	if !strings.HasPrefix(last, "    │ ") || !strings.Contains(last, "abcdefghi") {
		t.Errorf("last row %q is not a continuation row of the paragraph", last)
	}
	if first := g.Plain()[0]; strings.HasPrefix(first, "  1") {
		t.Errorf("first row %q shows the line number, but its first row is scrolled off", first)
	}
	// A press on the last row lands on the caret's row, in the tall line.
	if pos := p.PosAt(tallW, tallH, 8, tallH-1); pos.Line != 0 || pos.Col < len([]rune(strings.Repeat("abcdefghi ", 40)))-20 {
		t.Errorf("PosAt on the last row = %v, want a column near the end of line 0", pos)
	}
}

func TestRevealViewKeepsAVisibleCaretAndMovesBackUp(t *testing.T) {
	t.Parallel()
	d, rows := tallDoc()
	p := Pane{Doc: d, Wrap: true, TopRow: rows - tallH}
	if top, row := p.RevealView(tallW, tallH, 0); top != 0 || row != rows-tallH {
		t.Errorf("a caret in view moved the view to (%d, %d)", top, row)
	}
	// The caret moves to the start of the line: its row is above the rows shown.
	d.cursor = buffer.Pos{}
	if top, row := p.RevealView(tallW, tallH, 0); top != 0 || row != 0 {
		t.Errorf("RevealView = (%d, %d) with the caret above the rows shown, want (0, 0)", top, row)
	}
	// The caret moves to a row in the middle that is above the rows shown.
	d.cursor = buffer.Pos{Col: 15}
	if _, row := p.RevealView(tallW, tallH, 0); row != 1 {
		t.Errorf("row %d with the caret on row 1, want 1", row)
	}
}

func TestRevealViewKeepsTheMarginInsideATallLine(t *testing.T) {
	t.Parallel()
	d, rows := tallDoc()
	d.cursor = buffer.Pos{Col: 100} // a row below the first screenful
	p := Pane{Doc: d, Wrap: true}
	_, row := p.RevealView(tallW, tallH, 0)
	_, rowM := p.RevealView(tallW, tallH, 2)
	if rowM != row+2 {
		t.Fatalf("a margin of 2 gives row %d, want %d, 2 more than the view with none", rowM, row+2)
	}
	p.TopRow = rowM
	_, y, ok := p.Caret(tallW, tallH)
	if !ok || y != tallH-1-2 {
		t.Errorf("caret on row %d (ok %v) with a margin of 2 in a body of %d rows, of %d line rows", y, ok, tallH, rows)
	}
}

func TestScrollViewStepsThroughATallLineByRows(t *testing.T) {
	t.Parallel()
	d, rows := tallDoc()
	p := Pane{Doc: d, Wrap: true}
	top, row := p.ScrollView(tallW, tallH, 3)
	if top != 0 || row != 3 {
		t.Fatalf("a notch down = (%d, %d), want (0, 3)", top, row)
	}
	p.TopRow = row
	if top, row = p.ScrollView(tallW, tallH, -1); top != 0 || row != 2 {
		t.Errorf("a step up = (%d, %d), want (0, 2)", top, row)
	}
	// Far down: the line is read to its last row, then the next line comes.
	p.TopRow = rows - 2
	if top, row = p.ScrollView(tallW, tallH, 3); top != 1 || row != 0 {
		t.Errorf("past the end of the line = (%d, %d), want (1, 0)", top, row)
	}
	// Back up from the next line: its last row is above, and the tall line's last row shows.
	p.Top, p.TopRow = 1, 0
	if top, row = p.ScrollView(tallW, tallH, -1); top != 0 || row != rows-1 {
		t.Errorf("up from the next line = (%d, %d), want (0, %d)", top, row, rows-1)
	}
}

// A document with no line taller than the body scrolls by lines, as it did, and keeps
// its view at the first row of its top line.
func TestScrollViewMovesByLinesWhenNoLineIsTall(t *testing.T) {
	t.Parallel()
	d := newDoc(strings.Repeat("a b c d e f g h i j ", 3), "two", "three", "four", "five")
	p := Pane{Doc: d, Wrap: true}
	if top, row := p.ScrollView(30, 4, 3); top != 3 || row != 0 {
		t.Errorf("ScrollView = (%d, %d), want (3, 0)", top, row)
	}
}

// layoutMemoDoc keeps the values the pane stores until its text changes, as the editor's
// document does, and counts the lines that were laid out.
type layoutMemoDoc struct {
	*fakeDoc
	vals   map[any]any
	misses int
}

func (d *layoutMemoDoc) MemoValue(key any, compute func() any) any {
	if v, ok := d.vals[key]; ok {
		return v
	}
	d.misses++
	v := compute()
	d.vals[key] = v
	return v
}

// The layout of a line is made once for the text: the caret, the rows and the hit map of
// the next frames take it from the document.
func TestLayoutOfALineIsKeptAcrossFrames(t *testing.T) {
	t.Parallel()
	fd, _ := tallDoc()
	d := &layoutMemoDoc{fakeDoc: fd, vals: map[any]any{}}
	p := Pane{Doc: d, Wrap: true}
	frames := func() {
		p.Caret(tallW, tallH)
		p.RevealView(tallW, tallH, 2)
		p.RowStarts(tallW, tallH, 0)
		p.PosAt(tallW, tallH, 8, 2)
	}
	frames()
	first := d.misses
	if first == 0 {
		t.Fatal("no line was laid out through the memo")
	}
	frames()
	if d.misses != first {
		t.Errorf("%d lines were laid out again by later frames, want none", d.misses-first)
	}
	// The cache holds what the layout was for: another width is another layout.
	p.Caret(tallW+10, tallH)
	if d.misses == first {
		t.Error("a frame of another width took the layout of the first")
	}
}
