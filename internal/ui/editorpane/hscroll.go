package editorpane

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// hScrollMargin is how many columns the caret keeps from the left and right edge of
// the text area, as Fresh's horizontal_scroll_offset does.
const hScrollMargin = 5

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
		if f.isLong(i) {
			// A long line is not laid out here, because the window depends on the first
			// column this finds. Its columns are its runes, and its stored length bounds
			// them.
			content = max(content, f.win.LineBytes(i))
			if i == f.caret.Line {
				caretCol, caretShown = f.caret.Col, true
			}
			continue
		}
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
	if caretShown && !f.keepLeft {
		// The caret keeps a margin of columns from either edge, except where the
		// line has no more text to show.
		margin := min(hScrollMargin, (textW-1)/2)
		if caretCol < left+margin {
			left = max(caretCol-margin, 0)
		}
		if caretCol >= left+textW-margin {
			reach := caretCol - textW + 1
			left = min(reach+margin, max(content-textW+1, reach))
		}
	}
	f.left = left
}

// HScroll returns the first visible display column of a w by h pane, and the largest
// first column the visible lines allow. With Wrap on, or when the lines fit, the
// largest is 0.
func (p Pane) HScroll(w, h int) (left, most int) {
	f, ok := p.newFrame(w, h)
	if !ok {
		return 0, 0
	}
	return f.left, max(f.contentW-(f.w-1-f.gutter), 0)
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

// droppedBeyond returns how many of the n columns that dropColumns drops are past the end
// of cells: the distance by which the row ended left of the window.
func droppedBeyond(cells []cell, n int) int {
	for _, c := range cells {
		n -= ansi.StringWidth(string(c.r))
	}
	return max(n, 0)
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
