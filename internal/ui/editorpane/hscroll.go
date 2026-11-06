package editorpane

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// placeHorizontal sets the horizontal layout of a frame whose body has rows
// rows. With Wrap on it does nothing. With Wrap off it sets the widest visible
// line, the horizontal bar when that line is wider than the text area, and the
// first visible display column.
//
// The first column is want, clamped to the widest line. The window also moves so
// the caret stays on it. The bar takes the last body row, so the text has one
// row less while it shows.
func (f *frame) placeHorizontal(want, rows int) {
	if f.wrap {
		return
	}
	textW := f.w - 1 - f.gutter
	content, caretCol, caretShown := 0, 0, false
	for i, n := f.top, 0; i < f.lines && n < rows; i, n = f.next(i), n+1 {
		ln := f.layoutLine(i, false)
		content = max(content, ansi.StringWidth(string(ln.geom)))
		if i == f.caret.Line {
			raw := []rune(f.doc.Line(i))
			caretCol = ansi.StringWidth(string(ln.geom[:expandedIndex(raw, f.caret.Col, f.tab)]))
			caretShown = true
		}
	}
	f.contentW = content
	if content > textW && rows >= 2 && textW > 0 && !f.noHBar {
		f.hbar, f.barY, f.h = true, rows-1, rows-1
	}
	if textW <= 0 {
		return
	}
	left := min(max(want, 0), max(content-textW, 0))
	if caretShown {
		if caretCol < left {
			left = caretCol
		}
		if caretCol >= left+textW {
			left = caretCol - textW + 1
		}
	}
	f.left = left
}

// paintHorizontalBar draws the horizontal scrollbar on its row, from the gutter to
// the column before the last one. The track is a "─" in the track style and the
// thumb a "━" in the thumb style. The thumb's length is the share of the text
// area in the widest line, and its start is the share of the first column.
func (f frame) paintHorizontalBar(g *grid.Grid) {
	if !f.hbar {
		return
	}
	track := f.w - 1 - f.gutter
	if track <= 0 || f.contentW <= 0 {
		return
	}
	length := min(track, max(1, track*track/f.contentW))
	start := min(f.left*track/f.contentW, track-length)
	x0 := f.gutter
	for x := x0; x < x0+track; x++ {
		g.Set(x, f.barY, trackHGlyph, f.styles.Scrollbar)
	}
	for x := x0 + start; x < x0+start+length; x++ {
		g.Set(x, f.barY, thumbHGlyph, f.styles.Thumb)
	}
}

// dropColumns returns cells without its first n display columns. A cell that
// straddles the cut is dropped with the columns before it.
func dropColumns(cells []cell, n int) []cell {
	for k := range cells {
		if n <= 0 {
			return cells[k:]
		}
		n -= ansi.StringWidth(string(cells[k].r))
	}
	return nil
}
