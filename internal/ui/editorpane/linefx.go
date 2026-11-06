package editorpane

import (
	"slices"

	"github.com/tbxark/gvim/internal/display"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// guideReachLines bounds the lines read upward to find the blocks open above the
// window, and downward for the next line with content below a blank one.
const guideReachLines = 200

// decorate adds the options' marks to the cells of buffer line i: the whitespace
// marks, the guides and the brackets. It also sets the hanging indent and the
// padding of the continuation rows, and adds the cells that follow the text: the
// line-end marks, and the guides through a blank line. It runs once per painted
// line, in order from the top line.
func (f frame) decorate(i int, raw []rune, kinds []syntax.Kind, ln *line) {
	f.markWhitespace(ln.cells)
	f.bracketsOf(i, raw, kinds, ln.cells)
	cols := f.lineGuides(i, string(raw))
	f.applyGuides(ln.cells, cols)
	tail := f.breakCells(i, raw)
	ln.cells = append(ln.cells, tail...)
	ln.cells = append(ln.cells, f.guideFill(len(ln.cells), cols)...)
	if ln.indent > 0 {
		ln.pad = f.padCells(i, ln.indent)
	}
}

// markWhitespace replaces the blanks of a line with the visible whitespace marks
// its options ask for. A blank is leading, inner or trailing by its place among the
// cells that are not blanks, and a line of blanks only counts as both leading and
// trailing. Inside a selection the marks are drawn when InSelection is set.
func (f frame) markWhitespace(cells []cell) {
	ws := f.fx.ws
	first, last := -1, -1
	for j, c := range cells {
		if c.blank == blankNone {
			if first < 0 {
				first = j
			}
			last = j
		}
	}
	for j := range cells {
		c := &cells[j]
		if c.blank == blankNone || c.blank == blankTabPad {
			continue
		}
		var space, tab bool
		switch {
		case first < 0:
			space, tab = ws.SpacesLeading || ws.SpacesTrailing, ws.TabsLeading || ws.TabsTrailing
		case j < first:
			space, tab = ws.SpacesLeading, ws.TabsLeading
		case j > last:
			space, tab = ws.SpacesTrailing, ws.TabsTrailing
		default:
			space, tab = ws.SpacesInner, ws.TabsInner
		}
		if c.sel && ws.InSelection {
			space, tab = true, tab || !f.noTabArrows
		}
		switch {
		case c.blank == blankSpace && space:
			c.r, c.ws = '·', true
		case c.blank == blankTab && tab:
			c.r, c.ws = '→', true
		}
	}
}

// breakCells are the cells after the text of line i for the line break: the marks
// "↵" and "␍" when the options ask for them, else one blank in the selection color
// when the break is inside the selection, so that a selection over empty lines shows.
// The last line has no break after it.
func (f frame) breakCells(i int, raw []rune) []cell {
	if i >= f.lines-1 || f.cut {
		// A window that ends before its line does shows no end of line.
		return nil
	}
	sel := f.selectedIn(i)
	selected := sel != nil && sel(len(raw))
	ws := f.fx.ws
	var marks string
	switch {
	case ws.CarriageReturns && f.fx.crlf && ws.Newlines:
		marks = "␍↵"
	case ws.CarriageReturns && f.fx.crlf:
		marks = "␍"
	case ws.Newlines:
		marks = "↵"
	}
	if marks == "" {
		if selected {
			return []cell{{r: ' ', sel: true}}
		}
		return nil
	}
	out := make([]cell, 0, 2)
	for _, r := range marks {
		out = append(out, cell{r: r, ws: true, sel: selected})
	}
	return out
}

// lineGuides returns the guide columns of buffer line i with the given text. The
// scanner for every level sees the lines in order, so this runs once per line.
func (f frame) lineGuides(i int, text string) []int {
	fx := f.fx
	if f.off > 0 {
		// A window that starts inside the line does not show its leading blanks.
		return nil
	}
	switch fx.guide {
	case guidesOff:
		return nil
	case guidesActive:
		f.setActiveGuide()
		if fx.hasActive && i >= fx.activeFrom && i <= fx.activeTo {
			return []int{fx.activeCol}
		}
		return nil
	}
	if fx.scan == nil {
		fx.scan = display.NewGuideScanner(f.tab, display.GuideAncestors(f.head, f.top, f.tab, guideReachLines))
	}
	if w, content := display.LineIndent(text, f.tab); content {
		return fx.scan.Content(w)
	}
	return fx.scan.Blank(f.nextContentIndent(i))
}

// nextContentIndent is the indent of the first line with content below line i that
// a fold does not hide, or zero when there is none within reach.
func (f frame) nextContentIndent(i int) int {
	for j, n := f.next(i), 0; j < f.lines && n < guideReachLines; j, n = f.next(j), n+1 {
		if w, content := display.LineIndent(f.head(j), f.tab); content {
			return w
		}
	}
	return 0
}

// setActiveGuide finds, once, the block around the caret for the active guide: the
// innermost fold range that holds the caret line. The guide is at the indent of the
// range's first line and runs along the lines after it.
func (f frame) setActiveGuide() {
	fx := f.fx
	if fx.activeTo != 0 || fx.hasActive {
		return
	}
	fx.activeTo = -1
	fl, ok := f.doc.(Folder)
	if !ok || isLarge(f.doc) {
		return
	}
	for _, r := range fl.Folds(f.tab) {
		if r.Start <= f.caret.Line && f.caret.Line <= r.End {
			w, _ := display.LineIndent(f.head(r.Start), f.tab)
			fx.activeCol, fx.activeFrom, fx.activeTo, fx.hasActive = w, r.Start+1, r.End, true
		}
	}
}

// applyGuides draws the guides at cols over the blanks of a line. A guide keeps the
// column of a tab stop, and the tab arrow moves one cell to the right; a tab of one
// cell keeps its arrow and loses the guide.
func (f frame) applyGuides(cells []cell, cols []int) {
	for _, gc := range cols {
		if gc >= len(cells) {
			continue
		}
		c := &cells[gc]
		switch c.blank {
		case blankSpace:
		case blankTab:
			if c.r == '→' {
				if gc+1 >= len(cells) || cells[gc+1].blank != blankTabPad {
					continue
				}
				cells[gc+1].r, cells[gc+1].ws = '→', true
			}
		case blankTabPad:
			if c.r == '→' {
				continue
			}
		default:
			continue
		}
		*c = f.withGuide(*c, gc)
	}
}

// withGuide returns c drawn as the guide of column col.
func (f frame) withGuide(c cell, col int) cell {
	c.r, c.ws, c.mark, c.lvl = f.fx.glyph, false, markGuide, uint8(col/f.tab%style.RainbowLevels)
	return c
}

// guideFill returns the cells that carry the guides past the end of a line, from
// column have to the deepest guide, so that the guides run through a blank line. The
// columns between guides are blanks.
func (f frame) guideFill(have int, cols []int) []cell {
	last := -1
	for _, c := range cols {
		if c >= have {
			last = max(last, c)
		}
	}
	if last < 0 {
		return nil
	}
	out := make([]cell, 0, last-have+1)
	for c := have; c <= last; c++ {
		cl := cell{r: ' '}
		if slices.Contains(cols, c) {
			cl = f.withGuide(cl, c)
		}
		out = append(out, cl)
	}
	return out
}

// padCells are the cells of the hanging indent of a continuation row, with the
// guides of the blocks that end inside it.
func (f frame) padCells(i, indent int) []cell {
	pad := make([]cell, indent)
	for k := range pad {
		pad[k].r = ' '
	}
	var cols []int
	switch fx := f.fx; fx.guide {
	case guidesAll:
		if fx.scan != nil {
			cols = fx.scan.Continuation(indent)
		}
	case guidesActive:
		if fx.hasActive && i >= fx.activeFrom && i <= fx.activeTo {
			cols = []int{fx.activeCol}
		}
	}
	for _, gc := range cols {
		if gc < indent {
			pad[gc] = f.withGuide(pad[gc], gc)
		}
	}
	return pad
}

// decorStyle sets the foreground and the attributes that the marks of a cell give:
// a guide, a bracket colored by depth, and the pair at the caret.
func (f frame) decorStyle(c cell, st *grid.Style) {
	d := f.styles.Decor
	switch c.mark {
	case markGuide:
		fg := d.IndentGuide.FG
		if f.fx.rainbow && d.IndentRainbow[c.lvl].FG != nil {
			fg = d.IndentRainbow[c.lvl].FG
		}
		if fg == nil {
			fg = f.styles.Whitespace.FG
		}
		if fg != nil {
			st.FG = fg
		}
	case markBracket:
		if fg := d.BracketRainbow[c.lvl].FG; fg != nil {
			st.FG = fg
		}
	}
	if !c.pair {
		return
	}
	if c.mark == markBracket {
		m := d.BracketPair
		if !m.Bold && !m.Underline && !m.Italic && !m.Faint && !m.Reverse {
			m.Bold, m.Underline = true, true
		}
		st.Bold, st.Underline = st.Bold || m.Bold, st.Underline || m.Underline
		st.Italic, st.Faint, st.Reverse = st.Italic || m.Italic, st.Faint || m.Faint, st.Reverse || m.Reverse
		return
	}
	if fg := d.BracketMatch.FG; fg != nil {
		st.FG = fg
	} else {
		st.Bold = true
	}
}
