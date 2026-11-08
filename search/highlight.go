package search

import (
	"slices"
	"sort"
)

// LineSource is the text that Spans and Highlights cut matches from: the line
// count and the rune length of each line. *buffer.Buffer satisfies it, and so
// can any view of a document that has the same lines.
type LineSource interface {
	Lines() int
	LineLen(i int) int
}

// Span is the part of a match that lies on one line: the rune columns
// [Start, End) on Line. A match that crosses line breaks gives one Span per
// line it touches.
type Span struct {
	Line, Start, End int
}

// Highlight is a Span to paint on a line. Current is set on the span of the
// match the caret has selected, so it can be painted in the current-match
// colors and the other matches in the match colors.
type Highlight struct {
	Span
	Current bool
}

// Spans returns the highlight spans of matches on lines first through last of
// src, both inclusive, in document order. Lines outside src are clipped, and a
// match that starts or ends on another line contributes only its part on the
// visible lines. A span on a line the match runs past ends at the end of that
// line. Zero-width spans are dropped. matches must come from Find on the
// current text of src, in document order. The result is nil when no span is
// visible.
func Spans(src LineSource, matches []Match, first, last int) []Span {
	first = max(first, 0)
	last = min(last, src.Lines()-1)
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
			start, end := 0, src.LineLen(line)
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

// Highlights returns the highlight spans of matches on line, in document order.
// The span of current, when it is not nil, is marked Current. current is the
// match the caret has selected, if any. It need not be in matches. The result is
// nil when line has no span. matches must be in document order, as for Spans.
func Highlights(src LineSource, matches []Match, line int, current *Match) []Highlight {
	spans := Spans(src, matches, line, line)
	if len(spans) == 0 {
		return nil
	}
	var cur []Span
	if current != nil {
		cur = Spans(src, []Match{*current}, line, line)
	}
	out := make([]Highlight, len(spans))
	for i, s := range spans {
		out[i] = Highlight{Span: s, Current: slices.Contains(cur, s)}
	}
	return out
}
