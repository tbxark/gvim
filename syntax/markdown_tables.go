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
		sep := strings.TrimRight(lines[i+1], "\r")
		if !isTableHeader(header) || !isTableSeparator(sep) || len(splitTableRow(header).cells) != len(splitTableRow(sep).cells) {
			i++
			continue
		}
		id := len(tables)
		row := splitTableRow(header)
		align := tableAlignments(sep)
		t := Table{Align: align}
		out[i] = TableLine{Kind: TableHeader, Table: id, Cells: row.cells, Starts: row.starts, Ends: row.ends, Pipes: row.pipes}
		t.Lines = append(t.Lines, i)
		out[i+1] = TableLine{Kind: TableSeparator, Table: id}
		t.Lines = append(t.Lines, i+1)
		j := i + 2
		for ; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || !strings.Contains(lines[j], "|") || mdFence.MatchString(lines[j]) {
				break
			}
			row := splitTableRow(strings.TrimRight(lines[j], "\r"))
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
	for i, c := range r {
		if c == '|' {
			row.pipes = append(row.pipes, i)
		}
	}
	if len(r) == 0 {
		return row
	}
	// The cell ranges between pipes (or from the line ends when a pipe is
	// missing).
	from := 0
	if len(row.pipes) > 0 && row.pipes[0] == 0 {
		from = 1
	}
	last := len(r)
	if n := len(row.pipes); n > 0 && row.pipes[n-1] == len(r)-1 {
		last = len(r) - 1
	}
	// Walk the cell boundaries: the pipes inside [from, last), then last.
	bounds := append([]int{}, row.pipes...)
	bounds = append(bounds, last)
	start := from
	for _, b := range bounds {
		if b < start || (b == start && b != last) {
			if b == last {
				break
			}
			if b >= from && b <= last {
				start = b + 1
				continue
			}
			continue
		}
		if b > last {
			break
		}
		raw := string(r[start:b])
		lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
		trail := len(raw) - len(strings.TrimRight(raw, " \t"))
		contentEnd := b - trail
		row.cells = append(row.cells, raw[lead:len(raw)-trail])
		row.starts = append(row.starts, start+lead)
		row.ends = append(row.ends, max(contentEnd, start+lead))
		if b == last {
			break
		}
		start = b + 1
	}
	return row
}

// isTableHeader reports whether s can hold the header cells of a table: it has
// a pipe and is not itself a separator row.
func isTableHeader(s string) bool {
	return strings.Contains(s, "|") && !isTableSeparator(s)
}

// isTableSeparator reports whether s is the separator row under a table header:
// cells of dashes, optionally with a leading or trailing colon, under at least
// one pipe.
func isTableSeparator(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.Contains(t, "-") || !strings.Contains(t, "|") {
		return false
	}
	for _, c := range splitTableRow(t).cells {
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

// tableAlignments reads the alignments the separator row declares.
func tableAlignments(sep string) []TableAlign {
	cells := splitTableRow(sep).cells
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
