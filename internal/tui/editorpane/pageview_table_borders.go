package editorpane

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
		switch {
		case line == lines[0]:
			return ExtraRows{Above: []ExtraRow{{Text: tableBorder(layout.widths, "┌", "┬", "┐"), Style: text}}}
		case line == lines[len(lines)-1]:
			return ExtraRows{Below: []ExtraRow{{Text: tableBorder(layout.widths, "└", "┴", "┘"), Style: text}}}
		}
		return ExtraRows{}
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
