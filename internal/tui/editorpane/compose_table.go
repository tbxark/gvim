package editorpane

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/syntax"
)

// The page view draws a Markdown table as a box: every row's pipes become the
// frame's vertical edges, the cells are padded/aligned to one width per column,
// the source separator row becomes the mid border (├─┼─┤), and the frame sits at
// the page measure. Column widths come from all of the table's rows once per
// text version (stable while scrolling; recomputed on edit), and the separator's
// colon markers choose the alignment. Top and bottom border rows would be
// display rows that no source line owns; the appended-row model belongs to the
// shared page-view extra-row hook (internal/tui/editorpane/rows_extra.go), so the
// top/bottom borders are a documented residual until that hook lands. The
// source separator row is the mid border, which is a source row.
// See docs/markdown.md.

// tableColumnWidths is the layout of one table: the widths of its columns and
// their alignments.
type tableColumnWidths struct {
	widths []int
	align  []syntax.TableAlign
	// wrapped is set when the natural widths did not fit the measure and the
	// columns were shrunk: cells may then need several visual rows.
	wrapped bool
}

// tableModel is the page view's view of the document's tables, computed once per
// text version.
type tableModel struct {
	rows  map[int]syntax.TableLine
	width map[int]tableColumnWidths // by table id
	lines map[int][]int             // the line indexes of each table, by id
	// cells holds, for each table row line, the visible content of every cell
	// already split into the visual rows the wrapped cells need: cells[line][col]
	// is the cell's row segments (each a rune with its kind and source column).
	cells map[int][][][]tableCellRune
}

// tableCellRune is one visible rune of a table cell with its syntax kind and its
// source rune column in the row.
type tableCellRune struct {
	r   rune
	k   syntax.Kind
	src int
}

// tables returns the document's table classification and widths, kept in the
// document's layout memo until its text changes.
func (f frame) tables() *tableModel {
	compute := func() any {
		lines := make([]string, f.doc.LineCount())
		for i := range lines {
			lines[i] = f.doc.Line(i)
		}
		class, tables := syntax.MarkdownTables(lines)
		m := &tableModel{rows: map[int]syntax.TableLine{}, width: map[int]tableColumnWidths{},
			lines: map[int][]int{}, cells: map[int][][][]tableCellRune{}}
		for i, tl := range class {
			if tl.Kind != syntax.TableNone {
				m.rows[i] = tl
			}
		}
		for id, t := range tables {
			layout := f.tableWidths(t, class)
			m.width[id] = layout
			m.lines[id] = append([]int(nil), t.Lines...)
			for _, li := range t.Lines {
				if class[li].Kind == syntax.TableHeader || class[li].Kind == syntax.TableBody {
					m.cells[li] = f.tableCellSegments(li, class[li], layout)
				}
			}
		}
		return m
	}
	if lm, ok := f.doc.(LayoutMemo); ok && !isLarge(f.doc) {
		return lm.MemoValue(composeTableKey{measure: f.pageMeasure}, compute).(*tableModel)
	}
	return compute().(*tableModel)
}

// composeTableKey is the layout memo key of the table model: the model's widths
// depend on the page measure, so the two panes of the dual view (different
// measures) cannot share one entry. The document memo drops it when the text
// changes.
type composeTableKey struct{ measure int }

// tableWidths computes the column widths of table t from every row of it, once
// per text version: the natural width is the widest visible cell, and the budget
// (the measure less the pipes and the cell padding) shrinks the columns
// proportionally when the table is too wide, with a floor of three cells each,
// as Fresh distributes them.
func (f frame) tableWidths(t syntax.Table, class []syntax.TableLine) tableColumnWidths {
	n := len(t.Align)
	out := tableColumnWidths{align: t.Align, widths: make([]int, n)}
	if n == 0 {
		return out
	}
	natural := make([]int, n)
	for _, li := range t.Lines {
		line := class[li]
		if line.Kind == syntax.TableSeparator {
			continue
		}
		for c := 0; c < n && c < len(line.Cells); c++ {
			// The widths are sized to the raw cell text, not the composed one: the
			// caret's row shows the raw text, so the frame must already be wide
			// enough for it (Fresh sizes the columns the same way).
			if w := ansi.StringWidth(strings.TrimSpace(line.Cells[c])); w > natural[c] {
				natural[c] = w
			}
		}
	}
	budget := f.pageMeasure - 1 - 3*n
	sum := 0
	for _, w := range natural {
		sum += w
	}
	if sum <= budget {
		copy(out.widths, natural)
		for i := range out.widths {
			out.widths[i] = max(out.widths[i], 1)
		}
		return out
	}
	// The table is wider than the page: its cells may need several visual rows.
	out.wrapped = true
	const minCol = 3
	if budget <= minCol*n {
		for i := range out.widths {
			out.widths[i] = minCol
		}
		return out
	}
	extra := budget - minCol*n
	excess := 0
	for _, w := range natural {
		excess += max(w-minCol, 0)
	}
	left := extra
	for i := range out.widths {
		if excess == 0 {
			out.widths[i] = minCol
			continue
		}
		share := extra * max(natural[i]-minCol, 0) / excess
		out.widths[i] = minCol + share
		left -= share
	}
	// Give the rounding remainder to the widest column, so the budget is used.
	widest := 0
	for i := range natural {
		if natural[i] > natural[widest] {
			widest = i
		}
	}
	out.widths[widest] += left
	return out
}

// hiddenRunes marks the runes of a cell that its inline delimiters hide: the
// markers the page view conceals (see MarkdownInlineMarkers).
func hiddenRunes(cell string) []bool {
	runes := []rune(cell)
	marks := syntax.MarkdownInlineMarkers(cell)
	if len(marks) == 0 {
		return nil
	}
	hidden := make([]bool, len(runes))
	for _, m := range marks {
		for j := max(m.Start, 0); j < m.End && j < len(hidden); j++ {
			hidden[j] = true
		}
	}
	return hidden
}

// tableCellSegments builds the visible content of every cell of one table row,
// split into the visual rows the wrapped cells need. Hidden inline delimiters are
// dropped, an escaped pipe shows as the pipe itself, and each visible rune keeps
// its syntax kind and its source column so the primary row maps exactly.
func (f frame) tableCellSegments(li int, tl syntax.TableLine, layout tableColumnWidths) [][][]tableCellRune {
	raw := []rune(f.doc.Line(li))
	kinds := kindsPerRune(f.doc.Line(li), f.doc.LineSpans(li))
	out := make([][][]tableCellRune, len(layout.widths))
	for c := range layout.widths {
		if c >= len(tl.Cells) || c >= len(tl.Starts) || c >= len(tl.Ends) {
			out[c] = [][]tableCellRune{{}}
			continue
		}
		cell := tl.Cells[c]
		hidden := hiddenRunes(cell)
		start, end := tl.Starts[c], min(tl.Ends[c], len(raw))
		var content []tableCellRune
		for k := max(start, 0); k < end; k++ {
			if rel := k - start; rel >= 0 && rel < len(hidden) && hidden[rel] {
				continue
			}
			if raw[k] == '\\' && k+1 < end && raw[k+1] == '|' {
				content = append(content, tableCellRune{'|', kindAt(kinds, k+1), k + 1})
				k++
				continue
			}
			content = append(content, tableCellRune{raw[k], kindAt(kinds, k), k})
		}
		if layout.wrapped {
			out[c] = wrapCellSegments(content, max(layout.widths[c], 1))
		} else {
			out[c] = [][]tableCellRune{content}
		}
	}
	return out
}

// wrapCellSegments wraps the visible runes of a cell into rows of at most width
// cells, breaking after the last space that fits (the space ends the row) and
// cutting a word that has no space to break at.
func wrapCellSegments(content []tableCellRune, width int) [][]tableCellRune {
	if len(content) == 0 {
		return [][]tableCellRune{{}}
	}
	var out [][]tableCellRune
	start := 0
	for start < len(content) {
		used := 0
		end := start
		lastSpace := -1
		for end < len(content) {
			w := ansi.StringWidth(string(content[end].r))
			if used+w > width && end > start {
				// The space just met ends the row; a word boundary behind it takes
				// the break; a word longer than the row is cut.
				if content[end].r == ' ' {
					out = append(out, content[start:end])
					start = end + 1
				} else if lastSpace > start {
					out = append(out, content[start:lastSpace])
					start = lastSpace + 1
				} else {
					out = append(out, content[start:end])
					start = end
				}
				for start < len(content) && content[start].r == ' ' {
					start++
				}
				goto nextRow
			}
			used += w
			if content[end].r == ' ' {
				lastSpace = end
			}
			end++
		}
		out = append(out, content[start:])
		break
	nextRow:
	}
	return out
}

// tableContinuationRow builds visual row k (k >= 1) of a wrapped table row: the
// same frame as the primary row, with every cell's k-th segment and blanks where
// the cell has no such segment.
func tableContinuationRow(widths []int, cellsAtLine [][][]tableCellRune, k int) string {
	var b []rune
	b = append(b, '│')
	for c, w := range widths {
		b = append(b, ' ')
		n := 0
		if c < len(cellsAtLine) && k < len(cellsAtLine[c]) {
			for _, cr := range cellsAtLine[c][k] {
				b = append(b, cr.r)
				n += ansi.StringWidth(string(cr.r))
			}
		}
		for ; n < w; n++ {
			b = append(b, ' ')
		}
		b = append(b, ' ', '│')
	}
	return string(b)
}

// composeTableLine composes one line of a table. reveal shows the cell contents
// raw (the caret's row) while keeping the frame's padding, so the rows above and
// below do not jump.
func (f frame) composeTableLine(i int, tl syntax.TableLine, reveal bool) composedLine {
	out := composedLine{}
	raw := []rune(f.doc.Line(i))
	kinds := kindsPerRune(f.doc.Line(i), f.doc.LineSpans(i))
	layout := f.tables().width[tl.Table]
	add := func(r rune, k syntax.Kind, src int) {
		out.runes = append(out.runes, r)
		out.kinds = append(out.kinds, k)
		out.src = append(out.src, src)
	}
	if tl.Kind == syntax.TableSeparator {
		add('├', syntax.KindPlain, 0)
		for c, w := range layout.widths {
			for j := 0; j < w+2; j++ {
				add('─', syntax.KindPlain, 0)
			}
			if c == len(layout.widths)-1 {
				add('┤', syntax.KindPlain, 0)
			} else {
				add('┼', syntax.KindPlain, 0)
			}
		}
		finishMaps(&out, len(raw))
		return out
	}
	// The source columns of the pipes and of the cells' content.
	pipeAt := func(c int) int {
		if c < len(tl.Pipes) {
			return tl.Pipes[c]
		}
		return len(raw)
	}
	nextPipe := func(c int) int { return pipeAt(c + 1) }
	add('│', syntax.KindPlain, pipeAt(0))
	widths := layout.widths
	cellsAtLine := f.tables().cells[i]
	for c := 0; c < len(widths); c++ {
		add(' ', syntax.KindPlain, pipeAt(c))
		var content []tableCellRune
		start := 0
		if c < len(tl.Cells) && c < len(tl.Starts) {
			start = tl.Starts[c]
		}
		switch {
		case reveal && c < len(tl.Cells) && c < len(tl.Ends):
			// The caret's row shows the raw cell text: markers and escapes as the
			// source has them.
			for k := start; k < min(tl.Ends[c], len(raw)); k++ {
				content = append(content, tableCellRune{raw[k], kindAt(kinds, k), k})
			}
		case c < len(cellsAtLine) && len(cellsAtLine[c]) > 0:
			content = cellsAtLine[c][0]
		}
		contentWidth := 0
		for _, cr := range content {
			contentWidth += ansi.StringWidth(string(cr.r))
		}
		pad := max(widths[c]-contentWidth, 0)
		align := syntax.AlignLeft
		if c < len(layout.align) {
			align = layout.align[c]
		}
		before, after := 0, pad
		switch align {
		case syntax.AlignRight:
			before, after = pad, 0
		case syntax.AlignCenter:
			before, after = pad/2, pad-pad/2
		}
		for j := 0; j < before; j++ {
			add(' ', syntax.KindPlain, start+j)
		}
		for _, cr := range content {
			add(cr.r, cr.k, cr.src)
		}
		for j := 0; j < after; j++ {
			add(' ', syntax.KindPlain, min(start+len(content)+j, len(raw)))
		}
		add(' ', syntax.KindPlain, pipeAt(c))
		add('│', syntax.KindPlain, nextPipe(c))
	}
	finishMaps(&out, len(raw))
	return out
}
