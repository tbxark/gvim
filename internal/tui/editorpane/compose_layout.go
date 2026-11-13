package editorpane

import (
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
	if f.revealed(i) && !f.fenceBody(i) && !f.noPageReveal {
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
	geom, glue := expandedGeom(raw, f.tab)
	ln := line{geom: geom, raw: raw, off: 0, spans: []span{{0, len(geom)}},
		srcToDisp: cp.srcToDisp, dispToSrc: cp.dispToSrc}
	if f.wrap {
		ln.spans = wrapRows(geom, f.pageMeasure, cp.hang, glue)
		if len(ln.spans) > 1 {
			ln.indent = cp.hang
		}
	}
	if styled {
		// The selection test takes the source column of each visible rune.
		var sel func(int) bool
		if inSel := f.selectedIn(i); inSel != nil {
			sel = func(j int) bool {
				if j < len(cp.src) {
					j = cp.src[j]
				}
				return inSel(j)
			}
		}
		ln.cells = expandCells(raw, cp.kinds, sel, f.tab)
		f.markMatchesComposed(i, raw, cp, ln.cells)
		f.markDiagnosticsComposed(i, raw, cp, ln.cells)
		f.decorateComposed(i, raw, cp.kinds, &ln)
		f.markConceals(i, raw, &ln)
	}
	return ln
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
