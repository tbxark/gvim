package term

import (
	"image/color"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// Screen is a snapshot of the emulated screen. Its cells are stored row by row.
type Screen struct {
	// Rows and Cols are the size of the screen in cells.
	Rows, Cols int
	// Cursor is the cursor position when the snapshot was taken.
	Cursor Cursor
	cells  []Cell
}

// Cursor is a cursor position on a screen. Row and Col are zero-based.
type Cursor struct {
	Row, Col int
	// Visible is false while the child has hidden the cursor.
	Visible bool
}

// Cell is one screen cell.
type Cell struct {
	// Rune is the first rune of the cell's text. It is 0 for a cell with no text,
	// such as the right half of a wide character.
	Rune  rune
	Style Style
}

// Style is the basic character style of a cell. A nil colour means the terminal
// default.
type Style struct {
	Fg, Bg                   color.Color
	Bold, Underline, Reverse bool
}

// NewScreen returns a screen of rows by cols cells, filled row by row from cells.
// Cells past the end of cells are blank. A Session makes its own screens; this is
// for fakes and tests that need a screen with known cells.
func NewScreen(rows, cols int, cells []Cell) Screen {
	rows, cols = max(rows, 0), max(cols, 0)
	sc := Screen{Rows: rows, Cols: cols, cells: make([]Cell, rows*cols)}
	copy(sc.cells, cells)
	return sc
}

// Cell returns the cell at row and col. Out-of-range positions give a blank Cell.
func (sc Screen) Cell(row, col int) Cell {
	if row < 0 || row >= sc.Rows || col < 0 || col >= sc.Cols {
		return Cell{}
	}
	return sc.cells[row*sc.Cols+col]
}

// cellOf converts an emulator cell. A nil cell is blank.
func cellOf(c *uv.Cell) Cell {
	if c == nil {
		return Cell{}
	}
	var r rune
	if c.Content != "" {
		r, _ = utf8.DecodeRuneInString(c.Content)
	}
	return Cell{
		Rune: r,
		Style: Style{
			Fg:        c.Style.Fg,
			Bg:        c.Style.Bg,
			Bold:      c.Style.Attrs&uv.AttrBold != 0,
			Underline: c.Style.Underline != uv.UnderlineNone,
			Reverse:   c.Style.Attrs&uv.AttrReverse != 0,
		},
	}
}
