package editorpane

import (
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/syntax"
)

// mapSrcColumn maps a source rune column of a composed line to its composed rune
// column; a column inside a hidden delimiter maps to the nearest visible boundary.
func mapSrcColumn(ln line, srcCol int) int {
	if ln.srcToDisp == nil {
		return srcCol
	}
	if srcCol < 0 {
		return 0
	}
	if srcCol >= len(ln.srcToDisp) {
		return len(ln.raw)
	}
	return ln.srcToDisp[srcCol]
}

// mapDispColumn maps a composed rune column back to the source rune column.
func mapDispColumn(ln line, dispCol int) int {
	if ln.dispToSrc == nil {
		return dispCol
	}
	if dispCol < 0 {
		return 0
	}
	if dispCol >= len(ln.dispToSrc) {
		return ln.off + len(ln.raw)
	}
	return ln.dispToSrc[dispCol]
}

// layoutCompose lays out a composed Markdown line: the visible runes are wrapped
// at the page measure with the hanging indent, styled with the source syntax
// kinds, and marked with the selection and the per-line decorations mapped to the
// visible cells.
func (f frame) layoutCompose(i int, styled bool) line {
	cp := f.composedLine(i)
	if f.revealed(i) && !f.fenceBody(i) {
		// The caret's row of a table reveals its cell contents while the frame's
		// padding stays, so the borders above and below do not jump; every other
		// line reveals raw.
		if tl, ok := f.tables().rows[i]; ok && tl.Kind != syntax.TableSeparator {
			cp = f.composeTableLine(i, tl, true)
		} else {
			cp = f.rawComposed(i)
		}
	}
	raw := cp.runes
	plain := expandCells(raw, nil, nil, f.tab)
	geom := make([]rune, len(plain))
	glue := make([]bool, len(plain))
	for j, c := range plain {
		geom[j], glue[j] = c.r, c.glue
	}
	ln := line{geom: geom, raw: raw, off: 0, spans: []span{{0, len(geom)}},
		srcToDisp: cp.srcToDisp, dispToSrc: cp.dispToSrc}
	if f.wrap {
		if cp.hang > 0 {
			ln.spans = wrapRows(geom, f.pageMeasure, cp.hang, glue)
			if len(ln.spans) > 1 {
				ln.indent = cp.hang
			}
		} else {
			ln.spans = wrapRows(geom, f.pageMeasure, 0, glue)
		}
	}
	if styled {
		ln.cells = expandCellsSrc(raw, cp.kinds, cp.src, f.selectedInSrc(i), f.tab)
		f.markMatchesComposed(i, raw, cp, ln.cells)
		f.markDiagnosticsComposed(i, raw, cp, ln.cells)
		f.decorateComposed(i, raw, cp.kinds, &ln)
	}
	return ln
}

// selectedInSrc is selectedIn for a composed line: the test takes a source rune
// column, so the selection over hidden delimiters still marks the visible cells.
func (f frame) selectedInSrc(i int) func(srcCol int) bool {
	if len(f.sels) == 0 {
		return nil
	}
	var here []buffer.Selection
	for _, s := range f.sels {
		if from, to := s.Ordered(); from.Line <= i && i <= to.Line {
			here = append(here, s)
		}
	}
	if len(here) == 0 {
		return nil
	}
	return func(srcCol int) bool {
		p := buffer.Pos{Line: i, Col: srcCol}
		for _, s := range here {
			from, to := s.Ordered()
			if !p.Less(from) && p.Less(to) {
				return true
			}
		}
		return false
	}
}

// expandCellsSrc is expandCells for a composed line: sel takes the source rune
// column of each visible rune.
func expandCellsSrc(raw []rune, kinds []syntax.Kind, src []int, sel func(int) bool, tab int) []cell {
	out := make([]cell, 0, len(raw))
	col := 0
	for j, r := range raw {
		k := syntax.KindPlain
		if j < len(kinds) {
			k = kinds[j]
		}
		selected := false
		if sel != nil {
			srcCol := j
			if j < len(src) {
				srcCol = src[j]
			}
			selected = sel(srcCol)
		}
		switch {
		case r == '\t':
			n := cellAdvance(col, r, tab)
			for q := range n {
				b := blankTabPad
				if q == 0 {
					b = blankTab
				}
				out = append(out, cell{r: ' ', kind: k, sel: selected, blank: b})
			}
			col += n
		case controlRune(r):
			esc := controlEscape(r)
			for q, e := range esc {
				out = append(out, cell{r: e, kind: k, sel: selected, glue: q < len(esc)-1})
			}
			col += controlWidth
		default:
			c := cell{r: r, kind: k, sel: selected}
			if r == ' ' {
				c.blank = blankSpace
			}
			out = append(out, c)
			col += cellAdvance(col, r, tab)
		}
	}
	return out
}

// markMatchesComposed marks the search matches of a composed line: the highlight
// columns are source rune columns, which the line's map moves to visible columns.
func (f frame) markMatchesComposed(i int, raw []rune, cp composedLine, cells []cell) {
	if f.highlights == nil {
		return
	}
	var mapped []Highlight
	for _, h := range f.highlights(i) {
		from := srcColToDisp(cp, h.Start)
		to := srcColToDisp(cp, h.End)
		if to > from {
			mapped = append(mapped, Highlight{Start: from, End: to, Current: h.Current})
		}
	}
	if len(mapped) == 0 {
		return
	}
	f.highlights = func(int) []Highlight { return mapped }
	f.markMatches(i, raw, cells)
}

// markDiagnosticsComposed marks the diagnostics of a composed line through the
// same source-to-visible mapping as the search matches.
func (f frame) markDiagnosticsComposed(i int, raw []rune, cp composedLine, cells []cell) {
	if f.diags == nil {
		return
	}
	var mapped []DiagnosticSpan
	for _, d := range f.diags(i) {
		start, end := srcColToDisp(cp, d.Start), srcColToDisp(cp, d.End)
		if end <= start {
			continue
		}
		mapped = append(mapped, DiagnosticSpan{Start: start, End: end, Severity: d.Severity, Message: d.Message})
	}
	if len(mapped) == 0 {
		return
	}
	f.diags = func(int) []DiagnosticSpan { return mapped }
	f.markDiagnostics(i, raw, cells)
}

// srcColToDisp maps a source rune column to a composed rune column.
func srcColToDisp(cp composedLine, srcCol int) int {
	if len(cp.srcToDisp) == 0 {
		return srcCol
	}
	if srcCol < 0 {
		return 0
	}
	if srcCol >= len(cp.srcToDisp) {
		return len(cp.runes)
	}
	return cp.srcToDisp[srcCol]
}

// decorateComposed adds the decorations that survive the compose to a composed
// line: the whitespace marks, the bracket colors, the line-break cells and the
// blanks of the hanging indent. The indent guides and the overlays are off in the
// page view (docs/markdown.md).
func (f frame) decorateComposed(i int, raw []rune, kinds []syntax.Kind, ln *line) {
	f.markWhitespace(ln.cells)
	f.bracketsOf(i, raw, kinds, ln.cells)
	ln.cells = append(ln.cells, f.breakCells(i, raw)...)
	if ln.indent > 0 {
		pad := make([]cell, ln.indent)
		for j := range pad {
			pad[j] = cell{r: ' '}
		}
		ln.pad = pad
	}
}
