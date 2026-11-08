package editorpane

import "github.com/tbxark/gvim/syntax"

// PageViewTableBorders builds the page-view extra rows that draw a Markdown
// table's missing frame: the top border (┌─┬─┐) above the table's header line and
// the bottom border (└─┴─┘) below its last line. The middle border is the source
// separator row (compose_table.go). The rows go through the shared inserted-row
// hook (Pane.PageExtraRows, rows_extra.go), which owns every row calculation, so
// the caret, the mouse hit, selection, scroll and the scrollbar all follow the
// borders as ordinary display rows.
//
// The hook passes the frame's actual width, gutter and page measure, so the
// border text matches the row width the compose used: the same table model and
// column widths (memoized per text version) size both. A page too narrow for the
// table gets no borders: its rows wrap, and a border would not line up.
func PageViewTableBorders(doc Document, styles Styles) func(line, width, gutter, measure int) ExtraRows {
	if doc == nil {
		return nil
	}
	text := styles.Text
	return func(line, width, gutter, measure int) ExtraRows {
		if measure <= 0 {
			return ExtraRows{}
		}
		// A frame is the pane's state without geometry: the table model and its
		// widths depend only on the document text (memoized per version) and the
		// measure the hook reports, which is the page measure the compose used.
		tm := frame{doc: doc, pageOn: true, pageMarkdown: true, pageMeasure: measure}.tables()
		// Fenced code wins over a table-looking line inside it: the compose draws the
		// frame, so the extra rows are the frame's continuation rows only.
		if fl, ok := (frame{doc: doc, pageOn: true, pageMarkdown: true, pageMeasure: measure}).markdownFences()[line]; ok {
			if fl.Kind != syntax.FenceBody {
				return ExtraRows{}
			}
			if cur := doc.Cursor(); line == cur.Line || (line+1 == cur.Line && cur.Col == 0) {
				return ExtraRows{}
			}
			depth := max(fl.QuoteDepth, 0)
			raw := []rune(doc.Line(line))
			base := quotePrefixLen(raw, depth)
			indent := max(fl.Indent, 0)
			inner := frameWidth(measure-2*depth, indent) - 4
			segs := fenceBodySegments(raw, base+indent, inner)
			var rows []ExtraRow
			for k := 1; k < len(segs); k++ {
				rows = append(rows, ExtraRow{
					Text:     fenceBodyRowText(depth, indent, inner, segs[k]),
					Style:    text,
					Priority: k - 1,
				})
			}
			return ExtraRows{Below: rows}
		}
		row, ok := tm.rows[line]
		if !ok {
			return ExtraRows{}
		}
		layout, ok := tm.width[row.Table]
		lines := tm.lines[row.Table]
		if !ok || len(lines) == 0 || len(layout.widths) == 0 {
			return ExtraRows{}
		}
		if tableWidth(layout.widths) > measure {
			return ExtraRows{}
		}
		cur := doc.Cursor()
		// The caret's line renders raw, so its wrapped continuation rows stand down.
		revealed := line == cur.Line || (line+1 == cur.Line && cur.Col == 0)
		var above, below []ExtraRow
		if line == lines[0] {
			above = append(above, ExtraRow{Text: tableBorder(layout.widths, "┌", "┬", "┐"), Style: text})
		}
		if !revealed {
			cellsAtLine := tm.cells[line]
			maxSeg := 0
			for _, segs := range cellsAtLine {
				maxSeg = max(maxSeg, len(segs))
			}
			for k := 1; k < maxSeg; k++ {
				below = append(below, ExtraRow{
					Text:     tableContinuationRow(layout.widths, cellsAtLine, k),
					Style:    text,
					Priority: k - 1,
				})
			}
		}
		if line == lines[len(lines)-1] {
			below = append(below, ExtraRow{
				Text:     tableBorder(layout.widths, "└", "┴", "┘"),
				Style:    text,
				Priority: len(below),
			})
		}
		return ExtraRows{Above: above, Below: below}
	}
}

// tableWidth is the width of a composed row: one cell per pipe, one blank on
// each side of every cell, and the cells themselves.
func tableWidth(widths []int) int {
	n := len(widths)
	w := 3*n + 1
	for _, c := range widths {
		w += c
	}
	return w
}

// tableBorder builds a horizontal border line of the given corner and junction
// glyphs, one fill run per column, so it matches tableWidth exactly.
func tableBorder(widths []int, left, mid, right string) string {
	var b []rune
	b = append(b, []rune(left)...)
	for i, w := range widths {
		for j := 0; j < w+2; j++ {
			b = append(b, '─')
		}
		switch {
		case i == len(widths)-1:
			b = append(b, []rune(right)...)
		default:
			b = append(b, []rune(mid)...)
		}
	}
	return string(b)
}
