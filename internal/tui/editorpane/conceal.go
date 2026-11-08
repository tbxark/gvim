package editorpane

import (
	"github.com/tbxark/gvim/internal/tui/grid"
)

// Conceals and the cursor-line overlay are the pane's tier-1c decoration surface: a decorate
// hook may hide ranges of cells (without changing the line's columns, so caret, mouse and all
// other mappings stay source-based) and tint the caret's row. Conceal replacement text is
// drawn inside the hidden cells, so nothing moves.
//
// Conceal spans use rune columns of the buffer line. Their activation is evaluated per line
// in the frame: unless-cursor-in skips the caret's line, if-cursor-in applies only there, and
// scope bounds skip lines outside [ScopeStart, ScopeEnd]. A span with End < 0 runs to the end
// of the line.

// ConcealActivation says when a conceal span applies.
type ConcealActivation uint8

const (
	// ConcealAlways applies the span on every line.
	ConcealAlways ConcealActivation = iota
	// ConcealUnlessCursorIn skips the span on the caret's line.
	ConcealUnlessCursorIn
	// ConcealIfCursorIn applies the span only on the caret's line.
	ConcealIfCursorIn
)

// ConcealSpan hides the cells of one line's range, drawing replacement text in the first
// hidden cells instead. Start and End are rune columns of the line; End < 0 is the end.
type ConcealSpan struct {
	Start, End int
	Text       string
	Style      grid.Style
	Activation ConcealActivation
	// Scoped says ScopeStart/ScopeEnd bound the lines the span applies on.
	Scoped               bool
	ScopeStart, ScopeEnd int
}

// activeOn is active for the line being laid out.
func (c ConcealSpan) activeOn(line, caret int) bool {
	if c.Scoped && (line < c.ScopeStart || line > c.ScopeEnd) {
		return false
	}
	switch c.Activation {
	case ConcealUnlessCursorIn:
		return line != caret
	case ConcealIfCursorIn:
		return line == caret
	}
	return true
}

// markConceals hides the cells of the line's conceal spans and draws their replacement text.
// It runs after the other cell marks, so the conceal style and replacement win.
func (f frame) markConceals(i int, raw []rune, ln *line) {
	if f.conceals == nil || f.dx == nil || len(ln.cells) == 0 {
		return
	}
	spans := f.conceals(i)
	if len(spans) == 0 {
		return
	}
	cells := ln.cells
	index := func(col int) int { return expandedIndex(raw, max(col, 0), f.tab) }
	if len(spans) > 4 {
		table := expandedTable(raw, f.tab)
		index = func(col int) int { return lookup(table, col) }
	}
	caret := f.caret.Line
	for _, sp := range spans {
		start := sp.Start
		if ln.srcToDisp != nil && len(ln.dispToSrc) > 0 {
			// The page view composes the line: the span's source column maps to a composed one.
			start = mapSrcColumn(*ln, start)
		}
		start -= ln.off
		from := index(start)
		to := len(cells)
		if sp.End >= 0 {
			end := sp.End
			if ln.srcToDisp != nil && len(ln.dispToSrc) > 0 {
				end = mapSrcColumn(*ln, end)
			}
			end -= ln.off
			to = min(index(end), len(cells))
		}
		from = min(max(from, 0), len(cells))
		if from >= to || !sp.activeOn(i, caret) {
			continue
		}
		f.applyLook(cells, from, to, sp.Style)
		rep := []rune(sp.Text)
		k := 0
		for j := from; j < to; j++ {
			// The cells keep their columns: a replacement rune of one column is written into
			// the cell, and every other cell becomes a blank with the conceal look. repHidden
			// would collapse the run, moving the rest of the line left.
			if k < len(rep) && !controlRune(rep[k]) && cellAdvance(0, rep[k], f.tab) == 1 {
				cells[j].r = rep[k]
				k++
				continue
			}
			cells[j].r = ' '
			cells[j].glue = false
			cells[j].blank = blankNone
		}
	}
}

// paintCursorLineOverlay paints the caret row's overlay last among the row paints, so its
// background wins over the current-line tint while the caret and the secondary carets are
// still drawn on top. Only the text columns are painted; the scrollbar column keeps its mark.
func (f frame) paintCursorLineOverlay(g *grid.Grid) {
	if f.cursorLine == nil {
		return
	}
	patch, ok := f.cursorLine()
	if !ok || patch == (grid.Style{}) {
		return
	}
	_, y, ok := f.caretCell()
	if !ok || y < 0 || y >= f.h {
		return
	}
	for x := 0; x < f.w-1; x++ {
		c := g.At(x, y)
		if c.Text == "" && c.Style == (grid.Style{}) {
			continue
		}
		g.Set(x, y, c.Text, PatchStyle(c.Style, patch))
	}
}
