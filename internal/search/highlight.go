package search

import (
	"sort"

	"github.com/tbxark/gvim/internal/buffer"
)

// Span is the part of a match that lies on one line: the rune columns
// [Start, End) on Line. A match that crosses line breaks gives one Span per
// line it touches.
type Span struct {
	Line, Start, End int
}

// Spans returns the highlight spans of matches on lines first through last of
// b, both inclusive, in document order. Lines outside b are clipped, and a
// match that starts or ends on another line contributes only its part on the
// visible lines. A span on a line the match runs past ends at the end of that
// line. Zero-width spans are dropped. matches must come from Find on the
// current text of b, in document order. The result is nil when no span is
// visible.
func Spans(b *buffer.Buffer, matches []Match, first, last int) []Span {
	first = max(first, 0)
	last = min(last, b.Lines()-1)
	if first > last {
		return nil
	}
	// Matches do not overlap and are in document order, so their ends are
	// in order too, and the first one that ends on or after first is the
	// first one that can be visible.
	i := sort.Search(len(matches), func(i int) bool {
		return matches[i].End.Line >= first
	})
	var out []Span
	for ; i < len(matches) && matches[i].Start.Line <= last; i++ {
		m := matches[i]
		for line := max(m.Start.Line, first); line <= min(m.End.Line, last); line++ {
			start, end := 0, b.LineLen(line)
			if line == m.Start.Line {
				start = m.Start.Col
			}
			if line == m.End.Line {
				end = m.End.Col
			}
			if start < end {
				out = append(out, Span{Line: line, Start: start, End: end})
			}
		}
	}
	return out
}
