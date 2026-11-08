package palette

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// maxRows is the most item rows the box shows (n = clamp(H-4, 0, 10)).
const maxRows = 10

// Column widths of the rows, in cells. They come out of the width of the box, so a
// wide terminal gives every column more room instead of cutting names at a fixed
// column.
const (
	keysWidth     = 16  // the shortcut column
	keysMinText   = 44  // the text width from which the shortcut column is shown
	descMinText   = 100 // the text width from which the description column is shown
	srcMinText    = 60  // the text width from which the source label is shown
	srcReserveMax = 14  // the cells the source label may take, with its gap
	columnGap     = 2   // blank cells between the columns
	descShare     = 3   // the description column takes descShare tenths of the text
)

// visibleRows is n, the number of item rows in a frame H rows high.
func visibleRows(h int) int {
	return min(max(h-4, 0), maxRows)
}

// geometry is the placement of the palette in a frame of w by h cells. Rows are
// numbered from the top, and the *End and *W fields are exclusive or sizes.
type geometry struct {
	w, h    int
	rows    int // item rows in the box
	top     int // the top border row; the box is top through bottom
	bottom  int // the bottom border row
	footY   int // the hint row
	promptY int // the query line

	left, right int // the columns of the box's left and right border

	nameX, nameEnd int // the name column; nameEnd is exclusive
	keysX, keysEnd int // the shortcut column; keysX is -1 when hidden
	descX, descW   int // the description column; descW is 0 when hidden
	srcX           int // the source column's leftmost cell
	srcOK          bool
	textEnd        int // the end, exclusive, of every column: one padding cell before the border
}

// layoutFor places the palette in a w by h frame. It is a pure function of the
// size, so painting and hit testing read the same numbers. The box keeps a margin
// from the left and right edges (space.Margin), and its text keeps a padding cell
// from the borders.
func layoutFor(w, h int) geometry {
	n := visibleRows(h)
	m := space.Margin(w)
	left, right := m, w-1-m
	g := geometry{
		w: w, h: h, rows: n,
		top: h - 4 - n, bottom: h - 3,
		footY: h - 2, promptY: h - 1,
		left: left, right: right,
		keysX: -1,
	}
	// A frame too narrow for a box with an inside gives the whole width to the name.
	g.nameX = min(left+1+space.PadX, max(right-1, 0))
	g.textEnd = max(right-space.PadX, g.nameX)
	g.nameEnd, g.keysEnd, g.srcX = g.textEnd, g.textEnd, g.textEnd
	tw := g.textEnd - g.nameX
	edge := g.textEnd
	if tw >= srcMinText {
		g.srcOK = true
		g.srcX = edge - min(srcReserveMax, tw/5)
		edge = g.srcX
		g.keysEnd = edge
	}
	if tw >= descMinText {
		g.descW = tw*descShare/10 - columnGap
		g.descX = edge - g.descW
		edge = g.descX - columnGap
		g.keysEnd = edge
	}
	if tw >= keysMinText {
		g.keysX = edge - keysWidth
		g.nameEnd = g.keysX - columnGap
		g.keysEnd = max(g.keysEnd, g.keysX)
	} else {
		g.nameEnd = edge
	}
	return g
}

// fitColumns narrows the name column of a command list to its longest name, and gives
// the cells it frees to the description column. The shortcut column moves left with
// the name, so names, shortcuts and descriptions read as one row on a wide terminal
// instead of sitting at the two ends of the box. Nothing changes without a
// description column, or when a name needs the whole column.
func (g geometry) fitColumns(rows []row) geometry {
	if g.descW == 0 || g.keysX < 0 {
		return g
	}
	longest := 0
	for _, r := range rows {
		if nameEnd(g, r) == g.nameEnd {
			longest = max(longest, ansi.StringWidth(r.name))
		}
	}
	freed := g.nameEnd - g.nameX - longest
	if longest == 0 || freed <= 0 {
		return g
	}
	g.nameEnd -= freed
	g.keysX -= freed
	g.keysEnd -= freed
	g.descX -= freed
	g.descW += freed
	return g
}

// paint draws the palette over g. The box is drawn only when it has a top row.
// The row above the box is blanked when the box reaches the top, as the legacy
// palette did, so that the palette covers the menu row.
func (p *Palette) paint(g *grid.Grid) {
	geo := layoutFor(g.Width(), g.Height()).fitColumns(p.rows)
	if geo.top <= 0 {
		g.Fill(0, geo.w, 0, p.chrome.panel)
	}
	if geo.top >= 0 {
		p.paintBox(g, geo)
	}
	g.Fill(0, geo.w, geo.footY, p.chrome.panel)
	text := hint
	if p.live {
		text = liveHint
	}
	g.Put(geo.left, geo.footY, geo.w, truncate(text, geo.w-geo.left), p.chrome.panel)
	g.Fill(0, geo.w, geo.promptY, p.chrome.prompt)
	p.drawInput(g, geo.promptY, geo.nameX)
}

// paintBox draws the bordered box and the rows in view. Every row has a bar at
// each side.
func (p *Palette) paintBox(g *grid.Grid, geo geometry) {
	panel := p.chrome.panel
	boxW := geo.right - geo.left + 1
	g.Fill(geo.left, geo.right+1, geo.top, panel)
	g.Put(geo.left, geo.top, geo.right+1, "╭"+rule("─", boxW-2)+"╮", panel)

	first, count := p.nav.Window()
	for k := range geo.rows {
		y := geo.top + 1 + k
		g.Fill(geo.left, geo.right+1, y, panel)
		g.Set(geo.left, y, "│", panel)
		if boxW > 1 {
			g.Set(geo.right, y, "│", panel)
		}
		if len(p.rows) == 0 {
			if k == 0 {
				g.Put(geo.nameX, y, geo.textEnd, p.emptyText(), panel)
			}
			continue
		}
		if k >= count {
			continue
		}
		idx := first + k
		selected := idx == p.nav.Selected()
		if selected {
			g.Fill(geo.left+1, geo.right, y, p.chrome.selected)
		}
		p.paintRow(g, geo, y, p.rows[idx], selected)
	}

	// With no item rows the bottom border has no corner, as in the legacy palette.
	corner := "╯"
	if geo.rows == 0 {
		corner = " "
	}
	g.Put(geo.left, geo.bottom, geo.right+1, "╰"+rule("─", boxW-2)+corner, panel)
}

// paintRow draws the columns of one row: the name, the shortcut, the description
// and the source. Each is cut where its column ends.
func (p *Palette) paintRow(g *grid.Grid, geo geometry, y int, r row, selected bool) {
	base := p.chrome.panel
	if selected {
		base = p.chrome.selected
	}
	name := base
	if r.disabled {
		name = faint(name)
	}
	p.drawName(g, geo, y, r, name)
	if geo.keysX >= 0 && r.keys != "" {
		g.Put(geo.keysX, y, geo.keysEnd, r.keys, base)
	}
	if geo.descW > 0 && r.desc != "" {
		g.Put(geo.descX, y, geo.descX+geo.descW, truncate(r.desc, geo.descW), faint(base))
	}
	if geo.srcOK && r.source != "" {
		// Right-aligned so the last character is at the end of the text area, never
		// left of srcX.
		x := max(geo.textEnd-ansi.StringWidth(r.source), geo.srcX)
		g.Put(x, y, geo.textEnd, truncate(r.source, geo.textEnd-x), faint(base))
	}
}

// nameEnd is where the name of r is cut. A command row has a shortcut and a
// description column, so its name ends before the shortcut column. The other rows
// have neither, so their name runs up to the source label, or to the end of the text
// area when the frame is too narrow for the label.
func nameEnd(geo geometry, r row) int {
	if r.kind == rowCommand || r.keys != "" || r.desc != "" {
		return geo.nameEnd
	}
	end := geo.textEnd
	if geo.srcOK && r.source != "" {
		end = max(geo.textEnd-ansi.StringWidth(r.source), geo.srcX) - columnGap
	}
	return end
}

// drawName writes the name column. Matched runes are bold. A name that does not fit
// is shortened: a file path in the middle, keeping its base name, a grep row around
// the match, and anything else at its end. The matched offsets follow the cut.
func (p *Palette) drawName(g *grid.Grid, geo geometry, y int, r row, st grid.Style) {
	end := nameEnd(geo, r)
	width := end - geo.nameX
	if width <= 0 {
		return
	}
	shown, hl := fitName(r, width)
	if len(hl) == 0 {
		g.Put(geo.nameX, y, end, shown, st)
		return
	}
	x := geo.nameX
	for i, c := range []rune(shown) {
		s := st
		if slices.Contains(hl, i) {
			s.Bold = true
		}
		x += g.Put(x, y, end, string(c), s)
	}
}

// drawInput writes the query on row y from column x0 and reverses the cell under
// the caret. The row scrolls so that the caret stays in view.
func (p *Palette) drawInput(g *grid.Grid, y, x0 int) {
	w := g.Width()
	x0 = min(x0, max(w-1, 0))
	runes := []rune(p.input.Value())
	pos := min(max(p.input.Position(), 0), len(runes))
	start := 0
	for start < pos && ansi.StringWidth(string(runes[start:pos])) > w-x0-1 {
		start++
	}
	base := p.chrome.prompt
	x := x0
	for i := start; i < len(runes); i++ {
		st := base
		if i == pos {
			st.Reverse = true
		}
		x += g.Put(x, y, w, string(runes[i]), st)
	}
	if pos >= len(runes) {
		caret := base
		caret.Reverse = true
		g.Put(x, y, w, " ", caret)
	}
}

// mode is the mode of the current query.
func (p *Palette) mode() commands.Mode {
	mode, _ := commands.ParseQuery(p.input.Value())
	return mode
}

// truncate shortens s to at most w cells, ending with "…" when it is cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return space.End(s, w)
}

// rule repeats ch n times, or nothing when n is not positive.
func rule(ch string, n int) string {
	return strings.Repeat(ch, max(n, 0))
}

// faint returns st with the faint attribute, for the secondary columns and the
// disabled commands.
func faint(st grid.Style) grid.Style {
	st.Faint = true
	return st
}
