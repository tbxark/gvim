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
	if w < 2 {
		return []span{{0, len(runes)}}
	}
	var out []span
	start := 0
	for {
		end, cells := start, 0
		for end < len(runes) {
			cw := ansi.StringWidth(string(runes[end]))
			if cells+cw > w {
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
