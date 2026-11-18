package syntax

import "testing"

func TestMarkdownTablesClassifies(t *testing.T) {
	t.Parallel()
	lines := []string{
		"# T",
		"| Name | Qty |",
		"| :--- | ---: |",
		"| a | 1 |",
		"| b | 22 |",
		"",
		"text | pipe",
		"not a table",
	}
	got, tables := MarkdownTables(lines)
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	tl := tables[0]
	if len(tl.Lines) != 4 || tl.Align[0] != AlignLeft || tl.Align[1] != AlignRight {
		t.Fatalf("table = %+v", tl)
	}
	if got[1].Kind != TableHeader || got[2].Kind != TableSeparator || got[3].Kind != TableBody || got[4].Kind != TableBody {
		t.Fatalf("kinds = %v %v %v %v", got[1].Kind, got[2].Kind, got[3].Kind, got[4].Kind)
	}
	if got[1].Cells[0] != "Name" || got[1].Cells[1] != "Qty" {
		t.Fatalf("header cells = %q", got[1].Cells)
	}
	if got[0].Kind != TableNone || got[5].Kind != TableNone || got[6].Kind != TableNone {
		t.Fatalf("non-table lines classified: %v %v %v", got[0].Kind, got[5].Kind, got[6].Kind)
	}
	// The starts point at the trimmed content (the pipe, a blank, then the cell).
	if got[1].Starts[0] != 2 || got[1].Ends[0] != 6 {
		t.Fatalf("header cell range = %d..%d, want 2..6", got[1].Starts[0], got[1].Ends[0])
	}
}

func TestMarkdownTablesCenterAndNoPipes(t *testing.T) {
	t.Parallel()
	lines := []string{"a | b", "--- | :-:", "1 | 2"}
	got, tables := MarkdownTables(lines)
	if len(tables) != 1 || tables[0].Align[0] != AlignLeft || tables[0].Align[1] != AlignCenter {
		t.Fatalf("tables = %+v", tables)
	}
	if got[0].Kind != TableHeader || got[2].Kind != TableBody {
		t.Fatalf("kinds = %v %v", got[0].Kind, got[2].Kind)
	}
}

func TestMarkdownTablesBlankEmptySpacesAndCRLF(t *testing.T) {
	t.Parallel()
	// Whitespace-only cells must not panic; empty interior cells must be preserved;
	// outer whitespace and CRLF must work identically across header/separator/body.
	lines := []string{
		"  | a |   | c |  \r",
		"  | --- | --- | --- |  \r",
		"  | x || z |  \r",
	}
	got, tables := MarkdownTables(lines)
	if len(tables) != 1 || len(tables[0].Lines) != 3 {
		t.Fatalf("tables = %+v, want 1 table with 3 lines", tables)
	}
	if want := []string{"a", "", "c"}; len(got[0].Cells) != 3 || got[0].Cells[0] != want[0] || got[0].Cells[1] != want[1] || got[0].Cells[2] != want[2] {
		t.Fatalf("header cells = %q, want %q", got[0].Cells, want)
	}
	if want := []string{"x", "", "z"}; len(got[2].Cells) != 3 || got[2].Cells[0] != want[0] || got[2].Cells[1] != want[1] || got[2].Cells[2] != want[2] {
		t.Fatalf("body cells = %q, want %q", got[2].Cells, want)
	}
}
