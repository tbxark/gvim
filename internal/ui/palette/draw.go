package palette

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	model "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// maxRows is the most item rows the box shows (n = clamp(H-4, 0, 10)).
const maxRows = 10

// Column rules of the legacy palette (BLUEPRINT-v2 section 4.4, and the layout
// of the legacy screens). They are kept as numbers, so the legacy columns can be
// checked against them.
const (
	descColumn   = 49 // the description starts here when it is shown
	descMinStart = 67 // the description width is W - 67 and shown when at least 1
	keysMinWidth = 33 // the shortcut column is shown from this frame width
	sourceEnd    = 43 // the source ends no further left than this column
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

	nameX, nameEnd int // the name column; nameEnd is exclusive
	keysX, keysEnd int // the shortcut column; keysX is -1 when hidden
	descX, descW   int // the description column; descW is 0 when hidden
	srcX           int // the source column's leftmost cell
	srcOK          bool
}

// layoutFor places the palette in a w by h frame. It is a pure function of the
// size, so painting and hit testing read the same numbers.
func layoutFor(w, h int) geometry {
	n := visibleRows(h)
	g := geometry{
		w: w, h: h, rows: n,
		top: h - 4 - n, bottom: h - 3,
		footY: h - 2, promptY: h - 1,
		nameX:   min(max(w-58, 1), 3),
		nameEnd: w - 1,
		keysX:   -1,
		keysEnd: w - 1,
		srcX:    sourceEnd,
	}
	if w >= keysMinWidth {
		g.keysX = min(max(w-28, 31), 35)
		g.nameEnd = g.keysX
	}
	if d := w - descMinStart; d >= 1 {
		g.descX, g.descW = descColumn, d
		g.keysEnd = descColumn
	}
	g.srcOK = w-1 > sourceEnd
	return g
}

// paint draws the palette over g. The box is drawn only when it has a top row.
// The row above the box is blanked when the box reaches the top, as the legacy
// palette did, so that the palette covers the menu row.
func (p *Palette) paint(g *grid.Grid) {
	geo := layoutFor(g.Width(), g.Height())
	if geo.top <= 0 {
		g.Fill(0, geo.w, 0, p.chrome.panel)
	}
	if geo.top >= 0 {
		p.paintBox(g, geo)
	}
	g.Fill(0, geo.w, geo.footY, p.chrome.panel)
	g.Put(0, geo.footY, geo.w, truncate(hint, geo.w), p.chrome.panel)
	g.Fill(0, geo.w, geo.promptY, p.chrome.prompt)
	p.drawInput(g, geo.promptY)
}

// paintBox draws the bordered box and the rows in view. The left border is a
// single bar, with no right border, as in the legacy palette.
func (p *Palette) paintBox(g *grid.Grid, geo geometry) {
	panel := p.chrome.panel
	g.Fill(0, geo.w, geo.top, panel)
	g.Put(0, geo.top, geo.w, "┌"+rule("─", geo.w-2)+"┐", panel)

	first, count := p.nav.Window()
	for k := range geo.rows {
		y := geo.top + 1 + k
		g.Fill(0, geo.w, y, panel)
		g.Set(0, y, "│", panel)
		if len(p.rows) == 0 {
			if k == 0 {
				g.Put(geo.nameX, y, geo.w-1, p.emptyText(), panel)
			}
			continue
		}
		if k >= count {
			continue
		}
		idx := first + k
		selected := idx == p.nav.Selected()
		if selected {
			g.Fill(1, geo.w-1, y, p.chrome.selected)
		}
		p.paintRow(g, geo, y, p.rows[idx], selected)
	}

	// With no item rows the bottom border has no corner, as in the legacy palette.
	corner := "┘"
	if geo.rows == 0 {
		corner = " "
	}
	g.Put(0, geo.bottom, geo.w, "└"+rule("─", geo.w-2)+corner, panel)
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
		// Right-aligned so the last character is at column w-2, never left of srcX.
		x := max(geo.w-1-ansi.StringWidth(r.source), geo.srcX)
		g.Put(x, y, geo.w-1, truncate(r.source, geo.w-1-x), faint(base))
	}
}

// drawName writes the name column. Matched runes of a file row are bold. The cut
// keeps the prefix of the name, so a rune at index i of the cut is the rune at
// index i of the name whenever the two are equal.
func (p *Palette) drawName(g *grid.Grid, geo geometry, y int, r row, st grid.Style) {
	width := geo.nameEnd - geo.nameX
	if width <= 0 {
		return
	}
	cut := truncate(r.name, width)
	if len(r.hl) == 0 {
		g.Put(geo.nameX, y, geo.nameEnd, cut, st)
		return
	}
	orig, shown := []rune(r.name), []rune(cut)
	x := geo.nameX
	for i, c := range shown {
		s := st
		if i < len(orig) && orig[i] == c && slices.Contains(r.hl, i) {
			s.Bold = true
		}
		x += g.Put(x, y, geo.nameEnd, string(c), s)
	}
}

// drawInput writes the query on row y and reverses the cell under the caret.
// The row scrolls so that the caret stays in view.
func (p *Palette) drawInput(g *grid.Grid, y int) {
	w := g.Width()
	runes := []rune(p.input.Value())
	pos := min(max(p.input.Position(), 0), len(runes))
	start := 0
	for start < pos && ansi.StringWidth(string(runes[start:pos])) > w-1 {
		start++
	}
	base := p.chrome.prompt
	x := 0
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
func (p *Palette) mode() model.Mode {
	mode, _ := model.ParseQuery(p.input.Value())
	return mode
}

// truncate shortens s to at most w cells, ending with "…" when it is cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w-1, "") + "…"
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
