package terminal

import (
	"image/color"
	"testing"
)

// styledScreen builds a two-row screen with colours/attributes and a control cell.
func styledScreen() Screen {
	cells := make([]Cell, 2*5)
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	for i, r := range "ab" {
		cells[i] = Cell{Rune: r, Style: Style{Fg: red, Bg: blue, Bold: true}}
	}
	cells[2] = Cell{Rune: '\n'} // a control character must never survive
	cells[5] = Cell{Rune: 'z', Style: Style{Underline: true, Reverse: true}}
	return NewScreen(2, 5, cells)
}

func TestSnapshotScreenRoundTrip(t *testing.T) {
	t.Parallel()
	snap := SnapshotScreen(styledScreen())
	if snap == nil || snap.Cols != 5 || len(snap.Rows) != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(snap.Rows[0].Cells) != 3 {
		t.Fatalf("row 0 cells = %+v, want the trailing blanks trimmed", snap.Rows[0].Cells)
	}
	if got := snap.Rows[0].Cells[0]; got.Rune != 'a' || got.Fg != "#ff0000" || got.Bg != "#0000ff" || !got.Bold {
		t.Fatalf("styled cell = %+v", got)
	}
	if got := snap.Rows[0].Cells[2].Rune; got != ' ' {
		t.Fatalf("control character saved as %q, want a space", got)
	}
	lines := snap.Lines()
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if got := lines[0].Cells[0].Style; got.Fg == nil || got.Bg == nil || !got.Bold {
		t.Fatalf("restored style = %+v", got)
	}
	r, g, b, _ := lines[0].Cells[0].Style.Fg.RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Fatalf("restored fg = %v,%v,%v", r, g, b)
	}
	if !lines[1].Cells[0].Style.Underline || !lines[1].Cells[0].Style.Reverse {
		t.Fatalf("restored attributes = %+v", lines[1].Cells[0].Style)
	}
}

func TestSnapshotScreenBounds(t *testing.T) {
	t.Parallel()
	const rows, cols = MaxSavedScreenRows + 50, MaxSavedScreenCols + 50
	cells := make([]Cell, rows*cols)
	cells[(rows-1)*cols] = Cell{Rune: '#'}
	snap := SnapshotScreen(NewScreen(rows, cols, cells))
	if snap == nil || snap.Cols != MaxSavedScreenCols || len(snap.Rows) != MaxSavedScreenRows {
		t.Fatalf("bounded snapshot = %+v", snap)
	}
	if got := snap.Rows[len(snap.Rows)-1].Cells; len(got) != 1 || got[0].Rune != '#' {
		t.Fatalf("bottom row = %+v, want the marked tail row", got)
	}
	if SnapshotScreen(Screen{}) != nil {
		t.Fatal("an empty screen snapshotted")
	}
}

func TestSavedScreenLinesRejectInvalid(t *testing.T) {
	t.Parallel()
	valid := &SavedScreen{Cols: 3, Rows: []SavedRow{{Cells: []SavedCell{{Rune: 'a'}}}}}
	if got := valid.Lines(); len(got) != 1 {
		t.Fatalf("valid lines = %+v", got)
	}
	tests := []struct {
		name string
		snap *SavedScreen
	}{
		{name: "nil"},
		{name: "no cols", snap: &SavedScreen{Rows: []SavedRow{{}}}},
		{name: "cols over", snap: &SavedScreen{Cols: MaxSavedScreenCols + 1, Rows: []SavedRow{{}}}},
		{name: "no rows", snap: &SavedScreen{Cols: 2}},
		{name: "rows over", snap: &SavedScreen{Cols: 2, Rows: make([]SavedRow, MaxSavedScreenRows+1)}},
		{name: "row too wide", snap: &SavedScreen{Cols: 1, Rows: []SavedRow{{Cells: []SavedCell{{Rune: 'a'}, {Rune: 'b'}}}}}},
	}
	for _, tt := range tests {
		if got := tt.snap.Lines(); got != nil {
			t.Errorf("%s: Lines = %+v, want nil", tt.name, got)
		}
	}
}

func TestSavedScreenBoundedSanitizes(t *testing.T) {
	t.Parallel()
	snap := &SavedScreen{
		Cols: 4,
		Rows: []SavedRow{{Cells: []SavedCell{
			{Rune: 'a', Fg: "red"},
			{Rune: '\n', Bg: "#00ff00"},
		}}},
	}
	got := snap.Bounded()
	if got == nil || got.Cols != 4 || len(got.Rows) != 1 {
		t.Fatalf("bounded = %+v", got)
	}
	cells := got.Rows[0].Cells
	if len(cells) != 2 || cells[0].Fg != "" || cells[1].Rune != ' ' || cells[1].Bg != "#00ff00" {
		t.Fatalf("sanitized cells = %+v", cells)
	}
	over := &SavedScreen{Cols: MaxSavedScreenCols + 50, Rows: make([]SavedRow, MaxSavedScreenRows+10)}
	if b := over.Bounded(); b == nil || b.Cols != MaxSavedScreenCols || len(b.Rows) != MaxSavedScreenRows {
		t.Fatalf("over-bound bounded = %+v", b)
	}
	if (&SavedScreen{}).Bounded() != nil {
		t.Fatal("an empty snapshot survived Bounded")
	}
}

func TestSnapshotLinesKeepsStyles(t *testing.T) {
	t.Parallel()
	snap := SnapshotLines([]Line{
		{Cells: []Cell{{Rune: 'a'}}},
		{Cells: []Cell{{Rune: 'b', Style: Style{Fg: color.RGBA{R: 255, A: 255}}}}},
	})
	if snap == nil || snap.Cols != 1 || len(snap.Rows) != 2 {
		t.Fatalf("snapshot lines = %+v", snap)
	}
	if got := snap.Rows[1].Cells[0]; got.Fg != "#ff0000" || got.Rune != 'b' {
		t.Fatalf("styled cell = %+v", got)
	}
	if SnapshotLines(nil) != nil {
		t.Fatal("nil lines snapshotted")
	}
}
