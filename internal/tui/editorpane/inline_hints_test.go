package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// hintStyle is the faint look the root gives inline hints.
var hintStyle = grid.Style{FG: cGray, Faint: true}

// hintsAt returns an InlineHints hook with the hints of one line.
func hintsAt(line int, hints ...InlineHint) func(int) []InlineHint {
	return func(i int) []InlineHint {
		if i != line {
			return nil
		}
		return hints
	}
}

// hintRowText returns the text area of body row y (after the gutter), with trailing
// blanks removed.
func hintRowText(t *testing.T, g *grid.Grid, y int) string {
	t.Helper()
	var b strings.Builder
	for x := textX; x < g.Width()-1; x++ {
		b.WriteString(g.At(x, y).Text)
	}
	return strings.TrimRight(b.String(), " ")
}

// TestInlineHintInsertsCellsAtColumn checks the splice: the hint's cells land at its rune
// column, take the faint style, and the text after them moves right without changing the
// document.
func TestInlineHintInsertsCellsAtColumn(t *testing.T) {
	p := Pane{
		Doc:         newDoc("foo(bar)"),
		InlineHints: hintsAt(0, InlineHint{Col: 4, Text: "x: ", Style: hintStyle}),
	}
	g := p.Render(30, 2)
	if got := hintRowText(t, g, 0); got != "foo(x: bar)" {
		t.Fatalf("row = %q, want the hint spliced before b", got)
	}
	// The hint cells are faint, the document's cells are not.
	for x := textX; x < textX+9; x++ {
		c := cellAt(t, g, x, 0)
		wantFaint := x >= textX+4 && x < textX+7
		if c.Style.Faint != wantFaint {
			t.Errorf("cell x=%d %q faint=%v, want %v", x, c.Text, c.Style.Faint, wantFaint)
		}
	}
	if cellAt(t, g, textX, 0).Text != "f" || cellAt(t, g, textX+7, 0).Text != "b" {
		t.Fatalf("document cells moved: %q ... %q", cellAt(t, g, textX, 0).Text, cellAt(t, g, textX+7, 0).Text)
	}
}

// TestInlineHintsKeepServerOrderAtOneColumn checks multiple hints: the server order is
// kept, and two hints at the same column are concatenated in order.
func TestInlineHintsKeepServerOrderAtOneColumn(t *testing.T) {
	p := Pane{
		Doc: newDoc("xyz"),
		InlineHints: hintsAt(0,
			InlineHint{Col: 1, Text: "A", Style: hintStyle},
			InlineHint{Col: 1, Text: "B", Style: hintStyle},
			InlineHint{Col: 2, Text: "C", Style: hintStyle},
		),
	}
	g := p.Render(30, 2)
	if got := hintRowText(t, g, 0); got != "xAByCz" {
		t.Fatalf("row = %q, want xAByCz", got)
	}
}

// TestInlineHintColumnsUseExpandedCells checks the column conversion: a hint before a tab
// and a hint before a wide rune land on the right cells.
func TestInlineHintColumnsUseExpandedCells(t *testing.T) {
	t.Run("tab", func(t *testing.T) {
		p := Pane{Doc: newDoc("a\tb"), InlineHints: hintsAt(0, InlineHint{Col: 2, Text: "H", Style: hintStyle})}
		g := p.Render(30, 2)
		// The tab is four blanks; the hint after it comes before b.
		if got := hintRowText(t, g, 0); got != "a   Hb" {
			t.Fatalf("row = %q, want the hint after the tab", got)
		}
	})
	t.Run("wide rune", func(t *testing.T) {
		p := Pane{Doc: newDoc("a世b"), InlineHints: hintsAt(0, InlineHint{Col: 1, Text: "H", Style: hintStyle})}
		g := p.Render(30, 2)
		if got := hintRowText(t, g, 0); got != "aH世b" {
			t.Fatalf("row = %q, want the hint before 世", got)
		}
	})
}

// TestInlineHintAtWrapBoundaryStartsTheNextRow checks the row choice: a hint before a
// character that wrapped to the next row lands at that row's first column.
func TestInlineHintAtWrapBoundaryStartsTheNextRow(t *testing.T) {
	// Width 12, gutter 6: the wrap width is 4, so "abcdef" wraps as "abcd" / "ef".
	p := Pane{
		Doc:         newDoc("abcdef"),
		Wrap:        true,
		InlineHints: hintsAt(0, InlineHint{Col: 4, Text: "H", Style: hintStyle}),
	}
	g := p.Render(12, 3)
	if got := hintRowText(t, g, 0); got != "abcd" {
		t.Fatalf("first row = %q, want the plain wrap row", got)
	}
	if got := hintRowText(t, g, 1); got != "Hef" {
		t.Fatalf("second row = %q, want the hint before e", got)
	}
}

// TestInlineHintAfterSoftBreak checks a hint on a soft-break boundary: the break splits
// the rows, and the hint goes at the start of the row the character is on.
func TestInlineHintAfterSoftBreak(t *testing.T) {
	p := Pane{
		Doc:         newDoc("abcdef"),
		SoftBreaks:  func(line int) []SoftBreak { return []SoftBreak{{Col: 3}} },
		InlineHints: hintsAt(0, InlineHint{Col: 3, Text: "H", Style: hintStyle}),
	}
	g := p.Render(30, 2)
	if got := hintRowText(t, g, 0); got != "abc" {
		t.Fatalf("first row = %q, want abc", got)
	}
	if got := hintRowText(t, g, 1); got != "Hdef" {
		t.Fatalf("second row = %q, want the hint at its start", got)
	}
}

// TestInlineHintTruncatesOnANarrowPane checks the room rule: a hint wider than the row's
// remaining room is cut safely, and no document text is pushed past the pane's edge.
func TestInlineHintTruncatesOnANarrowPane(t *testing.T) {
	// Width 14, gutter 6: 7 columns for text plus the scrollbar column at 13.
	p := Pane{Doc: newDoc("abc"), InlineHints: hintsAt(0, InlineHint{Col: 1, Text: "0123456789", Style: hintStyle})}
	g := p.Render(14, 2)
	// Row width 3; room = 13 - 6 - 3 = 4, so the hint keeps 4 cells.
	if got := hintRowText(t, g, 0); got != "a0123bc" {
		t.Fatalf("row = %q, want the hint truncated to the room", got)
	}
	// Nothing was written into the scrollbar column.
	if c := cellAt(t, g, 13, 0); c.Text == "4" || c.Text == "5" {
		t.Fatalf("text reached the scrollbar column: %q", c.Text)
	}
}

// TestInlineHintFallsBackWhenTheRowHasNoRoom checks the tier-1 fallback: a full row keeps
// the end-of-line form, and the hint text is painted after the line.
func TestInlineHintFallsBackWhenTheRowHasNoRoom(t *testing.T) {
	// 20 wide: 13 usable columns. A 13-cell line fills them.
	p := Pane{Doc: newDoc("1234567890123"), InlineHints: hintsAt(0, InlineHint{Col: 2, Text: "H", Style: hintStyle})}
	g := p.Render(20, 2)
	if got := hintRowText(t, g, 0); got != "1234567890123" {
		t.Fatalf("row = %q, want no inline hint", got)
	}
	// The fallback row shows the hint after the line when there is a column left: the
	// 13 cells reach the last text column, so it is cut, but the call must not panic and
	// the document text is intact.
	if !strings.HasPrefix(hintRowText(t, g, 0), "1234567890") {
		t.Fatal("the document text was changed")
	}
}

// TestInlineHintFallbackInPageView checks that the page view keeps the end-of-line form:
// its composed columns are not the line's own, so no cells are inserted.
func TestInlineHintFallbackInPageView(t *testing.T) {
	p := Pane{
		Doc:         newDoc("abc"),
		PageView:    PageView{On: true, Measure: 20},
		InlineHints: hintsAt(0, InlineHint{Col: 1, Text: "H", Style: hintStyle}),
	}
	g := p.Render(30, 2)
	row := g.Plain()[0]
	if strings.Contains(row, "aHbc") {
		t.Fatalf("row = %q, the page view must not splice the hint", row)
	}
	if !strings.Contains(row, "abc") || !strings.Contains(row, "H") {
		t.Fatalf("row = %q, want the text and the end-of-line hint", row)
	}
}

// TestInlineHintInsideAConcealedRangeFallsBack checks the conceal rule: an insertion point
// inside a hidden range keeps the end-of-line form, so the hidden run is not split.
func TestInlineHintInsideAConcealedRangeFallsBack(t *testing.T) {
	p := Pane{
		Doc:         newDoc("abcdef"),
		Conceals:    func(int) []ConcealSpan { return []ConcealSpan{{Start: 2, End: 5, Text: "..."}} },
		InlineHints: hintsAt(0, InlineHint{Col: 3, Text: "H", Style: hintStyle}),
	}
	g := p.Render(30, 2)
	row := g.Plain()[0]
	if strings.Contains(row, "abHc") {
		t.Fatalf("row = %q, the hint split the concealed run", row)
	}
	if !strings.Contains(row, "H") {
		t.Fatalf("row = %q, want the fallback hint", row)
	}
}

// TestInlineHintDoesNotChangeVirtualRows checks that extra rows are unaffected below and
// above a line with an inline hint.
func TestInlineHintDoesNotChangeVirtualRows(t *testing.T) {
	p := Pane{
		Doc: newDoc("abc"),
		ExtraRows: func(line int) ExtraRows {
			if line != 0 {
				return ExtraRows{}
			}
			return ExtraRows{Above: []ExtraRow{{Text: "above"}}, Below: []ExtraRow{{Text: "below"}}}
		},
		InlineHints: hintsAt(0, InlineHint{Col: 1, Text: "H", Style: hintStyle}),
	}
	g := p.Render(30, 4)
	rows := rowsOf(g)
	if !strings.Contains(rows[0], "above") {
		t.Fatalf("row 0 = %q, want the virtual row above", rows[0])
	}
	if got := hintRowText(t, g, 1); got != "aHbc" {
		t.Fatalf("text row = %q, want the hint", got)
	}
	if !strings.Contains(rows[2], "below") {
		t.Fatalf("row 2 = %q, want the virtual row below", rows[2])
	}
}

// TestCaretAndMouseMapAroundInsertedHintCells checks that the caret moves with the text it
// is on, a press maps through the inserted cells, and a press on the hint maps to the
// character the hint annotates.
func TestCaretAndMouseMapAroundInsertedHintCells(t *testing.T) {
	doc := newDoc("abcd")
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	p := Pane{Doc: doc, InlineHints: hintsAt(0, InlineHint{Col: 2, Text: "XY", Style: hintStyle})}
	x, y, ok := p.Caret(30, 3)
	if !ok || y != 0 || x != textX+4 {
		t.Fatalf("caret = (%d,%d,%v), want the char c moved right by the 2 hint cells", x, y, ok)
	}
	// A press on the first hint cell is a press on the hint's anchor (column 2).
	if got := p.PosAt(30, 3, textX+2, 0); got != (buffer.Pos{Line: 0, Col: 2}) {
		t.Fatalf("PosAt on the hint = %+v, want column 2", got)
	}
	// A press on c (after the hint cells) is column 2 as well, and one on b is column 1.
	if got := p.PosAt(30, 3, textX+4, 0); got != (buffer.Pos{Line: 0, Col: 2}) {
		t.Fatalf("PosAt on c = %+v, want column 2", got)
	}
	if got := p.PosAt(30, 3, textX+1, 0); got != (buffer.Pos{Line: 0, Col: 1}) {
		t.Fatalf("PosAt on b = %+v, want column 1", got)
	}
}

// TestInlineHintNoneWithoutTheHook checks the zero value: no hook paints no hint cells.
func TestInlineHintNoneWithoutTheHook(t *testing.T) {
	g := Pane{Doc: newDoc("abc")}.Render(30, 2)
	if got := hintRowText(t, g, 0); got != "abc" {
		t.Fatalf("row = %q, want the plain text", got)
	}
}

// TestInlineHintPannedRowKeepsTheFallbackRule checks the horizontal scroll rule: a panned
// row splices no hint cells (its columns are cut from the left).
func TestInlineHintPannedRowKeepsTheFallbackRule(t *testing.T) {
	// 12 wide: 5 text columns, so a 10-cell line overflows and the view pans.
	p := Pane{Doc: newDoc("abcdefghij"), Left: 3, HideHorizontalScrollbar: true,
		InlineHints: hintsAt(0, InlineHint{Col: 5, Text: "H", Style: hintStyle})}
	g := p.Render(12, 2)
	if strings.Contains(g.Plain()[0], "H") {
		t.Fatalf("row = %q, a panned row must not splice a hint", g.Plain()[0])
	}
}
