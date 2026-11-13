package editorpane

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// rowText returns n cells of row y from column x as text (cell-based, not byte-based).
func rowText(g *grid.Grid, y, x, n int) string {
	var b strings.Builder
	for k := x; k < x+n; k++ {
		b.WriteString(g.At(k, y).Text)
	}
	return b.String()
}

// extraAt returns an ExtraRows hook for the given lines.
func extraAt(rows map[int]ExtraRows) func(int) ExtraRows {
	return func(line int) ExtraRows { return rows[line] }
}

// TestExtraRowsPaintAndMap checks virtual rows above and below a line are painted in slice
// order, with the gutter glyph, and that caret/hit mapping counts them.
func TestExtraRowsPaintAndMap(t *testing.T) {
	p := Pane{
		Doc: newDoc("abc", "def"),
		ExtraRows: extraAt(map[int]ExtraRows{0: {
			Above: []ExtraRow{{Text: "first", Priority: 1}, {Text: "second", GutterText: "-"}},
			Below: []ExtraRow{{Text: "below"}},
		}}),
	}
	g := p.Render(20, 4)
	if got := rowText(g, 0, textX, 6); got != "second" {
		t.Fatalf("row 0 = %q, want the lowest priority above row painted first", got)
	}
	if got := rowText(g, 1, textX, 6); got != "first " {
		t.Fatalf("row 1 = %q, want the last above row nearest the line", got)
	}
	if got := rowText(g, 2, textX, 3); got != "abc" {
		t.Fatalf("row 2 = %q", got)
	}
	if got := rowText(g, 3, textX, 5); got != "below" {
		t.Fatalf("row 3 = %q", got)
	}
	if got := g.At(textX-1, 0).Text; got != "-" {
		t.Fatalf("gutter glyph = %q, want %q", got, "-")
	}
	// The caret on line 0 gains one row per above row.
	doc := newDoc("abc", "def")
	doc.cursor = buffer.Pos{Line: 0, Col: 1}
	p.Doc = doc
	if x, y, ok := p.Caret(20, 4); !ok || y != 2 || x != textX+1 {
		t.Fatalf("caret = (%d,%d,%v), want y=2", x, y, ok)
	}
	if got := p.PosAt(20, 4, textX, 0); got != (buffer.Pos{Line: 0, Col: 0}) {
		t.Fatalf("PosAt on an above row = %+v, want the line start", got)
	}
	if got := p.PosAt(20, 4, textX+2, 3); got != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("PosAt on a below row = %+v, want the line end", got)
	}
}

// TestSoftBreakSplitsRow checks a soft break adds a row with the indent and prefix, that
// the text after it keeps its columns and that caret/hit mapping follows the row.
func TestSoftBreakSplitsRow(t *testing.T) {
	p := Pane{
		Doc: newDoc("abcdef"),
		SoftBreaks: func(line int) []SoftBreak {
			if line != 0 {
				return nil
			}
			return []SoftBreak{{Col: 3, Indent: 2, PrefixText: "> ", PrefixStyle: grid.Style{Bold: true}}}
		},
	}
	g := p.Render(20, 2)
	if got := rowText(g, 0, textX, 3); got != "abc" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowText(g, 1, textX, 8); got != "  > def " {
		t.Fatalf("row 1 = %q, want %q", got, "  > def ")
	}
	// The caret at rune column 3 (just after the break) is the first cell after the lead.
	doc := newDoc("abcdef")
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p.Doc = doc
	if x, y, ok := p.Caret(20, 2); !ok || y != 1 || x != textX+4 {
		t.Fatalf("caret = (%d,%d,%v), want y=1 x=%d", x, y, ok, textX+4)
	}
	if got := p.PosAt(20, 2, textX+4, 1); got != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("PosAt after the lead = %+v", got)
	}
	if got := p.PosAt(20, 2, textX+1, 1); got != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("PosAt in the indent = %+v, want the row start", got)
	}
}

// TestSoftBreakActivation checks unless-cursor-in skips the break on the caret's line and
// if-cursor-in applies it only there.
func TestSoftBreakActivation(t *testing.T) {
	doc := newDoc("abcdef", "abcdef")
	doc.cursor = buffer.Pos{Line: 1}
	unless := Pane{Doc: doc, SoftBreaks: func(line int) []SoftBreak {
		return []SoftBreak{{Col: 3, Indent: 1, Activation: ConcealUnlessCursorIn}}
	}}
	f, ok := unless.newFrame(20, 4)
	if !ok {
		t.Fatal("no frame")
	}
	if got := f.rowCount(0); got != 2 {
		t.Fatalf("unless-cursor-in: line 0 rows = %d, want 2", got)
	}
	if got := f.rowCount(1); got != 1 {
		t.Fatalf("unless-cursor-in: caret line rows = %d, want 1", got)
	}
	ifOnly := Pane{Doc: doc, SoftBreaks: func(line int) []SoftBreak {
		return []SoftBreak{{Col: 3, Indent: 1, Activation: ConcealIfCursorIn}}
	}}
	f, ok = ifOnly.newFrame(20, 4)
	if !ok {
		t.Fatal("no frame")
	}
	if got := f.rowCount(0); got != 1 {
		t.Fatalf("if-cursor-in: non-caret rows = %d, want 1", got)
	}
	if got := f.rowCount(1); got != 2 {
		t.Fatalf("if-cursor-in: caret line rows = %d, want 2", got)
	}
}

// TestScrollbarThumbCountsInsertedRows checks the thumb scales by display rows when
// virtual rows make the document taller: the length shrinks and the last line scrolls the
// thumb to the bottom of the track.
func TestScrollbarThumbCountsInsertedRows(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	doc := newDoc(lines...)
	p := Pane{Doc: doc, ExtraRows: func(line int) ExtraRows {
		if line == 0 {
			return ExtraRows{Above: []ExtraRow{{Text: "one"}, {Text: "two"}, {Text: "three"}}}
		}
		return ExtraRows{}
	}}
	f, ok := p.newFrame(20, 5)
	if !ok {
		t.Fatal("no frame")
	}
	total, ok := f.rowTotal()
	if !ok || total != 13 {
		t.Fatalf("rowTotal = %d, %v; want 13", total, ok)
	}
	start, length := f.thumb()
	if length >= 5 || start != 0 {
		t.Fatalf("thumb at the top = (%d,%d), want a shorter thumb at 0", start, length)
	}
	// Scroll to the last line: the thumb must reach the bottom of the track.
	last := Pane{Doc: doc, Top: 9, ExtraRows: p.ExtraRows}
	last.Top = 9
	lf, ok := last.newFrame(20, 5)
	if !ok {
		t.Fatal("no frame")
	}
	before, ok := lf.rowsBeforeTop()
	if !ok || before != 12 {
		t.Fatalf("rowsBeforeTop = %d, %v; want 12", before, ok)
	}
	s2, l2 := lf.thumb()
	if s2 != 5-l2 {
		t.Fatalf("thumb at the end = (%d,%d), want it at the bottom", s2, l2)
	}
}

// TestExtraRowsWrapAtThePaneEdge checks a virtual row longer than the text width wraps
// into display rows (like text rows), with the gutter glyph on the first chunk only,
// overlays re-based per chunk and every chunk mapping to the anchor line.
func TestExtraRowsWrapAtThePaneEdge(t *testing.T) {
	overlay := ExtraTextOverlay{Start: 15, End: 20, Style: grid.Style{Bold: true}}
	p := Pane{
		Doc: newDoc("abc"),
		ExtraRows: extraAt(map[int]ExtraRows{0: {Above: []ExtraRow{{
			Text: "abcdefghijklmnopqrstuvwxyz", GutterText: "-", Overlays: []ExtraTextOverlay{overlay},
		}}}}),
	}
	// Width 20 with the 6-column gutter leaves 13 text columns: 26 runes -> 2 rows.
	f, ok := p.newFrame(20, 4)
	if !ok {
		t.Fatal("no frame")
	}
	ex := f.extrasFor(0)
	if len(ex.Above) != 2 {
		t.Fatalf("wrapped rows = %d, want 2: %+v", len(ex.Above), ex.Above)
	}
	if ex.Above[0].GutterText != "-" || ex.Above[1].GutterText != "" {
		t.Fatalf("gutter glyphs = %q, %q; want it on the first chunk only", ex.Above[0].GutterText, ex.Above[1].GutterText)
	}
	if len(ex.Above[1].Overlays) != 1 || ex.Above[1].Overlays[0].Start != 2 || ex.Above[1].Overlays[0].End != 7 {
		t.Fatalf("second chunk overlays = %+v", ex.Above[1].Overlays)
	}
	g := p.Render(20, 4)
	if got := rowText(g, 0, textX, 13); got != "abcdefghijklm" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowText(g, 1, textX, 13); got != "nopqrstuvwxyz" {
		t.Fatalf("row 1 = %q", got)
	}
	// Both chunks of the wrapped virtual row map to the anchor line's start.
	if got := p.PosAt(20, 4, textX, 1); got != (buffer.Pos{Line: 0, Col: 0}) {
		t.Fatalf("PosAt on the second chunk = %+v", got)
	}
	// The caret on the line moves down by both chunks.
	doc := newDoc("abc")
	doc.cursor = buffer.Pos{Line: 0, Col: 1}
	p.Doc = doc
	if _, y, ok := p.Caret(20, 4); !ok || y != 2 {
		t.Fatalf("caret row = %d, %v; want 2", y, ok)
	}
}

// TestExtraRowOverlaysPaintFromTheRowStartAndBackToBack checks that an overlay that starts
// at the row's first byte, or right where the one before it ends, is painted.
func TestExtraRowOverlaysPaintFromTheRowStartAndBackToBack(t *testing.T) {
	p := Pane{
		Doc: newDoc("abc"),
		ExtraRows: extraAt(map[int]ExtraRows{0: {Above: []ExtraRow{{
			Text: "abcdef",
			Overlays: []ExtraTextOverlay{
				{Start: 0, End: 2, Style: grid.Style{FG: cRed}},
				{Start: 2, End: 4, Style: grid.Style{FG: cBlue}},
			},
		}}}}),
	}
	g := p.Render(20, 3)
	for x, want := range []color.Color{cRed, cRed, cBlue, cBlue} {
		if got := cellAt(t, g, textX+x, 0).Style.FG; got != want {
			t.Errorf("cell %d FG = %v, want %v", x, got, want)
		}
	}
	if got := cellAt(t, g, textX+4, 0).Style.FG; got == cRed || got == cBlue {
		t.Errorf("cell past the overlays FG = %v, want the row's own", got)
	}
}

// TestExtrasForDoesNotReorderTheHookSlices checks that sorting the rows of a line leaves
// the slices the hook returned as they were, so a plugin that keeps and reuses them is
// not reordered under it.
func TestExtrasForDoesNotReorderTheHookSlices(t *testing.T) {
	above := []ExtraRow{{Text: "late", Priority: 5}, {Text: "early", Priority: 1}}
	below := []ExtraRow{{Text: "b2", Priority: 2}, {Text: "b1", Priority: 1}}
	p := Pane{
		Doc: newDoc("abc"),
		ExtraRows: func(int) ExtraRows {
			return ExtraRows{Above: above, Below: below}
		},
	}
	f, ok := p.newFrame(40, 6)
	if !ok {
		t.Fatal("no frame")
	}
	ex := f.extrasFor(0)
	if len(ex.Above) != 2 || ex.Above[0].Text != "early" || ex.Below[0].Text != "b1" {
		t.Fatalf("rows = %+v / %+v, want them in priority order", ex.Above, ex.Below)
	}
	if above[0].Text != "late" || below[0].Text != "b2" {
		t.Fatalf("the hook slices were reordered: %+v / %+v", above, below)
	}
}
