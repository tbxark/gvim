package editorpane

import (
	"slices"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// foldPlaceholder is drawn after the header line of a collapsed fold.
const foldPlaceholder = " ..."

// lineNumber is the number shown in the gutter for buffer line i: its place in the
// document, or with relative numbers its distance from the caret line. The caret
// line keeps its own number.
func (f frame) lineNumber(i int) int {
	if !f.opts.RelativeLineNumbers || i == f.caret.Line {
		return i + 1
	}
	return max(i-f.caret.Line, f.caret.Line-i)
}

// numberStyle is the style of the gutter text of line i. With relative numbers the
// caret line takes the text color, so that it stands out among the distances.
func (f frame) numberStyle(i int) grid.Style {
	st := f.styles.Gutter
	if f.opts.RelativeLineNumbers && i == f.caret.Line && f.styles.Text.FG != nil {
		st.FG = f.styles.Text.FG
	}
	return st
}

// rowCells are the cells of display row k of a laid out line, whose span is sp.
// The last row also takes the cells past the text, and a continuation row starts
// with the hanging indent.
func (f frame) rowCells(rd rowDesc) []cell {
	ln := rd.ln
	hi := rd.sp.to
	if rd.k == len(ln.spans)-1 && rd.last {
		hi = len(ln.cells)
	}
	cells := ln.cells[rd.sp.from:hi]
	if lead := f.rowLead(ln, rd.k); lead > 0 {
		cells = slices.Concat(f.leadCells(ln, rd.k), cells)
	} else if rd.cont() && ln.indent > 0 {
		cells = slices.Concat(ln.pad, cells)
	}
	return cells
}

// paintFoldPlaceholder draws the placeholder after the header of line i when its
// fold is collapsed, from column end of row y, and returns the column after it. The
// placeholder shows whether or not the fold arrows do.
func (f frame) paintFoldPlaceholder(g *grid.Grid, i, y, end int, cur bool) int {
	if !f.folds[i] {
		return end
	}
	st := f.styles.Decor.FoldPlaceholder
	if cur && f.styles.CurrentLine.BG != nil {
		st = withBackground(st, f.styles.CurrentLine)
	}
	text := foldPlaceholder
	if p, ok := f.foldPlaceholder[i]; ok {
		text = p
	}
	return end + g.Put(end, y, f.w-1, text, st)
}
