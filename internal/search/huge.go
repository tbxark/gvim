package search

import (
	"regexp"

	"github.com/tbxark/gvim/internal/buffer"
)

// hugeSpan sizes the windows a huge line is searched in: window runes of it at a time, and
// margin runes of text read on each side of a window, besides its own runes. A match that
// starts in a window is found whole when it is no longer than margin, and a word boundary at
// the window's edge is seen as it is.
type hugeSpan struct {
	window, margin int
}

// defaultHuge is the span of every search: 64 Ki runes a window, with 4 Ki runes of margin.
var defaultHuge = hugeSpan{window: 1 << 16, margin: 1 << 12}

// text returns the text that the window of runes [lo, hi) of line i, which has runes runes,
// is searched in: the window with the margin on each side, clamped to the line. first is the
// column of the text's first rune.
func (sp hugeSpan) text(s *buffer.Snapshot, i, runes, lo, hi int) (first int, text string) {
	first = max(lo-sp.margin, 0)
	end := min(hi+sp.margin, runes)
	return first, s.LineRunes(i, first, end-first)
}

// hugeMatches returns the matches of re in text, which holds the runes of line i from column
// first on, that start in columns [lo, hi), with the positions of the whole line. A match that
// starts in another window is left to that window, so no match is returned twice.
func hugeMatches(i, first int, text string, re *regexp.Regexp, q Query, lo, hi int) []Match {
	d := newWindow(i, text)
	cur := posCursor{d: d}
	var out []Match
	for _, loc := range d.locate(re, q) {
		start := cur.pos(loc[0])
		end := cur.pos(loc[1])
		start.Col += first
		end.Col += first
		if start.Col >= lo && start.Col < hi {
			out = append(out, Match{Start: start, End: end})
		}
	}
	return out
}

// forwardHuge searches the huge line line, which has runes runes, window by window from the
// column the phase starts it at, for the first match on the travel side of from (see forward).
// The first line of a phase starts at its col, and the line of from, before the wrap, starts at
// from's column. done is false when the budget ran out: n.stop and n.stopCol then give the
// window the search goes on from.
func (n *nextSearch) forwardHuge(line, runes int, p phase) (Match, bool, bool, error) {
	col := 0
	if line == p.lo {
		col = p.col
	}
	if !p.wrapped && line == n.from.Line {
		col = max(col, min(n.from.Col, runes))
	}
	for col < runes {
		if p.wrapped && line == n.from.Line && col >= n.from.Col {
			break // what is left of the line is at or after from
		}
		if n.spent >= n.budget {
			n.stop, n.stopCol = line, col
			return Match{}, false, false, nil
		}
		hi := min(col+n.huge.window, runes)
		first, text := n.huge.text(n.s, line, runes, col, hi)
		if err := n.account(text); err != nil {
			return Match{}, false, false, err
		}
		for _, m := range hugeMatches(line, first, text, n.re, n.q, col, hi) {
			if m.Start.Less(n.from) != p.wrapped {
				if p.wrapped {
					// Every later match starts at or after from too.
					return Match{}, false, true, nil
				}
				continue
			}
			return m, true, true, nil
		}
		col = hi
	}
	return Match{}, false, true, nil
}

// backwardHuge searches the huge line line, which has runes runes, window by window from the
// top down, for the last match on the travel side of from (see backward). colHi, when set, is
// the column the line is searched up to, where an earlier search stopped. done is false when
// the budget ran out: n.stop and n.stopCol then give the line after this one and the column the
// line is searched up to.
func (n *nextSearch) backwardHuge(line, runes int, p phase, colHi int) (Match, bool, bool, error) {
	hi, floor := runes, 0
	if colHi > 0 {
		hi = min(colHi, runes)
	} else if line == n.from.Line && !p.wrapped {
		hi = min(n.from.Col, runes)
	}
	if line == n.from.Line && p.wrapped {
		floor = min(n.from.Col, runes)
	}
	for hi > floor {
		if n.spent >= n.budget {
			n.stop, n.stopCol = line+1, hi
			return Match{}, false, false, nil
		}
		lo := max(hi-n.huge.window, floor)
		first, text := n.huge.text(n.s, line, runes, lo, hi)
		if err := n.account(text); err != nil {
			return Match{}, false, false, err
		}
		ms := hugeMatches(line, first, text, n.re, n.q, lo, hi)
		for i := len(ms) - 1; i >= 0; i-- {
			m := ms[i]
			if m.Start.Less(n.from) == p.wrapped {
				if p.wrapped {
					// Every earlier match starts before from too.
					return Match{}, false, true, nil
				}
				continue
			}
			return m, true, true, nil
		}
		hi = lo
	}
	return Match{}, false, true, nil
}
