package uitest

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Default colours of a cell that the program did not colour: light grey text on
// a near-black background, the dark theme's defaults.
var (
	defaultFg = color.RGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	defaultBg = color.RGBA{R: 0x10, G: 0x10, B: 0x12, A: 0xff}
)

// Cell is one character cell of a screen.
type Cell struct {
	Char      string // the cell's text; " " for a blank
	Underline bool   // the cell is drawn underlined
	Bold      bool   // the cell is drawn bold
	// Fg and Bg are the colours the cell is drawn with, after reverse video is
	// applied. When DefaultFg or DefaultBg is set, the program did not choose
	// the colour and Fg or Bg is the default for the dark theme.
	Fg, Bg               color.RGBA
	DefaultFg, DefaultBg bool
}

// blankCell is a cell with no text, drawn in the default colours.
func blankCell() Cell {
	return Cell{Char: " ", Fg: defaultFg, Bg: defaultBg, DefaultFg: true, DefaultBg: true}
}

// Screen is a snapshot of the emulated terminal, row by row.
type Screen struct {
	w, h  int
	cells [][]Cell
}

// Width is the number of columns of the screen.
func (sc Screen) Width() int { return sc.w }

// Height is the number of rows of the screen.
func (sc Screen) Height() int { return sc.h }

// Cell returns the cell at column x, row y. Out-of-range coordinates return a
// blank cell.
func (sc Screen) Cell(x, y int) Cell {
	if y < 0 || y >= len(sc.cells) || x < 0 || x >= len(sc.cells[y]) {
		return blankCell()
	}
	return sc.cells[y][x]
}

// Row returns the text of row y. Wide characters occupy one entry here, so a
// column index from Find can differ from a display column after one.
func (sc Screen) Row(y int) string {
	if y < 0 || y >= len(sc.cells) {
		return ""
	}
	var b strings.Builder
	for _, c := range sc.cells[y] {
		b.WriteString(c.Char)
	}
	return b.String()
}

// Contains reports whether text appears on any row.
func (sc Screen) Contains(text string) bool {
	_, _, ok := sc.Find(text)
	return ok
}

// Find returns the column and row of the first occurrence of text, scanning
// rows top to bottom. The column is a cell index, not a display column.
func (sc Screen) Find(text string) (x, y int, ok bool) {
	for y := range sc.cells {
		row := sc.Row(y)
		before, _, ok := strings.Cut(row, text)
		if !ok {
			continue
		}
		return len([]rune(before)), y, true
	}
	return 0, 0, false
}

// Underlined reports the columns of row y that are drawn underlined, as the
// text of each cell. A test uses it to check accelerator letters.
func (sc Screen) Underlined(y int) []string {
	var out []string
	for x := range sc.Width() {
		if c := sc.Cell(x, y); c.Underline {
			out = append(out, c.Char)
		}
	}
	return out
}

// ParseKey converts a key spec into a key press message. The spec grammar is
// described on Session.Key.
func ParseKey(spec string) (tea.KeyPressMsg, error) {
	parts := strings.Split(spec, "+")
	name := parts[len(parts)-1]
	var mod tea.KeyMod
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "ctrl":
			mod |= tea.ModCtrl
		case "alt":
			mod |= tea.ModAlt
		case "shift":
			mod |= tea.ModShift
		default:
			return tea.KeyPressMsg{}, fmt.Errorf("unknown modifier %q in %q", m, spec)
		}
	}
	if code, ok := namedKeys[strings.ToLower(name)]; ok {
		return tea.KeyPressMsg{Code: code, Mod: mod}, nil
	}
	runes := []rune(name)
	if len(runes) != 1 {
		return tea.KeyPressMsg{}, fmt.Errorf("unknown key %q", name)
	}
	r := runes[0]
	if mod&tea.ModCtrl != 0 || mod&tea.ModAlt != 0 {
		// Control and Alt chords carry the key, not text.
		return tea.KeyPressMsg{Code: r, Mod: mod}, nil
	}
	return tea.KeyPressMsg{Code: r, Mod: mod, Text: string(r)}, nil
}

// namedKeys maps the key names of the keymap notation to bubbletea codes.
var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"escape":    tea.KeyEscape,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"space":     tea.KeySpace,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"f1":        tea.KeyF1,
	"f2":        tea.KeyF2,
	"f3":        tea.KeyF3,
	"f4":        tea.KeyF4,
	"f5":        tea.KeyF5,
	"f6":        tea.KeyF6,
	"f7":        tea.KeyF7,
	"f8":        tea.KeyF8,
	"f9":        tea.KeyF9,
	"f10":       tea.KeyF10,
	"f11":       tea.KeyF11,
	"f12":       tea.KeyF12,
}
