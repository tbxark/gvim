package editorpane

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// Overlays are the decorations that something outside the pane (a plugin) puts on the
// text: styled runs, a style for whole rows, virtual text after the end of a line, text
// that replaces one cell, a mark in the gutter and a mark in the last column. The root
// supplies them through the hooks of Pane, one line at a time, so the pane asks only for
// the lines it paints and a large document costs no more than a small one. They never
// become text of the document: the geometry the pane lays the text out with, and so the
// caret and the mouse, does not see them. Every hook is optional.
//
// A style given here is a patch over the look the cell would have: a color that is nil
// leaves the cell's own, and an attribute that is set is added to the cell's.

// StyledSpan is the run of rune columns [Start, End) of a line that is painted with Style.
// End < 0 runs to the end of the line, and past it, through the line break. With Extend
// set, a run that reaches the end of the line also paints the rest of the row with the
// style. Spans are painted in order of Priority, lowest first, so the highest shows.
type StyledSpan struct {
	Start, End int
	Style      grid.Style
	Extend     bool
	Priority   int
}

// VirtualText is text drawn after the last cell of a line. It is not part of the line:
// it takes no column of it, and the pane cuts it at its right edge.
type VirtualText struct {
	Text  string
	Style grid.Style
}

// Replacement is text drawn over the cell of the character at rune column Col of a line.
// It takes exactly the width of that cell: the first character of Text, cut to the
// cell's width (a character that is wider is left out for a blank), then blanks to fill.
// A tab is replaced in its first cell only.
type Replacement struct {
	Col   int
	Text  string
	Style grid.Style
}

// GutterMark replaces the change bar of a line. Text is one cell wide.
type GutterMark struct {
	Text  string
	Style grid.Style
}

// ScrollMark colors the change mark of a line in the last column. Only the colors of
// Style are used.
type ScrollMark struct {
	Style grid.Style
}

// overlayHooks are the hooks of the pane the frame carries.
type overlayHooks struct {
	spans    func(line int) []StyledSpan
	row      func(line int) (grid.Style, bool)
	eol      func(line int) []VirtualText
	replaces func(line int) []Replacement
	gutter   func(line int) (GutterMark, bool)
	scroll   func(line int) (ScrollMark, bool)
}

func (p Pane) overlayHooks() overlayHooks {
	return overlayHooks{p.Overlays, p.RowStyle, p.EOL, p.Replaces, p.Gutter, p.ScrollMarks}
}

func (h overlayHooks) any() bool {
	return h.spans != nil || h.row != nil || h.eol != nil || h.replaces != nil || h.gutter != nil || h.scroll != nil
}

// The most distinct looks the overlays of one frame can give cells, and the most
// replacement texts one line can have. A cell holds an index into the table, so beyond
// these the extra looks and replacements are left out.
const maxOverlayLooks = 1<<16 - 1

// repHidden marks a cell whose rune is covered by a replacement: it is not drawn.
const repHidden = 1

// overlayFX is the per-frame table of the overlay looks and replacement texts that cells
// index. It is shared by the copies of the frame. The looks last for the frame. The
// replacement texts last for the line being laid out, which is painted before the next
// one is laid out, so a screen full of labels does not fill the table.
type overlayFX struct {
	looks []grid.Style // cell.dec-1
	index map[grid.Style]uint16
	reps  []string // cell.rep-2
}

// PatchStyle applies the patch p to base: a color of p that is not nil replaces the color of
// base, and an attribute of p that is set is added to the attributes of base.
func PatchStyle(base, p grid.Style) grid.Style {
	if p.FG != nil {
		base.FG = p.FG
	}
	if p.BG != nil {
		base.BG = p.BG
	}
	base.Bold = base.Bold || p.Bold
	base.Faint = base.Faint || p.Faint
	base.Italic = base.Italic || p.Italic
	base.Underline = base.Underline || p.Underline
	base.Strike = base.Strike || p.Strike
	base.Reverse = base.Reverse || p.Reverse
	return base
}

// look returns the patch cell index dec stands for (the zero patch for 0).
func (x *overlayFX) look(dec uint16) grid.Style {
	if dec == 0 {
		return grid.Style{}
	}
	return x.looks[dec-1]
}

// intern returns the index of the look p, adding it to the table, or 0 when the table is
// full.
func (x *overlayFX) intern(p grid.Style) uint16 {
	if p == (grid.Style{}) {
		return 0
	}
	if i, ok := x.index[p]; ok {
		return i
	}
	if len(x.looks) >= maxOverlayLooks {
		return 0
	}
	x.looks = append(x.looks, p)
	if x.index == nil {
		x.index = make(map[grid.Style]uint16)
	}
	x.index[p] = uint16(len(x.looks))
	return uint16(len(x.looks))
}

// addRep stores a replacement text and returns the cell.rep that names it.
func (x *overlayFX) addRep(text string) uint16 {
	if len(x.reps) >= maxOverlayLooks-2 {
		return 0
	}
	x.reps = append(x.reps, text)
	return uint16(len(x.reps) + 1)
}

// repText is the text a cell with the replacement index rep draws.
func (x *overlayFX) repText(rep uint16) string {
	if rep == repHidden {
		return ""
	}
	return x.reps[rep-2]
}

// markOverlays gives the cells of line i (the text cells of ln, before the cells that
// follow the text are added) the looks of the line's overlays, and records the look of
// the rest of the row when an overlay asks for it. raw is the runes that are laid out,
// which start at rune column f.off of the line.
func (f frame) markOverlays(i int, raw []rune, ln *line) {
	h := f.hooks
	if f.dx == nil || !h.any() {
		return
	}
	cells := ln.cells
	f.dx.reps = f.dx.reps[:0]
	var tail grid.Style
	hasTail := false
	if h.row != nil {
		if st, ok := h.row(i); ok {
			f.applyLook(cells, 0, len(cells), st)
			tail, hasTail = PatchStyle(tail, st), true
		}
	}
	if h.spans != nil {
		spans := h.spans(i)
		if len(spans) > 1 && !slices.IsSortedFunc(spans, byPriority) {
			spans = slices.SortedStableFunc(slices.Values(spans), byPriority)
		}
		// Finding where each span starts and ends costs a walk over the line; a line with many
		// spans gets one table instead.
		index := func(col int) int { return expandedIndex(raw, col, f.tab) }
		if len(spans) > 4 {
			table := expandedTable(raw, f.tab)
			index = func(col int) int { return lookup(table, col) }
		}
		for _, sp := range spans {
			from := index(sp.Start - f.off)
			to := len(cells)
			if sp.End >= 0 {
				to = min(index(sp.End-f.off), len(cells))
			}
			if from < to {
				f.applyLook(cells, from, to, sp.Style)
			}
			// A run that reaches the end of the line paints the rest of the row too.
			reaches := sp.End < 0 || (sp.End-f.off >= len(raw) && !f.winCut(ln))
			if sp.Extend && reaches && sp.Start-f.off <= len(raw) {
				tail, hasTail = PatchStyle(tail, sp.Style), true
			}
		}
	}
	if h.replaces != nil {
		for _, r := range h.replaces(i) {
			f.replaceCell(raw, cells, r)
		}
	}
	if hasTail {
		ln.tail, ln.hasTail = tail, true
	}
}

// winCut reports whether the laid out line is a window that ends before its line does.
func (f frame) winCut(ln *line) bool { return ln.long && ln.cutEnd }

// applyLook adds the patch p to the looks of cells[from:to].
func (f frame) applyLook(cells []cell, from, to int, p grid.Style) {
	x := f.dx
	var memo map[uint16]uint16
	for k := max(from, 0); k < to && k < len(cells); k++ {
		old := cells[k].dec
		nw, ok := memo[old]
		if !ok {
			nw = x.intern(PatchStyle(x.look(old), p))
			if memo == nil {
				memo = make(map[uint16]uint16, 2)
			}
			memo[old] = nw
		}
		cells[k].dec = nw
	}
}

func byPriority(a, b StyledSpan) int { return cmp.Compare(a.Priority, b.Priority) }

// replaceCell draws the replacement r over the cell of its character.
func (f frame) replaceCell(raw []rune, cells []cell, r Replacement) {
	col := r.Col - f.off
	if col < 0 || col >= len(raw) {
		return
	}
	first := expandedIndex(raw, col, f.tab)
	if first >= len(cells) {
		return
	}
	ch := raw[col]
	// n is the cells the character has in the expanded line, w the columns of the one the
	// label covers.
	n, w := 1, ansi.StringWidth(string(ch))
	switch {
	case ch == '\t':
		w = 1 // a tab is replaced in its first cell only; the rest of it stays blank
	case controlRune(ch):
		n, w = controlWidth, controlWidth
	}
	if w <= 0 || first+n > len(cells) {
		return
	}
	idx := f.dx.addRep(fitLabel(r.Text, w))
	if idx == 0 {
		return
	}
	c := &cells[first]
	c.rep = idx
	for k := 1; k < n; k++ {
		cells[first+k].rep = repHidden
	}
	// A combining mark after the character belongs to it, so it goes with it.
	for k := first + n; k < len(cells) && cells[k].blank == blankNone && isZeroWidth(cells[k].r); k++ {
		cells[k].rep = repHidden
	}
	f.applyLook(cells, first, first+n, r.Style)
}

// fitLabel is the text that fills a cell of w columns: the first grapheme of label, or a
// blank when that does not fit, then blanks up to w columns.
func fitLabel(label string, w int) string {
	g := ""
	if label != "" {
		g, _, _, _ = uniseg.FirstGraphemeClusterInString(label, -1)
		g = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, g)
	}
	gw := ansi.StringWidth(g)
	if g == "" || gw > w {
		g, gw = " ", 1
	}
	return g + strings.Repeat(" ", w-gw)
}

// cleanVirtual returns text with its control characters turned into blanks, so virtual
// text cannot move the cursor or break the row.
func cleanVirtual(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

// paintRowTail fills the rest of row y from column end with the look of the line's row
// style and extended overlays.
func (f frame) paintRowTail(g *grid.Grid, ln line, y, end int, cur bool) {
	if !ln.hasTail || end >= f.w-1 {
		return
	}
	base := f.styles.Text
	if cur && f.styles.CurrentLine.BG != nil {
		base.BG = f.styles.CurrentLine.BG
	}
	g.Fill(end, f.w-1, y, PatchStyle(base, ln.tail))
}

// paintVirtualText writes the virtual text of line i after the last cell of its last row,
// from column end of row y, and cuts it at the scrollbar column. When the row ended skip
// columns left of the window (horizontal scrolling), the text starts that far left of
// column end and its first skip columns are cut.
func (f frame) paintVirtualText(g *grid.Grid, ln line, i, y, end, skip int, cur bool) {
	if f.hooks.eol == nil || ln.long {
		return
	}
	base := f.styles.Text
	if cur && f.styles.CurrentLine.BG != nil {
		base.BG = f.styles.CurrentLine.BG
	}
	x := end
	for _, t := range f.hooks.eol(i) {
		if x >= f.w-1 {
			return
		}
		text := cleanVirtual(t.Text)
		if skip > 0 {
			w := ansi.StringWidth(text)
			if skip >= w {
				skip -= w
				continue
			}
			text, skip = ansi.TruncateLeft(text, skip, ""), 0
		}
		x += g.Put(x, y, f.w-1, text, PatchStyle(base, t.Style))
	}
}

// gutterMark returns the mark that replaces the change bar of line i, with the cell
// text cut to one column.
func (f frame) gutterMark(i int) (text string, st grid.Style, ok bool) {
	if f.hooks.gutter == nil || f.gutter <= 0 || i >= f.lines {
		return "", grid.Style{}, false
	}
	m, ok := f.hooks.gutter(i)
	if !ok {
		return "", grid.Style{}, false
	}
	return fitLabel(m.Text, 1), PatchStyle(f.styles.Gutter, m.Style), true
}

// scrollMark returns the style of the mark that line i has in the last column.
func (f frame) scrollMark(i int) (grid.Style, bool) {
	if f.hooks.scroll == nil || i >= f.lines {
		return grid.Style{}, false
	}
	m, ok := f.hooks.scroll(i)
	if !ok {
		return grid.Style{}, false
	}
	return PatchStyle(f.styles.Gutter, m.Style), true
}
