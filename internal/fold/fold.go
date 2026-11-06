package fold

import (
	"strings"

	"github.com/tbxark/gvim/internal/display"
)

// Range is a fold range over zero-based line indexes. Start is the line that
// opens the range and End is the last line in it, both inclusive.
type Range struct {
	Start, End int
}

// Ranges returns the indentation fold ranges of lines, ordered by Start. A
// non-blank line opens a range when the next non-blank line is indented more
// than it. The range runs to the last non-blank line before the first later
// non-blank line that is not indented more than the opener. Blank lines inside
// a range belong to it, and blank lines never open one. Indentation is measured
// with display.IndentWidth.
func Ranges(lines []string, tabWidth int) []Range {
	tabWidth = max(tabWidth, 1)
	indent := make([]int, len(lines))
	var nonBlank []int
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent[i] = display.IndentWidth(line, tabWidth)
		nonBlank = append(nonBlank, i)
	}

	var ranges []Range
	// open holds the indexes into ranges of the ranges not yet closed, from the
	// outermost to the innermost. Their indents increase along the stack.
	var open []int
	for p, i := range nonBlank {
		for len(open) > 0 && indent[i] <= indent[ranges[open[len(open)-1]].Start] {
			ranges[open[len(open)-1]].End = nonBlank[p-1]
			open = open[:len(open)-1]
		}
		if p+1 < len(nonBlank) && indent[nonBlank[p+1]] > indent[i] {
			ranges = append(ranges, Range{Start: i, End: i})
			open = append(open, len(ranges)-1)
		}
	}
	for _, r := range open {
		ranges[r].End = nonBlank[len(nonBlank)-1]
	}
	return ranges
}

// Visible returns n flags, one per line, that are true for each line hidden by
// a folded range. A range whose Start is a key of folded with a true value hides
// the lines after Start up to End. The Start line itself is not hidden.
func Visible(n int, ranges []Range, folded map[int]bool) []bool {
	hidden := make([]bool, n)
	for _, r := range ranges {
		if !folded[r.Start] {
			continue
		}
		for i := max(r.Start+1, 0); i <= r.End && i < n; i++ {
			hidden[i] = true
		}
	}
	return hidden
}
