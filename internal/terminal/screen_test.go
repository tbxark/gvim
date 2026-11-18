package terminal

import "testing"

func TestScreenCell(t *testing.T) {
	sc := Screen{Rows: 2, Cols: 3, cells: make([]Cell, 2*3)}
	sc.cells[0] = Cell{Rune: 'a'}
	sc.cells[1*3+2] = Cell{Rune: 'z', Style: Style{Bold: true}}

	tests := []struct {
		name     string
		row, col int
		want     Cell
	}{
		{name: "top left", row: 0, col: 0, want: Cell{Rune: 'a'}},
		{name: "bottom right", row: 1, col: 2, want: Cell{Rune: 'z', Style: Style{Bold: true}}},
		{name: "blank inside", row: 0, col: 1, want: Cell{}},
		{name: "row above top", row: -1, col: 0, want: Cell{}},
		{name: "row below bottom", row: 2, col: 0, want: Cell{}},
		{name: "column left of first", row: 0, col: -1, want: Cell{}},
		{name: "column past last", row: 0, col: 3, want: Cell{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sc.Cell(tt.row, tt.col); got != tt.want {
				t.Fatalf("Cell(%d, %d) = %+v, want %+v", tt.row, tt.col, got, tt.want)
			}
		})
	}
}

func TestNewScreenHoldsCellsRowByRow(t *testing.T) {
	cells := []Cell{{Rune: 'a'}, {Rune: 'b'}, {Rune: 'c'}}
	tests := []struct {
		name     string
		row, col int
		want     Cell
	}{
		{name: "first cell", row: 0, col: 0, want: Cell{Rune: 'a'}},
		{name: "second row first cell", row: 1, col: 0, want: Cell{Rune: 'c'}},
		{name: "cell past the given cells is blank", row: 1, col: 1, want: Cell{}},
	}
	sc := NewScreen(2, 2, cells)
	if sc.Rows != 2 || sc.Cols != 2 {
		t.Fatalf("NewScreen(2, 2) size = %dx%d, want 2x2", sc.Rows, sc.Cols)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sc.Cell(tt.row, tt.col); got != tt.want {
				t.Fatalf("Cell(%d, %d) = %+v, want %+v", tt.row, tt.col, got, tt.want)
			}
		})
	}
}

func TestSavedScreenRoundTripsExtendedStyles(t *testing.T) {
	sc := NewScreen(1, 3, []Cell{
		{Rune: 'i', Style: Style{Italic: true}},
		{Rune: 'f', Style: Style{Faint: true}},
		{Rune: 's', Style: Style{Strikethrough: true}},
	})
	saved := SnapshotScreen(sc)
	if saved == nil || len(saved.Rows) != 1 || len(saved.Rows[0].Cells) != 3 {
		t.Fatalf("SnapshotScreen = %+v, want 1x3 cells", saved)
	}
	lines := saved.Lines()
	for idx, want := range []Style{{Italic: true}, {Faint: true}, {Strikethrough: true}} {
		if got := lines[0].Cells[idx].Style; got != want {
			t.Fatalf("cell %d restored style = %+v, want %+v", idx, got, want)
		}
	}
}
