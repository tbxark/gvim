package search

import (
	"regexp"
	"regexp/syntax"
	"slices"

	"github.com/tbxark/gvim/buffer"
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
// is searched in: the window with the margin on each side, clamped to the line. first and end
// are the rune column bounds of the returned text on line i.
func (sp hugeSpan) text(s *buffer.Snapshot, i, runes, lo, hi int) (first, end int, text string) {
	first = max(lo-sp.margin, 0)
	end = min(hi+sp.margin, runes)
	return first, end, s.LineRunes(i, first, end-first)
}

// hugeMatches returns the matches of re in text, which holds the runes [first, end) of line i
// (a line whose length is `runes`), that start in columns [lo, hi), with the positions of the
// whole line. Line/text anchors (^, \A when first > 0; $, \z when end < runes) are disabled so
// artificial window edges never produce false anchor matches. A match that starts in another
// window is left to that window, so no match is returned twice.
func hugeMatches(i, first, end, runes int, text string, re *regexp.Regexp, q Query, lo, hi int) []Match {
	if q.Regex {
		re = maskAnchors(re, first > 0, end < runes)
	}
	d := newWindow(i, text)
	cur := newOffsetPos(d.text, buffer.Pos{Line: d.first})
	var out []Match
	for _, loc := range d.locate(re, q) {
		start := cur.pos(loc[0])
		endPos := cur.pos(loc[1])
		start.Col += first
		endPos.Col += first
		if start.Col >= lo && start.Col < hi {
			out = append(out, Match{Start: start, End: endPos})
		}
	}
	return out
}

func maskAnchors(re *regexp.Regexp, stripStart, stripEnd bool) *regexp.Regexp {
	if !stripStart && !stripEnd {
		return re
	}
	ast, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return re
	}
	if !stripAnchorOps(ast, stripStart, stripEnd) {
		return re
	}
	masked, err := regexp.Compile(ast.String())
	if err != nil {
		return re
	}
	return masked
}

func stripAnchorOps(re *syntax.Regexp, stripStart, stripEnd bool) bool {
	changed := false
	switch re.Op {
	case syntax.OpBeginLine, syntax.OpBeginText:
		if stripStart {
			re.Op = syntax.OpNoMatch
			re.Rune = nil
			re.Sub = nil
			return true
		}
	case syntax.OpEndLine, syntax.OpEndText:
		if stripEnd {
			re.Op = syntax.OpNoMatch
			re.Rune = nil
			re.Sub = nil
			return true
		}
	}
	for _, sub := range re.Sub {
		if stripAnchorOps(sub, stripStart, stripEnd) {
			changed = true
		}
	}
	return changed
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
		first, end, text := n.huge.text(n.s, line, runes, col, hi)
		if err := n.account(text); err != nil {
			return Match{}, false, false, err
		}
		for _, m := range hugeMatches(line, first, end, runes, text, n.re, n.q, col, hi) {
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
		first, end, text := n.huge.text(n.s, line, runes, lo, hi)
		if err := n.account(text); err != nil {
			return Match{}, false, false, err
		}
		ms := hugeMatches(line, first, end, runes, text, n.re, n.q, lo, hi)
		for _, m := range slices.Backward(ms) {
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
