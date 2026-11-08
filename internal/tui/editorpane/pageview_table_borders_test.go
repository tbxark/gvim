package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// borderPane returns the page view over a table with the bottom borders built by
// the shared extra-row hook.
func borderPane(doc *fakeDoc) Pane {
	p := composePane(doc, 30)
	p.PageExtraRows = PageViewTableBorders(doc, Styles{})
	return p
}

func TestTableTopAndBottomBorderRows(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| Name | Qty |", "| :--- | ---: |", "| a | 1 |", "| b | 22 |")
	doc.cursor = buffer.Pos{Line: 5, Col: 0} // the trailing "tail" line
	p := borderPane(doc)
	rows := p.Render(40, 8).Plain()
	want := []string{
		"┌──────┬─────┐",
		"│ Name │ Qty │",
		"├──────┼─────┤",
		"│ a    │   1 │",
		"│ b    │  22 │",
		"└──────┴─────┘",
	}
	for y, w := range want {
		got := strings.TrimRight(rows[y][4:], " ")
		if got != w {
			t.Fatalf("row %d = %q, want %q (all rows %q)", y, got, w, rows[:6])
		}
	}
}

func TestTableBorderRowsMapToTheirLines(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| Name | Qty |", "| :--- | ---: |", "| a | 1 |", "| b | 22 |")
	doc.cursor = buffer.Pos{Line: 5, Col: 0}
	p := borderPane(doc)
	// The top border is above the header: a press maps to the header line start.
	if hit := p.Hit(40, 8, 4, 0); hit.Kind != HitText || hit.Pos.Line != 0 || hit.Pos.Col != 0 {
		t.Fatalf("top border hit = %+v, want line 0 column 0", hit)
	}
	// The bottom border is below the last body line: a press maps to its end.
	last := doc.lines[3]
	if hit := p.Hit(40, 8, 4, 5); hit.Kind != HitText || hit.Pos.Line != 3 || hit.Pos.Col != len([]rune(last)) {
		t.Fatalf("bottom border hit = %+v, want line 3 column %d", hit, len([]rune(last)))
	}
}

func TestTableBorderRowsShiftTheCaretRow(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| Name | Qty |", "| --- | --- |", "| a | 1 |")
	doc.cursor = buffer.Pos{Line: 0, Col: 2} // the header line, revealed by the caret
	plain := composePane(doc, 30)
	withBorders := borderPane(doc)
	_, yPlain, okPlain := plain.Caret(40, 8)
	_, yBorders, okBorders := withBorders.Caret(40, 8)
	if !okPlain || !okBorders {
		t.Fatalf("caret missing: plain %v, borders %v", okPlain, okBorders)
	}
	if yBorders != yPlain+1 {
		t.Fatalf("caret row with borders = %d, without = %d, want one row lower", yBorders, yPlain)
	}
}

func TestTableBordersSkippedWhenTheTableWraps(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| aaaaaaaaaa | bbbbbbbbbb |", "| --- | --- |", "| c | d |")
	doc.cursor = buffer.Pos{Line: 4, Col: 0}
	p := Pane{Doc: doc, PageView: PageView{On: true, Measure: 11, Markdown: true},
		PageExtraRows: PageViewTableBorders(doc, Styles{})}
	rows := p.Render(12, 8).Plain()
	for _, row := range rows {
		if strings.Contains(row, "┌") || strings.Contains(row, "└") {
			t.Fatalf("a wrapped table drew a border: %q", rows)
		}
	}
}

func TestTableBordersOnlyOnTheComposedPane(t *testing.T) {
	t.Parallel()
	doc := tableDoc("| Name | Qty |", "| --- | --- |", "| a | 1 |")
	doc.cursor = buffer.Pos{Line: 3, Col: 0}
	// No page view: the hook is never asked for rows.
	p := Pane{Doc: doc, PageExtraRows: PageViewTableBorders(doc, Styles{})}
	if rows := p.Render(40, 8).Plain(); strings.Contains(strings.Join(rows, "\n"), "┌") {
		t.Fatalf("a normal pane drew table borders: %q", rows)
	}
}
