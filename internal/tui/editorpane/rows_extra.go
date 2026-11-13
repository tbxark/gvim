package editorpane

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// Extra rows and soft breaks are the shared inserted-row mechanism of the pane (tier 1c-B).
// They run after the line layout (wrap, folds, the markdown compose) and before selection
// and current-line painting:
//
//	wrap+folds -> markdown compose -> conceal -> soft breaks -> virtual lines above/below
//	  -> cursor-line overlay -> selection/tail
//
// The frame keeps ONE cache of them per line, and every row calculation (rowCount, rowsIn,
// caretRow, caretRowIn, cellOf, posAt, Hit, ScrollTopFor, reveal, thumb) reads that cache, so
// the caret, the mouse, selection, search and diagnostics keep a single source<->display
// mapping. A line contributes:
//
//	fillAbove + len(Above) + its layout rows + len(Below) + fillAfter
//
// Extra rows are painted in slice order: of the rows Above, the last is nearest the line;
// of the rows Below, the first is nearest. A press on an extra row maps to the start of its
// line (Above) or its last position (Below).

// ExtraTextOverlay is a styled byte range within an extra row's text (a virtual line's
// text_overlays).
type ExtraTextOverlay struct {
	Start, End int
	Style      grid.Style
}

// ExtraRow is one display row that no text of the line owns.
type ExtraRow struct {
	Text  string
	Style grid.Style
	// GutterText draws one cell in the gutter column of the row (the last gutter column).
	GutterText  string
	GutterStyle grid.Style
	Priority    int
	// Overlays style byte ranges of Text (the virtual line's text_overlays).
	Overlays []ExtraTextOverlay
}

// ExtraRows is what line i adds around its own layout rows.
type ExtraRows struct {
	Above, Below []ExtraRow
}

// SoftBreak forces a display row boundary at rune column Col of a line. The text after it
// continues on a new row whose first cells are Indent blanks and the (cleaned) PrefixText
// in PrefixStyle; the columns of the text itself do not change.
type SoftBreak struct {
	Col                  int
	Indent               int
	PrefixText           string
	PrefixStyle          grid.Style
	Priority             int
	Activation           ConcealActivation
	Scoped               bool
	ScopeStart, ScopeEnd int
}

// activeOn reports whether the break applies on line when caret is the caret's line.
func (s SoftBreak) activeOn(line, caret int) bool {
	if s.Scoped && (line < s.ScopeStart || line > s.ScopeEnd) {
		return false
	}
	switch s.Activation {
	case ConcealUnlessCursorIn:
		return line != caret
	case ConcealIfCursorIn:
		return line == caret
	}
	return true
}

// extrasFor returns the extra rows of line i, merging the pane's rows with the page-view
// builder's width-dependent rows.
func (f frame) extrasFor(i int) ExtraRows {
	if (f.extras == nil && f.pageExtras == nil) || f.fill != nil || f.isTall(i) {
		// Filler panes and tall segments keep their own row model.
		return ExtraRows{}
	}
	if f.extraCache == nil {
		f.extraCache = make(map[int]ExtraRows)
	}
	if ex, ok := f.extraCache[i]; ok {
		return ex
	}
	var ex ExtraRows
	if f.extras != nil {
		ex = f.extras(i)
	}
	if f.pageExtras != nil && f.pageOn {
		// Concat builds new slices: appending would write into the hook's backing array.
		page := f.pageExtras(i, f.w, f.gutter, f.pageMeasure)
		ex.Above = slices.Concat(ex.Above, page.Above)
		ex.Below = slices.Concat(ex.Below, page.Below)
	}
	ex.Above = sortedExtraRows(ex.Above)
	ex.Below = sortedExtraRows(ex.Below)
	ex.Above = f.wrapExtraRows(ex.Above)
	ex.Below = f.wrapExtraRows(ex.Below)
	f.extraCache[i] = ex
	return ex
}

// maxExpandedExtraRows bounds the display rows one line's extra rows may expand to when
// they are wrapped, so a plugin cannot make one line cost unbounded rows.
const maxExpandedExtraRows = 256

// wrapExtraRows expands the extra rows that are longer than the pane's text width into
// several display rows, so a virtual line wraps like a text row. The gutter glyph is
// drawn on the first chunk only; text overlays are re-based to their chunk. The total
// expansion is capped at maxExpandedExtraRows.
func (f frame) wrapExtraRows(rows []ExtraRow) []ExtraRow {
	if len(rows) == 0 {
		return rows
	}
	avail := f.w - 1 - f.gutter
	if avail < 1 {
		avail = 1
	}
	out := make([]ExtraRow, 0, len(rows))
	for _, row := range rows {
		text := cleanVirtual(row.Text)
		if ansi.StringWidth(text) <= avail {
			row.Text = text
			out = append(out, row)
			if len(out) >= maxExpandedExtraRows {
				return out[:maxExpandedExtraRows]
			}
			continue
		}
		for k, ch := range wrapExtraText(text, avail, row.Overlays) {
			r := row
			r.Text = ch.text
			r.Overlays = ch.overlays
			if k > 0 {
				r.GutterText = ""
			}
			out = append(out, r)
			if len(out) >= maxExpandedExtraRows {
				return out[:maxExpandedExtraRows]
			}
		}
	}
	return out
}

// extraChunk is one wrapped display row of an extra row: its text and the overlays whose
// byte ranges fall in it (re-based to the chunk).
type extraChunk struct {
	text     string
	overlays []ExtraTextOverlay
}

// wrapExtraText splits text into chunks of at most avail display columns. The split is
// byte-safe (a wide rune that cannot fit is kept whole) and the overlays are clipped and
// re-based for each chunk.
func wrapExtraText(text string, avail int, overlays []ExtraTextOverlay) []extraChunk {
	var out []extraChunk
	start, width := 0, 0
	for i, r := range text {
		w := ansi.StringWidth(string(r))
		if width > 0 && width+w > avail {
			out = append(out, extraChunk{text: text[start:i], overlays: chunkOverlays(overlays, start, i)})
			start, width = i, 0
		}
		width += w
	}
	if start < len(text) {
		out = append(out, extraChunk{text: text[start:], overlays: chunkOverlays(overlays, start, len(text))})
	}
	return out
}

// chunkOverlays returns the overlays of [from, to) of text, re-based to the chunk start.
func chunkOverlays(overlays []ExtraTextOverlay, from, to int) []ExtraTextOverlay {
	var out []ExtraTextOverlay
	for _, ov := range overlays {
		start, end := max(ov.Start, from), min(ov.End, to)
		if start >= end {
			continue
		}
		out = append(out, ExtraTextOverlay{Start: start - from, End: end - from, Style: ov.Style})
	}
	return out
}

// sortedExtraRows returns rows in priority order, ties in their given order. It sorts a
// copy: the slice may be the one a hook returned and keeps.
func sortedExtraRows(rows []ExtraRow) []ExtraRow {
	if len(rows) < 2 || slices.IsSortedFunc(rows, extraRowOrder) {
		return rows
	}
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, extraRowOrder)
	return rows
}

func extraRowOrder(a, b ExtraRow) int { return cmp.Compare(a.Priority, b.Priority) }

// aboveCount and belowCount are the extra rows line i adds.
func (f frame) aboveCount(i int) int { return len(f.extrasFor(i).Above) }
func (f frame) belowCount(i int) int { return len(f.extrasFor(i).Below) }

// softBreaksFor returns the active soft breaks of line i, sorted by column.
func (f frame) softBreaksFor(i int) []SoftBreak {
	if f.soft == nil {
		return nil
	}
	if f.softCache == nil {
		f.softCache = make(map[int][]SoftBreak)
	}
	if sb, ok := f.softCache[i]; ok {
		return sb
	}
	all := f.soft(i)
	var sb []SoftBreak
	for _, b := range all {
		if b.activeOn(i, f.caret.Line) && b.Col >= 0 {
			sb = append(sb, b)
		}
	}
	if len(sb) > 1 {
		slices.SortStableFunc(sb, func(a, b SoftBreak) int {
			if a.Col != b.Col {
				return cmp.Compare(a.Col, b.Col)
			}
			return cmp.Compare(a.Priority, b.Priority)
		})
	}
	f.softCache[i] = sb
	return sb
}

// rowLead is the number of added cells at the start of row k of ln: the soft break's indent
// and prefix. A row without a soft break gives 0 and uses the hanging indent instead.
func (f frame) rowLead(ln line, k int) int {
	if k < 0 || k >= len(ln.lead) {
		return 0
	}
	return ln.lead[k]
}

// breakCell is the expanded-cell index of rune column col of ln (the source line of the
// layout; composed lines map the source column through srcToDisp).
func (f frame) breakCell(ln line, col int) int {
	if ln.srcToDisp != nil && len(ln.dispToSrc) > 0 {
		col = mapSrcColumn(ln, col)
	}
	col -= ln.off
	if col < 0 {
		return 0
	}
	return expandedIndex(ln.raw, col, f.tab)
}

// withSoftBreaks splits the layout rows of ln at its soft breaks. The returned line shares
// the cells of the base line and has its own spans, lead and prefix slices.
func (f frame) withSoftBreaks(i int, ln line) line {
	sbs := f.softBreaksFor(i)
	if len(sbs) == 0 || len(ln.spans) == 0 {
		return ln
	}
	// The break cells that fall inside the line; a break exactly at a row start belongs to
	// that row.
	byCell := make(map[int]SoftBreak, len(sbs))
	var cells []int
	for _, b := range sbs {
		cell := f.breakCell(ln, b.Col)
		if cell <= 0 || cell >= len(ln.geom) {
			continue
		}
		if _, dup := byCell[cell]; dup {
			continue
		}
		byCell[cell] = b
		cells = append(cells, cell)
	}
	if len(cells) == 0 {
		return ln
	}
	slices.Sort(cells)
	type row struct {
		from, to int
		brk      *SoftBreak
	}
	var rows []row
	breakAt := func(cell int) *SoftBreak {
		if b, ok := byCell[cell]; ok {
			return &b
		}
		return nil
	}
	bi := 0
	for _, sp := range ln.spans {
		cur := sp.from
		for bi < len(cells) && cells[bi] < sp.to && cells[bi] > cur {
			rows = append(rows, row{from: cur, to: cells[bi], brk: breakAt(cur)})
			cur = cells[bi]
			bi++
		}
		rows = append(rows, row{from: cur, to: sp.to, brk: breakAt(cur)})
	}
	if len(rows) == 0 {
		return ln
	}
	out := ln
	out.spans = make([]span, len(rows))
	out.lead = make([]int, len(rows))
	out.prefix = make([]string, len(rows))
	out.prefixStyle = make([]grid.Style, len(rows))
	for k, r := range rows {
		out.spans[k] = span{from: r.from, to: r.to}
		if r.brk != nil {
			out.prefix[k] = cleanVirtual(r.brk.PrefixText)
			out.prefixStyle[k] = r.brk.PrefixStyle
			out.lead[k] = max(r.brk.Indent, 0) + ansi.StringWidth(out.prefix[k])
		}
	}
	// A row that follows a soft break carries its own lead instead of the hanging indent.
	for k := 1; k < len(out.lead); k++ {
		if out.lead[k] > 0 {
			out.indent = 0
			break
		}
	}
	return out
}

// leadCells builds the cells that sit before the text of row k: indent blanks and the
// prefix text in its style, fitted to the lead width.
func (f frame) leadCells(ln line, k int) []cell {
	if len(ln.lead) == 0 || k >= len(ln.lead) || ln.lead[k] <= 0 {
		return nil
	}
	width := ln.lead[k]
	prefix := ""
	if k < len(ln.prefix) {
		prefix = ln.prefix[k]
	}
	var st grid.Style
	if k < len(ln.prefixStyle) {
		st = ln.prefixStyle[k]
	}
	dec := uint16(0)
	if f.dx != nil {
		dec = f.dx.intern(st)
	}
	pw := ansi.StringWidth(prefix)
	indent := max(width-pw, 0)
	out := make([]cell, 0, width)
	for range indent {
		out = append(out, cell{r: ' ', dec: dec, blank: blankSpace})
	}
	for _, r := range prefix {
		adv := ansi.StringWidth(string(r))
		if len(out)+adv > width {
			break
		}
		if r == ' ' {
			out = append(out, cell{r: ' ', dec: dec, blank: blankSpace})
			continue
		}
		out = append(out, cell{r: r, dec: dec})
	}
	for len(out) < width {
		out = append(out, cell{r: ' ', dec: dec, blank: blankSpace})
	}
	return out
}

// paintExtraRow paints one inserted display row: the blank gutter (with its glyph), then
// the row's text. The row is not text of the document, so it takes no selection and no
// line number.
func (f frame) paintExtraRow(g *grid.Grid, i int, er ExtraRow, y int, cur bool) {
	if f.gutter > 0 && !f.pageOn {
		label := fmt.Sprintf("%*s │ ", f.gutter-gutterSeparator, "")
		g.Put(0, y, f.w-1, label, f.styles.Gutter)
		if er.GutterText != "" {
			st := f.styles.Gutter
			if er.GutterStyle != (grid.Style{}) {
				st = PatchStyle(st, er.GutterStyle)
			}
			g.Set(f.gutter-1, y, er.GutterText, st)
		}
	}
	st := er.Style
	if cur && f.styles.CurrentLine.BG != nil {
		st = PatchStyle(st, grid.Style{BG: f.styles.CurrentLine.BG})
	}
	text := cleanVirtual(er.Text)
	if text == "" {
		return
	}
	if len(er.Overlays) == 0 {
		g.Put(f.gutter, y, f.w-1, text, st)
		return
	}
	// Paint the byte ranges of the row's overlays with their styles, the rest plain.
	runs := make([]ExtraTextOverlay, 0, len(er.Overlays))
	for _, ov := range er.Overlays {
		from, to := max(ov.Start, 0), max(ov.End, ov.Start)
		if to > len(text) {
			to = len(text)
		}
		if from >= to {
			continue
		}
		runs = append(runs, ExtraTextOverlay{Start: from, End: to, Style: ov.Style})
	}
	slices.SortStableFunc(runs, func(a, b ExtraTextOverlay) int { return cmp.Compare(a.Start, b.Start) })
	x, at := f.gutter, 0
	for _, r := range runs {
		if r.End <= at {
			continue // inside the run before it
		}
		r.Start = max(r.Start, at)
		x += g.Put(x, y, f.w-1, text[at:r.Start], st)
		x += g.Put(x, y, f.w-1, text[r.Start:r.End], PatchStyle(st, r.Style))
		at = r.End
	}
	if at < len(text) {
		g.Put(x, y, f.w-1, text[at:], st)
	}
}
