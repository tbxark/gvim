package syntax

import "testing"

func TestTableEscapedPipes(t *testing.T) {
	t.Parallel()
	lines := []string{"| a \\| b | c |", "| --- | --- |", "| d | e |"}
	got, tables := MarkdownTables(lines)
	if len(tables) != 1 {
		t.Fatalf("tables = %+v", tables)
	}
	if len(got[0].Cells) != 2 || got[0].Cells[0] != "a \\| b" {
		t.Fatalf("cells = %q, want the escaped pipe inside the first cell", got[0].Cells)
	}
	if len(got[0].Pipes) != 3 {
		t.Fatalf("pipes = %v, want 3 (the escaped pipe is not a separator)", got[0].Pipes)
	}
}

func TestTablesInContainersAreNotClassified(t *testing.T) {
	t.Parallel()
	lines := []string{
		"> | a | b |",
		"> | --- | --- |",
		"- | c | d |",
		"| --- | --- |",
	}
	got, tables := MarkdownTables(lines)
	if len(tables) != 0 {
		t.Fatalf("tables = %+v, want no top-level table in a quote/list", tables)
	}
	for i, tl := range got {
		if tl.Kind != TableNone {
			t.Fatalf("line %d classified as %v", i, tl.Kind)
		}
	}
}
