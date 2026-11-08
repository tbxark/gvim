package editorpane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
)

// wrapTableDoc returns a table whose cells are too wide for a narrow page, with
// the caret on a trailing line so the table's rows are composed.
func wrapTableDoc(lines ...string) *fakeDoc {
	all := append(append([]string{}, lines...), "after", "tail")
	doc := newDoc(all...)
	doc.cursor = buffer.Pos{Line: len(all) - 1, Col: 0}
	return doc
}

func TestWrappedTableCellsUseContinuationRows(t *testing.T) {
	t.Parallel()
	doc := wrapTableDoc("| alpha beta gamma | q |", "| --- | --- |", "| delta epsilon zeta | r |")
	p := composePane(doc, 20)
	p.PageExtraRows = PageViewTableBorders(doc, Styles{})
	rows := p.Render(24, 12).Plain()
	trim := func(y int) string { return strings.TrimRight(rows[y][1:], " ") }
	if !strings.HasPrefix(trim(0), "┌") {
		t.Fatalf("row 0 = %q, want the top border", trim(0))
	}
	if !strings.Contains(trim(1), "alpha beta") {
		t.Fatalf("row 1 = %q, want the first segment of the header", trim(1))
	}
	if !strings.Contains(trim(2), "gamma") {
		t.Fatalf("row 2 = %q, want the header's continuation row", trim(2))
	}
	if !strings.HasPrefix(trim(3), "├") {
		t.Fatalf("row 3 = %q, want the mid border after the header's rows", trim(3))
	}
	if !strings.Contains(trim(4), "delta") {
		t.Fatalf("row 4 = %q, want the body's first segment", trim(4))
	}
	joined := trim(5) + trim(6)
	if !strings.Contains(joined, "epsilon") && !strings.Contains(joined, "zeta") {
		t.Fatalf("rows 5-6 = %q, want the body's continuation rows", joined)
	}
	if !strings.HasPrefix(trim(7), "└") {
		t.Fatalf("row 7 = %q, want the bottom border after the continuations", trim(7))
	}
	// Every continuation row is a full frame row: same display width as the table.
	if got, want := ansi.StringWidth(trim(2)), 20; got != want {
		t.Fatalf("continuation row width = %d, want %d (%q)", got, want, trim(2))
	}
	if !strings.HasSuffix(trim(2), "│") {
		t.Fatalf("continuation row = %q, want the closing rail", trim(2))
	}
}

func TestWrappedTableCaretLineHidesContinuations(t *testing.T) {
	t.Parallel()
	doc := wrapTableDoc("| alpha beta gamma | q |", "| --- | --- |", "| delta epsilon zeta | r |")
	// The caret on the header line reveals it raw and its continuation rows stand down.
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	p := composePane(doc, 20)
	p.PageExtraRows = PageViewTableBorders(doc, Styles{})
	rows := p.Render(24, 12).Plain()
	trim := func(y int) string { return strings.TrimRight(rows[y][1:], " ") }
	if !strings.HasPrefix(trim(0), "┌") {
		t.Fatalf("row 0 = %q, want the top border", trim(0))
	}
	if !strings.Contains(trim(1), "alpha beta gamma") {
		t.Fatalf("row 1 = %q, want the raw header under the caret", trim(1))
	}
	if !strings.Contains(trim(2), "q") {
		t.Fatalf("row 2 = %q, want the rest of the revealed header", trim(2))
	}
	if !strings.HasPrefix(trim(3), "├") {
		t.Fatalf("row 3 = %q, want the mid border after the raw header rows", trim(3))
	}
}

func TestWrappedTablePrimaryRowHitIsExact(t *testing.T) {
	t.Parallel()
	doc := wrapTableDoc("| alpha beta gamma | q |", "| --- | --- |", "| delta epsilon zeta | r |")
	p := composePane(doc, 20)
	p.PageExtraRows = PageViewTableBorders(doc, Styles{})
	// Primary header row: margin 1, "│"(col 0) " "(1) then "alpha".
	hit := p.Hit(24, 12, 1+2, 1)
	if hit.Kind != HitText || hit.Pos.Line != 0 || hit.Pos.Col != 2 {
		t.Fatalf("hit = %+v, want source line 0 column 2", hit)
	}
	// A press on a continuation row maps to the end of its source line (the hook's
	// row-level mapping), which reveals the raw row.
	hit = p.Hit(24, 12, 1+2, 2)
	if hit.Kind != HitText || hit.Pos.Line != 0 {
		t.Fatalf("continuation hit = %+v, want line 0", hit)
	}
}

func TestEscapedPipeInACell(t *testing.T) {
	t.Parallel()
	doc := wrapTableDoc("| a \\| b | c |", "| --- | --- |", "| d | e |")
	p := composePane(doc, 30)
	rows := p.Render(40, 10).Plain()
	row := ""
	for _, r := range rows {
		if strings.Contains(r, "a | b") {
			row = r
		}
	}
	if row == "" {
		t.Fatalf("rows = %q, want an unescaped pipe in the composed cell", rows)
	}
	// The caret on the row shows the source text, escape included.
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	rows = p.Render(40, 10).Plain()
	if !strings.Contains(strings.Join(rows, "\n"), "a \\| b") {
		t.Fatalf("rows = %q, want the raw escape under the caret", rows)
	}
}

func TestTableWidthsKeyedByMeasure(t *testing.T) {
	t.Parallel()
	inner := wrapTableDoc("| alpha beta gamma | q |", "| --- | --- |", "| delta epsilon zeta | r |")
	doc := &memoTableDoc{fakeDoc: inner, memo: map[any]any{}}
	narrow := frame{doc: doc, pageOn: true, pageMarkdown: true, pageMeasure: 20}.tables()
	wide := frame{doc: doc, pageOn: true, pageMarkdown: true, pageMeasure: 40}.tables()
	if !narrow.width[0].wrapped {
		t.Fatalf("narrow widths = %+v, want the wrapped columns", narrow.width[0])
	}
	if wide.width[0].wrapped {
		t.Fatalf("wide widths = %+v, want the natural columns", wide.width[0])
	}
}
