package terminal

import "github.com/charmbracelet/x/ansi"

// ReflowLines re-wraps stored transcript rows from fromWidth to toWidth cells.
//
// It is a TEXT re-wrap, not a semantic reflow: the transcript stores display rows,
// not the soft/hard newline structure that produced them, so each stored row is
// treated as a hard newline and is wrapped on its own. A row that was a
// soft-wrapped continuation is therefore not rejoined with the row before it;
// running text re-wraps approximately (docs/terminal.md).
//
// What is preserved: each cell's style, wide glyphs (a double-width cell and its
// continuation cell stay together and are never split), and the row boundaries.
// Trailing blank cells are dropped from each re-wrapped row, as the stored rows
// already trim them. A non-positive toWidth returns the lines unchanged; a
// zero/unknown fromWidth is treated as the line's own width (no-op for that row).
func ReflowLines(lines []Line, fromWidth, toWidth int) []Line {
	if toWidth <= 0 || len(lines) == 0 {
		return lines
	}
	out := make([]Line, 0, len(lines))
	for _, line := range lines {
		cells := line.Cells
		if fromWidth > 0 && len(cells) > fromWidth {
			cells = cells[:fromWidth]
		}
		// Trim trailing spaces: they are padding, not text. A continuation half
		// (Rune 0) is not padding: it belongs to the wide rune before it.
		end := len(cells)
		for end > 0 && cells[end-1].Rune == ' ' {
			end--
		}
		if end == 0 {
			out = append(out, Line{})
			continue
		}
		var cur []Cell
		width := 0
		for i := 0; i < end; {
			c := cells[i]
			if c.Rune == 0 {
				// A continuation half without its wide rune: drop it rather than
				// draw a lone half.
				i++
				continue
			}
			w := ansi.StringWidth(string(c.Rune))
			if w < 1 {
				w = 1
			}
			unit := []Cell{c}
			advance := 1
			if w > 1 && i+1 < end && cells[i+1].Rune == 0 {
				unit = append(unit, cells[i+1])
				advance = 2
			}
			if width > 0 && width+w > toWidth {
				out = append(out, Line{Cells: cur})
				cur = nil
				width = 0
			}
			cur = append(cur, unit...)
			width += w
			i += advance
		}
		out = append(out, Line{Cells: cur})
	}
	return out
}
