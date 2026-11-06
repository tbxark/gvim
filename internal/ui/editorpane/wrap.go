package editorpane

import "github.com/charmbracelet/x/ansi"

// Soft wrap (display only). A buffer line wider than the wrap width is shown on
// several display rows. The editor still moves by buffer lines, so only painting,
// the caret's row and the mouse mapping read these spans.
//
// The rule comes from the legacy screens (the 14x24 open screen and the
// W-14 to W-18 sweep). A row breaks after the last space that fits in the width.
// With no space in the width, the row is cut at the width. The space stays on
// the upper row, and the next row starts at the break:
//
//	W=14 (text area 8, wrap width 6): "hello world" -> "hello " / "world"
//	W=14: "second line" has no space in its first 6 cells -> "second" / " line"
//	W=15 (wrap width 7): "second line" -> "second " / "line"
//
// A wrap width under two cells does not wrap (legacy 14x24 explorer screen).

// span is a run of display runes in an expanded line, as indexes [from, to).
type span struct{ from, to int }

// wrapSpans splits the expanded runes of a buffer line into rows of at most w
// cells using the break rule above. An empty line still has one row. A width
// under two cells does not wrap, so the line stays one row.
func wrapSpans(runes []rune, w int) []span {
	return wrapRows(runes, w, 0, nil)
}

// minContinuation is the least width of a continuation row after its hanging indent.
// A line indented deeper than the row leaves is not given a hanging indent.
const minContinuation = 10

// wrapHanging is wrapSpans with a hanging indent when hang is set. The rows after
// the first start under the first character of the line: they are indent cells
// narrower, and the indent is the number of leading blanks of the line. It is zero
// when the line has no continuation row, or when it would leave a row under
// minContinuation cells. glue may be nil; glue[i] joins rune i to rune i+1, so a
// row does not break between them unless the run is wider than the row.
func wrapHanging(runes []rune, w int, hang bool, glue []bool) (spans []span, indent int) {
	if hang {
		for indent < len(runes) && runes[indent] == ' ' {
			indent++
		}
		if indent+minContinuation > w {
			indent = 0
		}
	}
	spans = wrapRows(runes, w, indent, glue)
	if len(spans) == 1 {
		indent = 0
	}
	return spans, indent
}

// wrapRows splits runes into rows of w cells, the rows after the first being indent
// cells narrower. A rune glued to the next one (see wrapHanging) is not split from it:
// the row breaks before the run, as Fresh keeps a <XX> byte token whole.
func wrapRows(runes []rune, w, indent int, glue []bool) []span {
	if w < 2 {
		return []span{{0, len(runes)}}
	}
	var out []span
	start := 0
	for {
		limit := w
		if start > 0 {
			limit -= indent
		}
		end, cells := start, 0
		for end < len(runes) {
			cw := ansi.StringWidth(string(runes[end]))
			if cells+cw > limit {
				break
			}
			cells += cw
			end++
		}
		if end == len(runes) {
			return append(out, span{start, end})
		}
		if end == start {
			// One rune is wider than the row. It takes a row of its own.
			end = start + 1
		}
		if end > start && end < len(runes) && isGlued(glue, end-1) {
			// The row ends inside a glued run: break before the run, unless the
			// run starts the row, where it is cut as a wide word is.
			first := end - 1
			for first > start && isGlued(glue, first-1) {
				first--
			}
			if first > start {
				end = first
			}
		}
		brk := end
		for j := end - 1; j >= start; j-- {
			if runes[j] == ' ' {
				brk = j + 1
				break
			}
		}
		out = append(out, span{start, brk})
		start = brk
	}
}

// isGlued reports whether rune i is joined to the rune after it.
func isGlued(glue []bool, i int) bool {
	return i >= 0 && i < len(glue) && glue[i]
}

// caretInSpans returns the display row holding expanded rune index idx, and the
// cells from that row's first cell to idx. A caret at the end of the line lands
// on the last row.
func caretInSpans(runes []rune, spans []span, idx int) (row, cells int) {
	for k, sp := range spans {
		if idx < sp.to || k == len(spans)-1 {
			return k, ansi.StringWidth(string(runes[sp.from:min(max(idx, sp.from), sp.to)]))
		}
	}
	return 0, 0
}
