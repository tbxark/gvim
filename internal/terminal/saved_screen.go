package terminal

// Styled screen snapshots for the saved terminal record. Fresh's backing file
// keeps the scrollback and a screen snapshot so a restored terminal reconstructs
// the screen exactly; gvim's bounded slice keeps the same shape in JSON: a styled
// visible screen, a bounded styled history window above it, the cursor position/
// visibility/shape and the alternate-screen flag. The text transcript rows remain
// the migration/fallback representation and the strict tail-match guard; the
// snapshot only paints styles, so a corrupt record is ignored whole. It is still
// bounded (rows, cells and serialized bytes) and does not carry OSC state,
// hyperlinks or byte-exact scrollback (docs/terminal.md).

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Bounds of a saved screen snapshot, so a corrupt or hand-edited record cannot
// make the loader allocate or paint without end.
const (
	MaxSavedScreenRows = 200
	MaxSavedScreenCols = 512
	// MaxSavedHistoryRows bounds the styled history window kept above the visible
	// screen (newest rows win when the terminal's scrollback is deeper).
	MaxSavedHistoryRows = 200
	// MaxSavedScreenBytes bounds the serialized snapshot; the history window is
	// trimmed oldest-first to fit, and a visible screen that cannot fit is dropped
	// rather than written oversized.
	MaxSavedScreenBytes = 512 << 10
)

// SavedScreen is a bounded, styled snapshot of a terminal's screen: the visible
// rows, the styled history window above them, the cursor and the alternate-screen
// flag. Absent fields are the old-record meaning (no styles, no history, no alt).
type SavedScreen struct {
	// Cols is the screen width the rows were captured at.
	Cols int `json:"cols,omitempty"`
	// History is the styled scrollback window above the visible screen, oldest
	// first. It is trimmed to the newest rows when captured.
	History []SavedRow `json:"history,omitempty"`
	// Rows are the visible screen rows, top to bottom; trailing blank cells trimmed.
	Rows []SavedRow `json:"rows,omitempty"`
	// Cursor is the saved cursor in the visible screen's coordinates.
	Cursor SavedCursor `json:"cursor,omitempty"`
	// Alt records that the saved visible screen was the alternate screen (a
	// full-screen program's frame); it has no history.
	Alt bool `json:"alt,omitempty"`
}

// SavedCursor is the saved cursor position, visibility and shape. Row/Col are
// zero-based within Rows; Visible false means the child had hidden the cursor;
// Shape is "block", "underline" or "bar" ("" means default/unknown, painted as a
// block). The restored read-only view paints it; a revived shell owns the live
// cursor from the dive on.
type SavedCursor struct {
	Row     int    `json:"row,omitempty"`
	Col     int    `json:"col,omitempty"`
	Visible bool   `json:"visible,omitempty"`
	Shape   string `json:"shape,omitempty"`
}

// SavedRow is one row of a SavedScreen.
type SavedRow struct {
	Cells []SavedCell `json:"cells,omitempty"`
}

// SavedCell is one styled cell of a SavedScreen. Colours are #rrggbb strings (the
// terminal's colors are RGB after the emulator resolves SGR), empty for the
// terminal default; Rune 0 is blank or a wide-character continuation half.
type SavedCell struct {
	Rune      rune   `json:"r,omitempty"`
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Reverse   bool   `json:"reverse,omitempty"`
}

// SnapshotScreen returns the bounded styled snapshot of sc, or nil for an empty
// screen. A screen taller/wider than the bounds keeps its bottom rows and left
// columns (the visible tail that matters for the read-only/revived view). Control
// characters are replaced by spaces so a restored snapshot can never move the
// loader's cursor; trailing blank cells are trimmed.
func SnapshotScreen(sc Screen) *SavedScreen {
	if sc.Rows == 0 || sc.Cols == 0 {
		return nil
	}
	rows := min(sc.Rows, MaxSavedScreenRows)
	cols := min(sc.Cols, MaxSavedScreenCols)
	out := &SavedScreen{
		Cols:   cols,
		Rows:   make([]SavedRow, rows),
		Cursor: savedCursorOf(sc, rows),
		Alt:    sc.Alt,
	}
	for i := range rows {
		// Keep the bottom rows when the screen is taller than the bound.
		r := sc.Rows - rows + i
		out.Rows[i] = savedRowOf(sc, r, cols)
	}
	return out.trimBytes()
}

// SnapshotScreenWithHistory is SnapshotScreen plus the styled scrollback window
// above it (history is oldest-first; the newest rows are kept when it is longer
// than the bound). The captured history is added after the rows, so the visible
// tail and the cursor keep their meaning.
func SnapshotScreenWithHistory(sc Screen, history []Line) *SavedScreen {
	out := SnapshotScreen(sc)
	if out == nil {
		return nil
	}
	if sc.Alt || len(history) == 0 {
		return out
	}
	keep := min(len(history), MaxSavedHistoryRows)
	history = history[len(history)-keep:]
	out.History = savedRowsOf(history, out.Cols, keep)
	return out.trimBytes()
}

// SnapshotPriorScreen is the no-live-screen capture: it rebuilds the snapshot from
// a restored transcript's styled rows. visibleRows is how many trailing prior rows
// were the visible screen (saved when the snapshot was applied); the rows above it
// become the styled history window. cursor/alt are carried over so re-saving a
// restored-but-not-revived tab keeps the full snapshot instead of dropping the
// cursor/alt/history. It returns nil when the counts cannot describe a snapshot.
func SnapshotPriorScreen(prior []Line, visibleRows int, cursor SavedCursor, alt bool) *SavedScreen {
	if visibleRows <= 0 || visibleRows > len(prior) || visibleRows > MaxSavedScreenRows {
		return nil
	}
	cols := 0
	for i := len(prior) - visibleRows; i < len(prior); i++ {
		cols = max(cols, len(prior[i].Cells))
	}
	cols = min(cols, MaxSavedScreenCols)
	if cols == 0 {
		return nil
	}
	out := &SavedScreen{Cols: cols, Rows: savedRowsOf(prior[len(prior)-visibleRows:], cols, visibleRows), Cursor: cursor, Alt: alt}
	if !alt {
		history := prior[:len(prior)-visibleRows]
		keep := min(len(history), MaxSavedHistoryRows)
		out.History = savedRowsOf(history[len(history)-keep:], cols, keep)
	}
	out.Cursor = boundCursor(out.Cursor, len(out.Rows), cols)
	return out.trimBytes()
}

// SnapshotLines is SnapshotScreen for a stored transcript's styled rows (the last
// rows of it), so a restored-but-not-revived tab keeps its styles across another
// save even though it has no live screen. It uses the widest row as Cols.
func SnapshotLines(lines []Line) *SavedScreen {
	if len(lines) == 0 {
		return nil
	}
	rows := min(len(lines), MaxSavedScreenRows)
	cols := 0
	for _, line := range lines[len(lines)-rows:] {
		cols = max(cols, len(line.Cells))
	}
	cols = min(cols, MaxSavedScreenCols)
	if cols == 0 {
		return nil
	}
	out := &SavedScreen{Cols: cols, Rows: make([]SavedRow, rows)}
	for i := range rows {
		line := lines[len(lines)-rows+i]
		cells := make([]SavedCell, 0, min(len(line.Cells), cols))
		for c := 0; c < min(len(line.Cells), cols); c++ {
			cells = append(cells, savedCellOf(line.Cells[c]))
		}
		for len(cells) > 0 && blankSavedCell(cells[len(cells)-1]) {
			cells = cells[:len(cells)-1]
		}
		out.Rows[i] = SavedRow{Cells: cells}
	}
	return out
}

// Lines converts the saved visible screen back into transcript lines. An
// invalid snapshot (wrong bounds, over-wide rows) returns nil, so the caller
// falls back to the plain text transcript instead of painting something wrong.
func (s *SavedScreen) Lines() []Line {
	if s == nil || s.Cols <= 0 || s.Cols > MaxSavedScreenCols || len(s.Rows) == 0 || len(s.Rows) > MaxSavedScreenRows {
		return nil
	}
	return linesFromSavedRows(s.Rows, s.Cols)
}

// HistoryLines converts the saved history window back into transcript lines
// (oldest first). nil means no history or an invalid snapshot.
func (s *SavedScreen) HistoryLines() []Line {
	if s == nil || s.Cols <= 0 || s.Cols > MaxSavedScreenCols || len(s.Rows) == 0 || len(s.Rows) > MaxSavedScreenRows {
		return nil
	}
	if len(s.History) > MaxSavedHistoryRows {
		return nil
	}
	return linesFromSavedRows(s.History, s.Cols)
}

// Valid reports whether the record's structure is usable: positive bounded
// columns, a bounded non-empty visible screen, a bounded history window, rows no
// wider than the screen and a cursor inside the visible rows with a known shape.
// The dock applies a snapshot only when this holds AND the texts match exactly.
func (s *SavedScreen) Valid() bool {
	if s == nil || s.Cols <= 0 || s.Cols > MaxSavedScreenCols || len(s.Rows) == 0 || len(s.Rows) > MaxSavedScreenRows {
		return false
	}
	if len(s.History) > MaxSavedHistoryRows {
		return false
	}
	for _, row := range s.Rows {
		if len(row.Cells) > s.Cols {
			return false
		}
	}
	for _, row := range s.History {
		if len(row.Cells) > s.Cols {
			return false
		}
	}
	if s.Cursor.Row < 0 || s.Cursor.Row >= len(s.Rows) || s.Cursor.Col < 0 || s.Cursor.Col >= s.Cols {
		return false
	}
	switch s.Cursor.Shape {
	case "", "block", "underline", "bar":
	default:
		return false
	}
	return true
}

// linesFromSavedRows converts rows to lines; nil for an over-wide row.
func linesFromSavedRows(rows []SavedRow, cols int) []Line {
	lines := make([]Line, len(rows))
	for i, row := range rows {
		if len(row.Cells) > cols {
			return nil
		}
		cells := make([]Cell, len(row.Cells))
		for j, sc := range row.Cells {
			cells[j] = Cell{Rune: cleanSavedRune(sc.Rune), Style: savedStyle(sc)}
		}
		lines[i] = Line{Cells: cells}
	}
	return lines
}

// savedStyle converts a saved cell's style.
func savedStyle(sc SavedCell) Style {
	return Style{
		Fg:        savedColor(sc.Fg),
		Bg:        savedColor(sc.Bg),
		Bold:      sc.Bold,
		Underline: sc.Underline,
		Reverse:   sc.Reverse,
	}
}

// Bounded returns a sanitized copy of s: the rows/cols bounds applied, control
// characters replaced, invalid colours dropped, over-wide rows clipped and
// trailing blanks trimmed. A nil/empty result means there is no usable snapshot.
// LoadTerminal calls it so a corrupt file cannot paint garbage.
func (s *SavedScreen) Bounded() *SavedScreen {
	if s == nil {
		return nil
	}
	cols := s.Cols
	if cols <= 0 || cols > MaxSavedScreenCols {
		cols = min(max(cols, 0), MaxSavedScreenCols)
	}
	rows := min(len(s.Rows), MaxSavedScreenRows)
	if cols == 0 || rows == 0 {
		return nil
	}
	out := &SavedScreen{Cols: cols, Alt: s.Alt}
	out.Rows = boundedSavedRows(s.Rows, rows, cols)
	out.History = boundedSavedRows(s.History, MaxSavedHistoryRows, cols)
	out.Cursor = boundCursor(s.Cursor, len(out.Rows), cols)
	return out.trimBytes()
}

// boundedSavedRows sanitizes at most max rows of rows and trims their trailing
// blanks.
func boundedSavedRows(rows []SavedRow, maxRows, cols int) []SavedRow {
	rows = rows[:min(len(rows), maxRows)]
	out := make([]SavedRow, 0, len(rows))
	for _, row := range rows {
		cells := make([]SavedCell, 0, min(len(row.Cells), cols))
		for _, sc := range row.Cells[:min(len(row.Cells), cols)] {
			sc.Rune = cleanSavedRune(sc.Rune)
			if !validSavedColor(sc.Fg) {
				sc.Fg = ""
			}
			if !validSavedColor(sc.Bg) {
				sc.Bg = ""
			}
			cells = append(cells, sc)
		}
		for len(cells) > 0 && blankSavedCell(cells[len(cells)-1]) {
			cells = cells[:len(cells)-1]
		}
		out = append(out, SavedRow{Cells: cells})
	}
	return out
}

// boundCursor clamps a saved cursor into rows/cols and clears an unknown shape;
// a cursor that cannot fit is hidden at the last valid cell.
func boundCursor(c SavedCursor, rows, cols int) SavedCursor {
	if rows <= 0 || cols <= 0 {
		return SavedCursor{}
	}
	c.Row = min(max(c.Row, 0), rows-1)
	c.Col = min(max(c.Col, 0), cols-1)
	switch c.Shape {
	case "", "block", "underline", "bar":
	default:
		c.Shape = ""
	}
	return c
}

// trimBytes trims the history window oldest-first until the serialized snapshot
// fits MaxSavedScreenBytes; a visible screen that alone exceeds the cap drops the
// whole snapshot (the strict tail guard would ignore it anyway).
func (s *SavedScreen) trimBytes() *SavedScreen {
	if s == nil {
		return nil
	}
	size, err := json.Marshal(s)
	if err != nil || len(size) <= MaxSavedScreenBytes {
		if err != nil {
			return nil
		}
		return s
	}
	// Binary search the number of history rows to drop.
	lo, hi := 0, len(s.History)
	for lo < hi {
		mid := (lo + hi) / 2
		cand := *s
		cand.History = s.History[mid:]
		data, cerr := json.Marshal(&cand)
		if cerr == nil && len(data) <= MaxSavedScreenBytes {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	out := *s
	if lo >= len(out.History) {
		out.History = nil
	} else {
		out.History = out.History[lo:]
	}
	data, err := json.Marshal(&out)
	if err != nil || len(data) > MaxSavedScreenBytes {
		return nil
	}
	return &out
}

// savedRowsOf converts styled lines to saved rows (newest keepRows rows), trimming
// trailing blanks. It returns at most keepRows rows; cols is the snapshot width.
func savedRowsOf(lines []Line, cols, keepRows int) []SavedRow {
	keepRows = min(keepRows, len(lines))
	out := make([]SavedRow, 0, keepRows)
	for _, line := range lines[len(lines)-keepRows:] {
		cells := make([]SavedCell, 0, min(len(line.Cells), cols))
		for c := 0; c < min(len(line.Cells), cols); c++ {
			cells = append(cells, savedCellOf(line.Cells[c]))
		}
		for len(cells) > 0 && blankSavedCell(cells[len(cells)-1]) {
			cells = cells[:len(cells)-1]
		}
		out = append(out, SavedRow{Cells: cells})
	}
	return out
}

// savedRowOf converts visible screen row r to a saved row.
func savedRowOf(sc Screen, r, cols int) SavedRow {
	cells := make([]SavedCell, 0, cols)
	for c := 0; c < cols; c++ {
		cells = append(cells, savedCellOf(sc.Cell(r, c)))
	}
	for len(cells) > 0 && blankSavedCell(cells[len(cells)-1]) {
		cells = cells[:len(cells)-1]
	}
	return SavedRow{Cells: cells}
}

// savedCursorOf converts a screen's cursor for a snapshot keeping the bottom rows
// (the row is shifted when a taller screen was cropped at the top).
func savedCursorOf(sc Screen, keptRows int) SavedCursor {
	shape := sc.Cursor.Shape
	switch shape {
	case "", "block", "underline", "bar":
	default:
		shape = ""
	}
	row := sc.Cursor.Row - (sc.Rows - keptRows)
	return boundCursor(SavedCursor{Row: row, Col: sc.Cursor.Col, Visible: sc.Cursor.Visible, Shape: shape}, keptRows, min(sc.Cols, MaxSavedScreenCols))
}

// savedCellOf converts one terminal cell to its saved form.
func savedCellOf(c Cell) SavedCell {
	return SavedCell{
		Rune:      cleanSavedRune(c.Rune),
		Fg:        savedColorHex(c.Style.Fg),
		Bg:        savedColorHex(c.Style.Bg),
		Bold:      c.Style.Bold,
		Underline: c.Style.Underline,
		Reverse:   c.Style.Reverse,
	}
}

// cleanSavedRune replaces a control character (or an invalid rune) with a space;
// 0 (blank/continuation) stays 0.
func cleanSavedRune(r rune) rune {
	if r == 0 {
		return 0
	}
	if !utf8.ValidRune(r) || unicode.IsControl(r) {
		return ' '
	}
	return r
}

// blankSavedCell reports whether c carries no text and no style (trailing padding).
func blankSavedCell(c SavedCell) bool {
	return c.Rune == 0 && c.Fg == "" && c.Bg == "" && !c.Bold && !c.Underline && !c.Reverse
}

// savedColorHex is the #rrggbb form of a colour, or "" for the terminal default.
func savedColorHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// validSavedColor reports whether s is "" or a #rrggbb colour.
func validSavedColor(s string) bool {
	_, ok := parseSavedColor(s)
	return s == "" || ok
}

// savedColor parses a saved #rrggbb colour; an empty/invalid value is the
// terminal default.
func savedColor(s string) color.Color {
	c, ok := parseSavedColor(s)
	if !ok {
		return nil
	}
	return c
}

// parseSavedColor parses "#rrggbb".
func parseSavedColor(s string) (color.RGBA, bool) {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}
