package palette

import (
	"slices"

	"github.com/charmbracelet/x/ansi"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// maxRows is the most item rows the sheet shows (n = clamp(H-4, 0, 10)).
const maxRows = 10

// Column widths of the rows, in cells. They come out of the width of the card, so a
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

// visibleRows is n, the number of item rows a frame H rows high gives the sheet. The
// sheet is a card docked to the last row: the title border, the item rows, the query
// row and the bottom border that carries the hints. One row of the frame stays above
// it when the frame has room.
func visibleRows(h int) int {
	return min(max(h-4, 0), maxRows)
}

// layoutMode is what the palette shows beside its item rows: the Live Grep toolbar row
// and the file preview column. The zero value is the plain sheet of the other modes.
type layoutMode struct {
	toolbar bool // one row above the item rows
	preview bool // a column to the right of them
}

// geometry is the placement of the palette in a frame of w by h cells. Rows are
// numbered from the top, and the *End and *W fields are exclusive or sizes.
type geometry struct {
	w, h   int
	rows   int // item rows in the card
	top    int // the top border row; the card is top through bottom
	bottom int // the bottom border row, the frame's last row
	inputY int // the query row, between the last item row and the bottom border

	// toolbarY is the Live Grep toolbar row, between the top border and the first item
	// row; -1 when the mode has no toolbar. rowsTop is the first item row.
	toolbarY int
	rowsTop  int

	left, right int // the columns of the card's left and right border

	nameX, nameEnd int // the name column; nameEnd is exclusive
	keysX, keysEnd int // the shortcut column; keysX is -1 when hidden
	descX, descW   int // the description column; descW is 0 when hidden
	srcX           int // the source column's leftmost cell
	srcOK          bool
	textEnd        int // the end, exclusive, of the text area: one padding cell before the border
	listEnd        int // the end, exclusive, of the list columns: textEnd, or the gap before the preview
	// preview is true when the file preview column is painted: divX is its divider
	// column, and prevX through prevEnd is its text.
	preview     bool
	divX, prevX int
	prevEnd     int
}

// layoutFor places the palette in a w by h frame. It is the plain mode: no toolbar and
// no preview column.
func layoutFor(w, h int) geometry { return layoutForMode(w, h, layoutMode{}) }

// layoutForMode places the palette in a w by h frame. It is a pure function of the size
// and the mode, so painting and hit testing read the same numbers. The card docks to the
// last row of the frame and keeps a margin from the left and right edges (space.Margin),
// and its text keeps a padding cell from the borders.
func layoutForMode(w, h int, mode layoutMode) geometry {
	n := visibleRows(h)
	if mode.toolbar {
		n = min(n, max(h-5, 0)) // the toolbar row comes out of the frame's spare rows
	}
	m := space.Margin(w)
	left, right := m, w-1-m
	g := geometry{
		w: w, h: h, rows: n,
		bottom: h - 1, inputY: h - 2,
		left: left, right: right,
		keysX: -1, toolbarY: -1,
	}
	g.top = g.bottom - 2 - n
	if mode.toolbar {
		g.top--
		g.toolbarY = g.top + 1
	}
	g.rowsTop = g.top + 1
	if mode.toolbar {
		g.rowsTop++
	}
	// A frame too narrow for a card with an inside gives the whole width to the name.
	g.nameX = min(left+1+space.PadX, max(right-1, 0))
	g.textEnd = max(right-space.PadX, g.nameX)
	g.listEnd = g.textEnd
	// The preview column takes the right of the card when there is room for it and a
	// list beside it. A narrow frame keeps the full-width list.
	if inner := g.textEnd - g.nameX; mode.preview && inner >= previewMinText {
		previewW := min(max(inner*previewShare/5, previewMinWidth), previewMaxWidth)
		if div := g.textEnd - previewW; div-g.nameX >= previewMinList {
			g.preview = true
			g.divX = div
			g.prevX = div + 2
			g.prevEnd = g.textEnd + 1
			g.listEnd = div - columnGap
		}
	}
	g.nameEnd, g.keysEnd, g.srcX = g.listEnd, g.listEnd, g.listEnd
	tw := g.listEnd - g.nameX
	edge := g.listEnd
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
// instead of sitting at the two ends of the card. Nothing changes without a
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

// title is the mode the query is in, on the top border of the sheet. It is what the
// card is for, so the prefix characters of the query do not have to say it.
func (p *Palette) title() string {
	if p.live {
		return p.liveTitle()
	}
	mode, _ := commands.ParseQuery(p.input.Value())
	switch mode {
	case commands.ModeCommand:
		return "Commands"
	case commands.ModeLine:
		return "Go to Line"
	case commands.ModeBuffer:
		return "Buffers"
	}
	return "Files"
}

// layoutMode is what the palette shows beside its item rows in the current mode: the
// Live Grep toolbar and the file mode's preview column.
func (p *Palette) layoutMode() layoutMode {
	return layoutMode{toolbar: p.live, preview: p.previewWanted()}
}

// layout places the palette in a w by h frame in the current mode. Painting and hit
// testing read it, so the card and its columns cannot drift apart.
func (p *Palette) layout(w, h int) geometry {
	return layoutForMode(w, h, p.layoutMode())
}

// paint draws the palette over g. The sheet is one card docked to the last row: the
// title border, the Live Grep toolbar in live mode, the item rows (with the file preview
// column beside them), the query row and the hints on the bottom border. A frame with no
// room for the card paints nothing.
func (p *Palette) paint(g *grid.Grid) {
	geo := p.layout(g.Width(), g.Height()).fitColumns(p.rows)
	if geo.top < 0 || geo.right <= geo.left {
		return
	}
	boxW, boxH := geo.right-geo.left+1, geo.bottom-geo.top+1
	p.chrome.ov.PaintBox(g, geo.left, geo.top, boxW, boxH, p.title())
	p.paintToolbar(g, geo)
	p.paintRows(g, geo)
	p.paintPreview(g, geo)
	p.chrome.ov.PaintFooter(g, geo.left, geo.bottom, boxW, p.hint())
	g.Fill(geo.left+1, geo.right, geo.inputY, p.chrome.input)
	p.drawInput(g, geo.inputY, geo.nameX, geo.textEnd)
}

// hint is the text on the bottom border: the mode prefixes of the file, command, line
// and buffer modes, or the Live Grep keys.
func (p *Palette) hint() string {
	if p.live {
		out := liveHint
		if note := p.liveCompletenessHint(); note != "" {
			out += " · " + note
		}
		return out
	}
	return hint
}

// paintToolbar draws the Live Grep toggle row inside the card: each toggle with its key
// and its state, from the row under the title border. A mode with no toolbar paints
// nothing.
func (p *Palette) paintToolbar(g *grid.Grid, geo geometry) {
	if geo.toolbarY < 0 {
		return
	}
	x := geo.nameX
	for _, seg := range p.toolbarSegments() {
		if x >= geo.textEnd {
			break
		}
		st := p.chrome.panel
		if !seg.on {
			st = faint(st)
		}
		x += g.Put(x, geo.toolbarY, geo.textEnd, seg.text, st)
	}
}

// paintRows draws the item rows in view inside the card. An empty list shows the
// message of its mode on the first row.
func (p *Palette) paintRows(g *grid.Grid, geo geometry) {
	if len(p.rows) == 0 {
		if geo.rows > 0 {
			g.Put(geo.nameX, geo.rowsTop, geo.listEnd, p.emptyText(), p.chrome.muted)
		}
		return
	}
	first, count := p.nav.Window()
	for k := range geo.rows {
		if k >= count {
			break
		}
		idx := first + k
		y := geo.rowsTop + k
		selected := idx == p.nav.Selected()
		if selected {
			// The highlight stops at the preview divider: the preview column is not a row.
			end := geo.right
			if geo.preview {
				end = geo.divX
			}
			g.Fill(geo.left+1, end, y, p.chrome.selected)
		}
		p.paintRow(g, geo, y, p.rows[idx], selected)
	}
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
		// Right-aligned so the last character is at the end of the list columns, never
		// left of srcX.
		x := max(geo.listEnd-ansi.StringWidth(r.source), geo.srcX)
		g.Put(x, y, geo.listEnd, truncate(r.source, geo.listEnd-x), faint(base))
	}
}

// nameEnd is where the name of r is cut. A command row has a shortcut and a
// description column, so its name ends before the shortcut column. The other rows
// have neither, so their name runs up to the source label, or to the end of the list
// columns when the frame is too narrow for the label.
func nameEnd(geo geometry, r row) int {
	if r.kind == rowCommand || r.keys != "" || r.desc != "" {
		return geo.nameEnd
	}
	end := geo.listEnd
	if geo.srcOK && r.source != "" {
		end = max(geo.listEnd-ansi.StringWidth(r.source), geo.srcX) - columnGap
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

// drawInput writes the query on row y in [x0, w) and reverses the cell under the
// caret. The row scrolls so that the caret stays in view; the text never reaches
// the card's border at w.
func (p *Palette) drawInput(g *grid.Grid, y, x0, w int) {
	w = min(w, g.Width())
	x0 = min(x0, max(w-1, 0))
	runes := []rune(p.input.Value())
	pos := min(max(p.input.Position(), 0), len(runes))
	start := 0
	for start < pos && ansi.StringWidth(string(runes[start:pos])) > w-x0-1 {
		start++
	}
	base := p.chrome.input
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

// faint returns st with the faint attribute, for the secondary columns and the
// disabled commands.
func faint(st grid.Style) grid.Style {
	st.Faint = true
	return st
}
