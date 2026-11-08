package terminal

// Styled screen snapshots for the saved terminal record. Fresh's backing file
// keeps the scrollback and a screen snapshot so a restored terminal reconstructs
// the screen exactly; gvim's bounded slice keeps a styled snapshot of the visible
// screen next to the text transcript rows, so a revived tab paints the old screen
// with its colours/attributes above the new shell output. It is deliberately only
// the visible screen tail (bounded rows/columns), not the full backing-file
// semantics: older history rows stay plain text (docs/terminal.md).

import (
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
)

// SavedScreen is a bounded, styled snapshot of a terminal's visible screen.
type SavedScreen struct {
	// Cols is the screen width the rows were captured at.
	Cols int `json:"cols,omitempty"`
	// Rows are the screen rows, top to bottom; trailing blank cells are trimmed.
	Rows []SavedRow `json:"rows,omitempty"`
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
	out := &SavedScreen{Cols: cols, Rows: make([]SavedRow, rows)}
	for i := range rows {
		// Keep the bottom rows when the screen is taller than the bound.
		r := sc.Rows - rows + i
		cells := make([]SavedCell, 0, cols)
		for c := 0; c < cols; c++ {
			cells = append(cells, savedCellOf(sc.Cell(r, c)))
		}
		for len(cells) > 0 && blankSavedCell(cells[len(cells)-1]) {
			cells = cells[:len(cells)-1]
		}
		out.Rows[i] = SavedRow{Cells: cells}
	}
	return out
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

// Lines converts a saved screen back into transcript lines. An invalid snapshot
// (wrong bounds, control-only garbage) returns nil, so the caller falls back to
// the plain text transcript instead of painting something wrong.
func (s *SavedScreen) Lines() []Line {
	if s == nil || s.Cols <= 0 || s.Cols > MaxSavedScreenCols || len(s.Rows) == 0 || len(s.Rows) > MaxSavedScreenRows {
		return nil
	}
	lines := make([]Line, len(s.Rows))
	for i, row := range s.Rows {
		if len(row.Cells) > s.Cols {
			return nil
		}
		cells := make([]Cell, len(row.Cells))
		for j, sc := range row.Cells {
			c := Cell{Rune: cleanSavedRune(sc.Rune)}
			c.Style = Style{
				Fg:        savedColor(sc.Fg),
				Bg:        savedColor(sc.Bg),
				Bold:      sc.Bold,
				Underline: sc.Underline,
				Reverse:   sc.Reverse,
			}
			cells[j] = c
		}
		lines[i] = Line{Cells: cells}
	}
	return lines
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
	out := &SavedScreen{Cols: cols, Rows: make([]SavedRow, 0, rows)}
	for _, row := range s.Rows[:rows] {
		cells := make([]SavedCell, 0, min(len(row.Cells), cols))
		for _, sc := range row.Cells[:min(len(row.Cells), cols)] {
			r := cleanSavedRune(sc.Rune)
			sc.Rune = r
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
		out.Rows = append(out.Rows, SavedRow{Cells: cells})
	}
	return out
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
