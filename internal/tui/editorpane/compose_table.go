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
}

// tableModel is the page view's view of the document's tables, computed once per
// text version.
type tableModel struct {
	rows  map[int]syntax.TableLine
	width map[int]tableColumnWidths // by table id
	lines map[int][]int             // the line indexes of each table, by id
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
			lines: map[int][]int{}}
		for i, tl := range class {
			if tl.Kind != syntax.TableNone {
				m.rows[i] = tl
			}
		}
		for id, t := range tables {
			m.width[id] = f.tableWidths(t, class)
			m.lines[id] = append([]int(nil), t.Lines...)
		}
		return m
	}
	if lm, ok := f.doc.(LayoutMemo); ok && !isLarge(f.doc) {
		return lm.MemoValue(composeTableKey{}, compute).(*tableModel)
	}
	return compute().(*tableModel)
}

type composeTableKey struct{}

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
	for c := 0; c < len(widths); c++ {
		add(' ', syntax.KindPlain, pipeAt(c))
		cell := ""
		var start, end int
		if c < len(tl.Cells) {
			cell = tl.Cells[c]
			start, end = tl.Starts[c], tl.Ends[c]
		}
		cellRunes := []rune(cell)
		hidden := hiddenRunes(cell)
		if reveal {
			hidden = nil
		}
		type run struct {
			r   rune
			k   syntax.Kind
			src int
		}
		var content []run
		contentWidth := 0
		for k, r := range cellRunes {
			if k < len(hidden) && hidden[k] {
				continue
			}
			content = append(content, run{r, kindAt(kinds, min(start+k, len(kinds)-1)), min(start+k, len(raw))})
			contentWidth += ansi.StringWidth(string(r))
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
			add(' ', syntax.KindPlain, start)
		}
		for _, rn := range content {
			add(rn.r, rn.k, rn.src)
		}
		for j := 0; j < after; j++ {
			add(' ', syntax.KindPlain, min(start+len(cellRunes), len(raw)))
		}
		_ = end
		add(' ', syntax.KindPlain, pipeAt(c))
		add('│', syntax.KindPlain, nextPipe(c))
	}
	finishMaps(&out, len(raw))
	return out
}
