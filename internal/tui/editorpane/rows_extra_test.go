package editorpane

import (
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
