package syntax

import "strings"

// TableLineKind classifies a line by its place in a Markdown table.
type TableLineKind uint8

// The table line kinds. A line outside every table is TableNone.
const (
	TableNone TableLineKind = iota
	TableHeader
	TableSeparator
	TableBody
)

// TableAlign is the column alignment the separator row declares.
type TableAlign uint8

// The column alignments. The zero value is Markdown's default, left.
const (
	AlignLeft TableAlign = iota
	AlignRight
	AlignCenter
)

// TableLine is the table classification of one line.
type TableLine struct {
	Kind TableLineKind
	// Table is the index of the table the line belongs to, or -1.
	Table int
	// Cells are the raw cell contents between the pipes, trimmed of blanks.
	Cells []string
	// Starts are the rune columns where each cell's content starts in the line
	// (after its leading blanks), and Ends where it ends (before its trailing
	// blanks). The cells are those of Cells.
	Starts, Ends []int
	// Pipes are the rune columns of the line's pipes, in order.
	Pipes []int
}

// Table is the layout of one table: its line indexes and the alignment of every
// column.
type Table struct {
	Lines []int
	Align []TableAlign
}

// MarkdownTables classifies the lines of text by the Markdown tables the
// tokenizer would read: a header line, the separator row, and the body rows up
// to the next blank line or non-pipe line. The rules are the tokenizer's
// (mdTableRule and the "|" paragraph rule); it is a separate pass so the page
// view can box-draw a table without moving the parser.
func MarkdownTables(lines []string) ([]TableLine, []Table) {
	out := make([]TableLine, len(lines))
	for i := range out {
		out[i] = TableLine{Table: -1}
	}
	var tables []Table
	for i := 0; i+1 < len(lines); {
		header := strings.TrimRight(lines[i], "\r")
		if !strings.Contains(header, "|") || mdQuote.MatchString(header) || mdList.MatchString(header) {
			i++
			continue
		}
		sep := strings.TrimRight(lines[i+1], "\r")
		if !strings.Contains(sep, "|") || !strings.Contains(sep, "-") {
			i++
			continue
		}
		headerRow := splitTableRow(header)
		if isTableSeparatorRow(header, headerRow) {
			i++
			continue
		}
		sepRow := splitTableRow(sep)
		if !isTableSeparatorRow(sep, sepRow) || len(headerRow.cells) != len(sepRow.cells) {
			i++
			continue
		}
		id := len(tables)
		align := tableAlignmentsFromCells(sepRow.cells)
		t := Table{Align: align}
		out[i] = TableLine{Kind: TableHeader, Table: id, Cells: headerRow.cells, Starts: headerRow.starts, Ends: headerRow.ends, Pipes: headerRow.pipes}
		t.Lines = append(t.Lines, i)
		out[i+1] = TableLine{Kind: TableSeparator, Table: id}
		t.Lines = append(t.Lines, i+1)
		j := i + 2
		for ; j < len(lines); j++ {
			line := strings.TrimRight(lines[j], "\r")
			if strings.TrimSpace(line) == "" || !strings.Contains(line, "|") || mdFence.MatchString(line) {
				break
			}
			row := splitTableRow(line)
			if len(row.cells) != len(align) {
				break
			}
			out[j] = TableLine{Kind: TableBody, Table: id, Cells: row.cells, Starts: row.starts, Ends: row.ends, Pipes: row.pipes}
			t.Lines = append(t.Lines, j)
		}
		tables = append(tables, t)
		i = j
	}
	return out, tables
}

// tableRow is one row of a table split at its pipes.
type tableRow struct {
	cells        []string
	starts, ends []int // rune columns of the trimmed content
	pipes        []int // rune columns of the pipes
}

// splitTableRow splits s at its pipes: the outer pipes are optional, the cells
// are trimmed of blanks, and the rune columns of the content and of the pipes
// are kept for the caret and the hit test.
func splitTableRow(s string) tableRow {
	r := []rune(s)
	var row tableRow
	for i := 0; i < len(r); i++ {
		if r[i] == '\\' && i+1 < len(r) && r[i+1] == '|' {
			// An escaped pipe is cell text, not a separator.
			i++
			continue
		}
		if r[i] == '|' {
			row.pipes = append(row.pipes, i)
		}
	}
	if len(r) == 0 {
		return row
	}
	// The cell ranges between pipes (or from the line ends when a pipe is
	// missing), skipping outer spaces/tabs around optional outer pipes.
	firstNB, lastNB := 0, len(r)-1
	for firstNB < len(r) && (r[firstNB] == ' ' || r[firstNB] == '\t') {
		firstNB++
	}
	for lastNB >= firstNB && (r[lastNB] == ' ' || r[lastNB] == '\t') {
		lastNB--
	}
	if firstNB > lastNB {
		return row
	}
	if len(row.pipes) == 1 && row.pipes[0] == firstNB && row.pipes[0] == lastNB {
		return row
	}
	from, last := 0, len(r)
	pipes := row.pipes
	if len(pipes) > 0 && pipes[0] == firstNB {
		from = pipes[0] + 1
		pipes = pipes[1:]
	}
	if len(pipes) > 0 && pipes[len(pipes)-1] == lastNB {
		last = pipes[len(pipes)-1]
		pipes = pipes[:len(pipes)-1]
	}
	if from > last {
		return row
	}
	start := from
	for k := 0; k <= len(pipes); k++ {
		b := last
		if k < len(pipes) {
			b = pipes[k]
		}
		raw := string(r[start:b])
		trimmed := strings.Trim(raw, " \t")
		cellStart, cellEnd := b, b
		if trimmed != "" {
			lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
			trail := len(raw) - len(strings.TrimRight(raw, " \t"))
			cellStart = start + lead
			cellEnd = b - trail
		}
		row.cells = append(row.cells, trimmed)
		row.starts = append(row.starts, cellStart)
		row.ends = append(row.ends, cellEnd)
		start = b + 1
	}
	return row
}

// isTableSeparatorRow reports whether s (already split into row) is the separator row
// under a table header: cells of dashes, optionally with a leading or trailing colon,
// under at least one pipe.
func isTableSeparatorRow(s string, row tableRow) bool {
	if !strings.Contains(s, "-") || !strings.Contains(s, "|") {
		return false
	}
	cells := row.cells
	if t := strings.TrimSpace(s); len(t) != len(strings.Trim(s, " \t")) {
		cells = splitTableRow(t).cells
	}
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		c = strings.TrimPrefix(c, ":")
		c = strings.TrimSuffix(c, ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

// tableAlignmentsFromCells reads the alignments the separator row cells declare.
func tableAlignmentsFromCells(cells []string) []TableAlign {
	out := make([]TableAlign, len(cells))
	for i, c := range cells {
		c = strings.TrimSpace(c)
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		switch {
		case left && right:
			out[i] = AlignCenter
		case right:
			out[i] = AlignRight
		default:
			out[i] = AlignLeft
		}
	}
	return out
}
