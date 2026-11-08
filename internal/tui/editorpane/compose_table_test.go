package editorpane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/syntax"
)

// tableDoc returns a Markdown document with a table and a trailing line so the
// table stays concealed.
func tableDoc(lines ...string) *fakeDoc {
	all := append(append([]string{}, lines...), "after")
	doc := newDoc(all...)
	doc.cursor = buffer.Pos{Line: len(all) - 1, Col: 0}
	return doc
}

func TestTableBoxDrawing(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| Name | Qty |", "| :--- | ---: |", "| a | 1 |", "| b | 22 |")
	doc.cursor = buffer.Pos{Line: 4, Col: 0} // appended "tail"
	p := composePane(doc, 30)
	rows := p.Render(40, 8).Plain()
	// Widths: Name=4, Qty=3; the row is 1 + (4+3) + (3+3) = 14 cells, margin 4.
	want := []string{
		"│ Name │ Qty │",
		"├──────┼─────┤",
		"│ a    │   1 │",
		"│ b    │  22 │",
	}
	for y, w := range want {
		got := strings.TrimRight(rows[y][4:], " ")
		if got != w {
			t.Fatalf("row %d = %q, want %q", y, got, w)
		}
		if got := ansi.StringWidth(got); got != 14 {
			t.Fatalf("row %d width = %d, want 14", y, got)
		}
	}
}

func TestTableWideCells(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| 名字 | x |", "| --- | --- |", "| a | y |")
	doc.cursor = buffer.Pos{Line: 3, Col: 0}
	p := composePane(doc, 30)
	rows := p.Render(40, 8).Plain()
	row := strings.TrimRight(rows[0][4:], " ")
	if !strings.Contains(row, "名字") {
		t.Fatalf("row = %q, want the wide cells kept", row)
	}
	if got := ansi.StringWidth(row); got != 1+(4+3)+(1+3) {
		t.Fatalf("row width = %d, want the wide cell counted as 4 cells", got)
	}
	// The body's short first cell is padded to the wide column.
	if got := strings.TrimRight(rows[2][4:], " "); got != "│ a    │ y │" {
		t.Fatalf("body row = %q", got)
	}
}

func TestTableCaretRevealKeepsTheFrameWidth(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| **a** | b |", "| --- | --- |", "| x | y |")
	p := composePane(doc, 30)
	// The caret on the header reveals the raw markers, and the column is already
	// wide enough (5 cells) so the frame does not grow.
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	rows := p.Render(40, 8).Plain()
	row := strings.TrimRight(rows[0][4:], " ")
	if !strings.Contains(row, "**a**") {
		t.Fatalf("revealed header = %q, want the raw markers", row)
	}
	if got := ansi.StringWidth(row); got != 1+(5+3)+(1+3) {
		t.Fatalf("revealed row width = %d, want the frame's width", got)
	}
}

func TestTableHitMapsIntoTheCells(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| Name | Qty |", "| --- | --- |", "| a | 1 |")
	doc.cursor = buffer.Pos{Line: 3, Col: 0}
	p := composePane(doc, 30)
	// Row 0 is "│ Name │ Qty │"; the "N" sits at composed column 2 (margin 4).
	if hit := p.Hit(40, 8, 4+2, 0); hit.Kind != HitText || hit.Pos.Line != 0 || hit.Pos.Col != 2 {
		t.Fatalf("hit = %+v, want source line 0 column 2", hit)
	}
	// The Qty cell's "Q" is at composed column 9 (1 + 6 + 2), source column 9.
	if hit := p.Hit(40, 8, 4+9, 0); hit.Pos.Col != 9 {
		t.Fatalf("hit = %+v, want source column 9", hit)
	}
}

func TestTableSelectionPaintsTheCells(t *testing.T) {
	t.Parallel()
	inner := tableDoc("| Name | Qty |", "| --- | --- |", "| a | 1 |")
	inner.cursor = buffer.Pos{Line: 3, Col: 0}
	doc := composeSelDoc{fakeDoc: inner,
		anchor: buffer.Pos{Line: 0, Col: 2}, head: buffer.Pos{Line: 0, Col: 6}}
	p := Pane{Doc: doc, PageView: PageView{On: true, Measure: 30, Markdown: true}}
	g := p.Render(40, 8)
	// Composed columns 2..5 are "Name"; margin 4.
	for x := 4 + 2; x < 4+6; x++ {
		if !g.At(x, 0).Style.Reverse {
			t.Fatalf("cell %d is not painted selected", x)
		}
	}
	if g.At(4+1, 0).Style.Reverse {
		t.Fatal("the cell padding is painted selected")
	}
}

// memoTableDoc is a fake document with a layout memo, so a test can check that
// the table widths are recomputed when the text changes.
type memoTableDoc struct {
	*fakeDoc
	memo map[any]any
}

func (d *memoTableDoc) MemoValue(key any, compute func() any) any {
	if v, ok := d.memo[key]; ok {
		return v
	}
	v := compute()
	d.memo[key] = v
	return v
}

func TestTableWidthsRecomputeOnEdit(t *testing.T) {
	t.Parallel()
	inner := tableDoc("| a | b |", "| --- | --- |", "| c | d |")
	inner.cursor = buffer.Pos{Line: 3, Col: 0}
	doc := &memoTableDoc{fakeDoc: inner, memo: map[any]any{}}
	p := Pane{Doc: doc, PageView: PageView{On: true, Measure: 30, Markdown: true}}
	first := strings.TrimRight(p.Render(40, 8).Plain()[0][4:], " ")
	if first != "│ a │ b │" {
		t.Fatalf("first render = %q, want the narrow frame", first)
	}
	// A wider cell on the same version keeps the cached width...
	doc.lines[2] = "| longer | d |"
	if got := strings.TrimRight(p.Render(40, 8).Plain()[0][4:], " "); got != first {
		t.Fatalf("cached width changed before the text version: %q", got)
	}
	// ...and the new text version recomputes it.
	doc.memo = map[any]any{}
	wider := strings.TrimRight(p.Render(40, 8).Plain()[0][4:], " ")
	if ansi.StringWidth(wider) != 14 || ansi.StringWidth(wider) <= ansi.StringWidth(first) {
		t.Fatalf("widths after the edit = %q (was %q), want wider columns", wider, first)
	}
	if !strings.Contains(wider, "│ a      │ b │") {
		t.Fatalf("row after the edit = %q, want the column widened", wider)
	}
}

func TestTablesInFencedCodeStayLiteral(t *testing.T) {
	t.Parallel()
	doc := tableDoc("```", "| a | b |", "| --- | --- |", "```")
	p := composePane(doc, 30)
	rows := p.Render(40, 8).Plain()
	// The fenced body line is framed as code, not box-drawn as a table.
	if got := strings.TrimRight(rows[1][4:], " "); !strings.HasPrefix(got, "│ | a | b |") {
		t.Fatalf("fenced body = %q, want it left as code", got)
	}
}

var _ = syntax.KindPlain
