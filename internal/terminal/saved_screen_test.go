package terminal

import (
	"encoding/json"
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

// TestSnapshotScreenCursorAltHistory checks the backing-file remainder: cursor
// position/visibility/shape, the alt flag and the styled history window survive
// the snapshot round-trip.
func TestSnapshotScreenCursorAltHistory(t *testing.T) {
	t.Parallel()
	sc := NewScreen(2, 4, []Cell{
		{Rune: 'h', Style: Style{Fg: color.RGBA{R: 255, A: 255}}},
		{Rune: 'v'},
	})
	sc.Cursor = Cursor{Row: 1, Col: 2, Visible: true, Shape: "bar"}
	history := []Line{
		{Cells: []Cell{{Rune: 'a', Style: Style{Bg: color.RGBA{B: 255, A: 255}}}}},
		{Cells: []Cell{{Rune: 'b', Style: Style{Bold: true}}}},
	}
	snap := SnapshotScreenWithHistory(sc, history)
	if snap == nil {
		t.Fatal("no snapshot")
	}
	if snap.Cursor != (SavedCursor{Row: 1, Col: 2, Visible: true, Shape: "bar"}) {
		t.Fatalf("saved cursor = %+v", snap.Cursor)
	}
	if len(snap.History) != 2 || len(snap.Rows) != 2 {
		t.Fatalf("history/rows = %d/%d", len(snap.History), len(snap.Rows))
	}
	hist := snap.HistoryLines()
	if len(hist) != 2 || hist[0].Cells[0].Style.Bg == nil || !hist[1].Cells[0].Style.Bold {
		t.Fatalf("history styles = %+v", hist)
	}
	if !snap.Valid() {
		t.Fatal("a fresh snapshot is invalid")
	}
	// The alt flag is recorded and an alt screen has no history window.
	sc.Alt = true
	alt := SnapshotScreenWithHistory(sc, history)
	if alt == nil || !alt.Alt || len(alt.History) != 0 {
		t.Fatalf("alt snapshot = %+v", alt)
	}
}

// TestSnapshotHistoryBounds checks the history window keeps the newest rows and
// that the serialized snapshot is trimmed to the byte cap.
func TestSnapshotHistoryBounds(t *testing.T) {
	t.Parallel()
	history := make([]Line, MaxSavedHistoryRows+5)
	for i := range history {
		history[i] = Line{Cells: []Cell{{Rune: rune('a' + i%26)}}}
	}
	sc := NewScreen(1, 4, []Cell{{Rune: 'x'}})
	snap := SnapshotScreenWithHistory(sc, history)
	if got := len(snap.History); got != MaxSavedHistoryRows {
		t.Fatalf("history rows = %d, want %d", got, MaxSavedHistoryRows)
	}
	if got := snap.History[0].Cells[0].Rune; got != rune('a'+5%26) {
		t.Fatalf("oldest kept history row = %q, want the newest window", got)
	}
	// A tall screen keeps the bottom rows and shifts the cursor with them.
	tall := NewScreen(MaxSavedScreenRows+50, 4, nil)
	tall.Cursor = Cursor{Row: MaxSavedScreenRows + 40, Col: 3, Visible: true}
	tallSnap := SnapshotScreen(tall)
	if len(tallSnap.Rows) != MaxSavedScreenRows {
		t.Fatalf("tall rows = %d", len(tallSnap.Rows))
	}
	if tallSnap.Cursor.Row != MaxSavedScreenRows-10 {
		t.Fatalf("cropped cursor row = %d", tallSnap.Cursor.Row)
	}
	// Dense coloured history: the byte cap trims oldest rows rather than writing an
	// oversized record.
	dense := make([]Line, MaxSavedHistoryRows)
	for i := range dense {
		cells := make([]Cell, MaxSavedScreenCols)
		for j := range cells {
			cells[j] = Cell{Rune: 'z', Style: Style{Fg: color.RGBA{R: 255, A: 255}, Bg: color.RGBA{B: 255, A: 255}, Bold: true, Underline: true}}
		}
		dense[i] = Line{Cells: cells}
	}
	wide := NewScreen(1, MaxSavedScreenCols, nil)
	bounded := SnapshotScreenWithHistory(wide, dense)
	if bounded == nil {
		t.Fatal("a dense snapshot was dropped entirely")
	}
	if len(bounded.History) == 0 || len(bounded.History) >= MaxSavedHistoryRows {
		t.Fatalf("byte-trimmed history rows = %d", len(bounded.History))
	}
	if n, err := json.Marshal(bounded); err != nil || len(n) > MaxSavedScreenBytes {
		t.Fatalf("snapshot bytes = %d, %v (cap %d)", len(n), err, MaxSavedScreenBytes)
	}
}

// TestSavedScreenValidAndBoundedCursor checks the strict structure guard and the
// defensive cursor clamp.
func TestSavedScreenValidAndBoundedCursor(t *testing.T) {
	t.Parallel()
	good := &SavedScreen{Cols: 4, Rows: []SavedRow{{Cells: []SavedCell{{Rune: 'a'}}}}, Cursor: SavedCursor{Row: 0, Col: 1, Visible: true, Shape: "underline"}}
	if !good.Valid() {
		t.Fatal("a good snapshot is invalid")
	}
	bad := []*SavedScreen{
		{Cols: 0, Rows: good.Rows},
		{Cols: 4, Rows: nil},
		{Cols: 4, Rows: good.Rows, Cursor: SavedCursor{Row: 9}},
		{Cols: 4, Rows: good.Rows, Cursor: SavedCursor{Col: 9}},
		{Cols: 4, Rows: good.Rows, Cursor: SavedCursor{Shape: "star"}},
		{Cols: 4, Rows: []SavedRow{{Cells: make([]SavedCell, 5)}}},
		{Cols: 4, Rows: good.Rows, History: make([]SavedRow, MaxSavedHistoryRows+1)},
	}
	for i, snap := range bad {
		if snap.Valid() {
			t.Errorf("bad snapshot %d reported valid", i)
		}
	}
	clamped := (&SavedScreen{
		Cols:   4,
		Rows:   []SavedRow{{Cells: []SavedCell{{Rune: 'a'}}}},
		Cursor: SavedCursor{Row: 99, Col: -3, Visible: true, Shape: "star"},
	}).Bounded()
	if clamped == nil || clamped.Cursor.Row != 0 || clamped.Cursor.Col != 0 || clamped.Cursor.Shape != "" {
		t.Fatalf("bounded cursor = %+v", clamped)
	}
}

// TestSavedScreenOldRecordMigration checks a record from before the cursor/alt/
// history fields loads with the old meaning and stays valid.
func TestSavedScreenOldRecordMigration(t *testing.T) {
	t.Parallel()
	var snap SavedScreen
	if err := json.Unmarshal([]byte(`{"cols":4,"rows":[{"cells":[{"r":65,"fg":"#ff0000"}]}]}`), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Cursor.Visible || snap.Cursor.Row != 0 || snap.Cursor.Col != 0 || snap.Alt || len(snap.History) != 0 {
		t.Fatalf("old record gained state: %+v", snap)
	}
	if !snap.Valid() {
		t.Fatal("an old record is invalid")
	}
}

// TestSnapshotPriorScreenRebuilds checks the no-live-screen capture: a restored
// transcript's history/visible split and its cursor/alt metadata are rebuilt so a
// re-save keeps the full snapshot.
func TestSnapshotPriorScreenRebuilds(t *testing.T) {
	t.Parallel()
	prior := []Line{
		{Cells: []Cell{{Rune: 'h', Style: Style{Bold: true}}}},
		{Cells: []Cell{{Rune: 'i', Style: Style{Underline: true}}}},
		{Cells: []Cell{{Rune: 'v', Style: Style{Reverse: true}}}},
	}
	cursor := SavedCursor{Row: 0, Col: 0, Visible: true, Shape: "block"}
	snap := SnapshotPriorScreen(prior, 1, cursor, false)
	if snap == nil || len(snap.Rows) != 1 || len(snap.History) != 2 {
		t.Fatalf("prior snapshot = %+v", snap)
	}
	if snap.Cursor != cursor || !snap.History[0].Cells[0].Bold || !snap.History[1].Cells[0].Underline {
		t.Fatalf("prior styles/cursor = %+v / %+v", snap.History, snap.Cursor)
	}
	if !snap.Valid() {
		t.Fatal("rebuilt snapshot is invalid")
	}
	if SnapshotPriorScreen(prior, 0, cursor, false) != nil || SnapshotPriorScreen(prior, 9, cursor, false) != nil {
		t.Fatal("an impossible visible-row count produced a snapshot")
	}
}

// TestAltScreenSnapshotHasNoHistory checks the alternate screen's bounded snapshot:
// its content/cursor/alt flag round-trip, a history window on an alt frame is corrupt
// (Valid false, and Bounded drops the history while keeping the frame).
func TestAltScreenSnapshotHasNoHistory(t *testing.T) {
	t.Parallel()
	sc := NewScreen(2, 6, []Cell{
		{Rune: 'A', Style: Style{Bold: true}},
		{Rune: 'L'}, {Rune: 'T'},
		{Rune: 'x'},
	})
	sc.Alt = true
	sc.Cursor = Cursor{Row: 1, Col: 1, Visible: true, Shape: "block"}
	history := []Line{{Cells: []Cell{{Rune: 'h'}}}}
	snap := SnapshotScreenWithHistory(sc, history)
	if snap == nil || !snap.Alt || len(snap.History) != 0 {
		t.Fatalf("alt snapshot = %+v", snap)
	}
	if len(snap.Rows) != 2 || snap.Rows[0].Cells[0].Rune != 'A' || !snap.Rows[0].Cells[0].Bold {
		t.Fatalf("alt content = %+v", snap.Rows)
	}
	if snap.Cursor != (SavedCursor{Row: 1, Col: 1, Visible: true, Shape: "block"}) {
		t.Fatalf("alt cursor = %+v", snap.Cursor)
	}
	corrupt := *snap
	corrupt.History = []SavedRow{{Cells: []SavedCell{{Rune: 'h'}}}}
	if corrupt.Valid() {
		t.Fatal("an alt frame with a history window reported valid")
	}
	if got := corrupt.Bounded(); got == nil || !got.Alt || len(got.History) != 0 || len(got.Rows) == 0 {
		t.Fatalf("bounded alt frame = %+v", got)
	}
}
