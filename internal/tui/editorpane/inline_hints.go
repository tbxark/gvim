package editorpane

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// InlineHint is display-only text spliced into a display row before rune column Col of
// its line (tier-2 inlay hints). The cells take columns and move the text after them on
// the row, but they are not text of the document: copy, edit, undo and save use the
// document, and the caret, the mouse, selection, search and diagnostics keep the line's
// own columns (see cellOf/posAt). The root supplies hints through Pane.InlineHints.
//
// The hint is spliced before the character at Col, and at the end of the row when Col is
// the line's length (the end-of-line anchor). Text carries its own spacing (the root adds
// Fresh's before-char spacing and the protocol padding). Style is a patch over the row
// look, so a faint hint keeps the current-line tint and the selection under it is not
// painted on the hint's own cells.
type InlineHint struct {
	Col   int
	Text  string
	Style grid.Style
}

// inlinePlacement is one hint placed on a display row.
type inlinePlacement struct {
	// at is the index in the row's cell slice the hint goes before.
	at int
	// atCol is the hint's insertion column from the row's start, prefix included.
	atCol int
	// width is the display width of text, which may be a truncated part of the hint.
	width int
	text  string
	style grid.Style
	// anchor is the line's rune column the hint is anchored to, for a press inside
	// its cells.
	anchor int
}

// inlineLineData is the placement of one line's hints on its display rows, and the
// hints no row could take (the end-of-line fallback).
type inlineLineData struct {
	rows     map[int][]inlinePlacement
	unplaced []InlineHint
}

// inlineAvailable reports whether inline hints can be placed on line i's layout at all:
// the page view composes the columns, a windowed (long) line shows a slice of the line,
// and a tall line's rows are separate segments, so those keep the end-of-line form.
func (f frame) inlineAvailable(i int, ln line) bool {
	return f.inline != nil && !f.pageOn && !ln.long && ln.srcToDisp == nil && !f.isTall(i)
}

// inlineData returns the placements of line i's hints. It is computed once per line per
// frame and shared by the painter and the caret and hit paths, so a moved caret does not
// lay a line out again for its hints.
func (f frame) inlineData(i int, ln line) inlineLineData {
	if data, ok := f.inlineCache[i]; ok {
		return data
	}
	data := inlineLineData{rows: map[int][]inlinePlacement{}}
	if f.inline != nil && !f.inlineAvailable(i, ln) {
		if f.pageOn || ln.long || f.isTall(i) {
			// The page view and a line shown as a window or in segments keep the
			// end-of-line form for every hint.
			data.unplaced = f.inline(i)
		}
		return data
	}
	if f.inline == nil {
		return data
	}
	// Hint cells cost this much of the pane's text width on their row. Placed widths
	// accumulate so a row full of text only takes the hints that fit: no hint may push
	// document text past the pane's edge.
	rowWidth := make(map[int]int, len(ln.spans))
	inset := make(map[int]int, len(ln.spans))
	prefixW := make(map[int]int, len(ln.spans))
	for k, sp := range ln.spans {
		_, preW := f.inlinePrefix(ln, k)
		prefixW[k] = preW
		rowWidth[k] = preW + ansi.StringWidth(string(ln.geom[sp.from:sp.to]))
	}
	for _, h := range f.inline(i) {
		col := min(max(h.Col, 0), len(ln.raw))
		text := cleanVirtual(h.Text)
		if text == "" || ansi.StringWidth(text) == 0 || f.inlineConcealed(i, col) {
			// An insertion point inside a concealed range would split the hidden run
			// and the replacement text: the hint keeps the end-of-line form there.
			data.unplaced = append(data.unplaced, h)
			continue
		}
		idx := expandedIndex(ln.raw, col, f.tab)
		k, inRow := inlineRowOf(ln.spans, idx)
		sp := ln.spans[k]
		room := f.textRight() - f.gutter - rowWidth[k] - inset[k]
		if room <= 0 {
			data.unplaced = append(data.unplaced, h)
			continue
		}
		if w := ansi.StringWidth(text); w > room {
			text = ansi.Truncate(text, room, "")
			if text == "" || ansi.StringWidth(text) == 0 {
				data.unplaced = append(data.unplaced, h)
				continue
			}
		}
		prefixCells, _ := f.inlinePrefix(ln, k)
		atCol := prefixW[k] + ansi.StringWidth(string(ln.geom[sp.from:min(sp.from+inRow, sp.to)]))
		w := ansi.StringWidth(text)
		inset[k] += w
		data.rows[k] = append(data.rows[k], inlinePlacement{
			at:     prefixCells + inRow,
			atCol:  atCol,
			width:  w,
			text:   text,
			style:  h.Style,
			anchor: col,
		})
	}
	return data
}

// inlineConcealed reports whether rune column col of line i falls in a conceal span that is
// active on the line. A hint there falls back to the end of the line.
func (f frame) inlineConcealed(i, col int) bool {
	if f.conceals == nil {
		return false
	}
	for _, c := range f.conceals(i) {
		if !c.activeOn(i, f.caret.Line) {
			continue
		}
		end := c.End
		if end < 0 || end > len(f.doc.Line(i)) {
			end = len(f.doc.Line(i))
		}
		if col >= c.Start && col < end {
			return true
		}
	}
	return false
}

// inlineRowOf returns the display row of the insertion point idx of the line's geom, and
// its geom offset in that row. A point exactly on a row boundary is the start of the
// next row, so a hint before a wrapped character lands on the next row's first column;
// past the last row's end it is the last row's end.
func inlineRowOf(spans []span, idx int) (k, inRow int) {
	for k, sp := range spans {
		if idx < sp.to {
			return k, max(idx-sp.from, 0)
		}
	}
	k = len(spans) - 1
	return k, spans[k].to - spans[k].from
}

// inlinePrefix returns the number of cells and the display width the layout prepends to
// the text of a continuation row (the hanging indent or a soft break's lead cells). The
// cells are not text, so a hint at the row's first column goes after them.
func (f frame) inlinePrefix(ln line, k int) (cells, width int) {
	if k <= 0 {
		return 0, 0
	}
	if lead := f.rowLead(ln, k); lead > 0 {
		lc := f.leadCells(ln, k)
		return len(lc), cellsWidth(lc)
	}
	if ln.indent > 0 {
		return len(ln.pad), cellsWidth(ln.pad)
	}
	return 0, 0
}

// spliceInlineHints inserts line i's hints into display row k's cells, before painting.
// The hint cells carry the hint style through the cell look table, so the row paints with
// the same run grouping and patching as any other cell. inserted reports the placements
// spliced, for callers that need to know nothing else is left of the row.
func (f frame) spliceInlineHints(i int, ln line, k int, cells []cell) (out []cell, inserted []inlinePlacement) {
	if f.dx == nil {
		return cells, nil
	}
	data := f.inlineData(i, ln)
	placements := data.rows[k]
	if len(placements) == 0 {
		return cells, nil
	}
	out = make([]cell, 0, len(cells)+16)
	cursor := 0
	for _, p := range placements {
		at := min(max(p.at, cursor), len(cells))
		out = append(out, cells[cursor:at]...)
		cursor = at
		dec := f.dx.intern(p.style)
		for _, r := range p.text {
			out = append(out, cell{r: r, dec: dec})
		}
	}
	out = append(out, cells[cursor:]...)
	return out, placements
}

// inlineShift is how many display columns the hints insert before column cells of the
// row: a caret at cells sits left of a hint only when the hint's insertion column is
// after it, which is what keeps the caret on its own character.
func (f frame) inlineShift(i int, ln line, k, cells int) int {
	data := f.inlineData(i, ln)
	shift := 0
	for _, p := range data.rows[k] {
		if p.atCol <= cells {
			shift += p.width
		}
	}
	return shift
}

// inlineUnshift maps a display column of a hint-bearing row back to the line: the widths
// of hints before it are taken away, and a column inside a hint maps to the hint's anchor
// column (a press on a hint is a press on the character it annotates). ok is true then.
func (f frame) inlineUnshift(i int, ln line, k, cells int) (adjusted int, anchor int, ok bool) {
	data := f.inlineData(i, ln)
	adjusted = cells
	for _, p := range data.rows[k] {
		if cells < p.atCol {
			break
		}
		if cells < p.atCol+p.width {
			return 0, p.anchor, true
		}
		adjusted -= p.width
	}
	return adjusted, 0, false
}

// paintInlineHintFallback paints the hints no row could take as end-of-line text, the
// tier-1 form: the row is full, or the line is in the page view, shown as a window, tall,
// or panned by horizontal scrolling. It returns the column after the painted text.
func (f frame) paintInlineHintFallback(g *grid.Grid, i int, ln line, y, end int, cur bool) int {
	if f.inline == nil || ln.long || f.left > 0 {
		return end
	}
	data := f.inlineData(i, ln)
	if len(data.unplaced) == 0 {
		return end
	}
	base := f.styles.Text
	if cur && f.styles.CurrentLine.BG != nil {
		base = withBackground(base, f.styles.CurrentLine)
	}
	x := end
	for _, h := range data.unplaced {
		if x >= f.w-1 {
			break
		}
		text := cleanVirtual(h.Text)
		if text == "" {
			continue
		}
		x += g.Put(x, y, f.w-1, " "+text, PatchStyle(base, h.Style))
	}
	return x
}
