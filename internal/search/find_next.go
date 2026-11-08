package search

import (
	"math"
	"regexp"

	"github.com/tbxark/gvim/internal/buffer"
)

// Dir is the direction of a FindNext search.
type Dir int

const (
	// Forward looks for the first match that starts at or after the position.
	Forward Dir = iota
	// Backward looks for the last match that starts before the position.
	Backward
)

const (
	// DefaultNextBudget is the number of bytes of text one FindNext call reads
	// when its maxBytes is not positive.
	DefaultNextBudget int64 = 64 << 20
	// nextWindow is the size of one window of whole lines that FindNext
	// searches at a time.
	nextWindow = 1 << 20
)

// FindNext finds the match of q to go to from the position from in snapshot s,
// without building a list of all matches. This is what F3 and Shift+F3 do on a
// large buffer.
//
// With Forward it returns the first match that starts at or after from, as
// Next does; pass the end of the current selection. With Backward it returns
// the last match that starts before from, as Prev does; pass the start of the
// selection. Either way the search wraps around the document and ends at from,
// so a match is found anywhere in the document if there is one. The caller can
// tell a wrapped result by comparing m.Start with from.
//
// The text is read in windows of about 1 MiB of whole lines and the search
// stops at the first hit. A huge line, longer than the line cap of a large buffer,
// is read in windows of runes of its own, so a match anywhere in it is found. A
// multi-line match that crosses a window boundary is not found, and a match that
// overlaps an earlier one in its window may be missed, as in Find. maxBytes limits the text read by one call; it defaults to
// DefaultNextBudget when not positive. scanned is false when that budget ran
// out (or a read of the snapshot failed) before the answer was known, so
// found == false then means "not found yet" and not "no match". scanned is
// true when found is true, and when the whole document was searched. An empty
// query finds nothing. A pattern that does not compile, or a failed read of the
// snapshot, returns an error.
func FindNext(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64) (m Match, found bool, scanned bool, err error) {
	m, found, scanned, _, err = FindNextAt(s, q, from, dir, maxBytes, Cursor{})
	return m, found, scanned, err
}

// Cursor is how far a FindNext that ran out of its budget got. Pass it back to
// FindNextAt, with the same snapshot, query, position and direction, and the
// search goes on from there, so a document longer than one budget is searched in
// several calls. The zero Cursor starts at the beginning.
type Cursor struct {
	started bool
	phase   int // 0 for the far side of from, 1 after the wrap
	pos     int // the first line (forward) or the end line (backward) left to search
	// col is where a huge line stopped: the first column left to search of line pos
	// (forward), or the column the line is searched up to (backward). Zero when the
	// search stopped between lines.
	col int
}

// FindNextAt is FindNext that starts at the cursor at. next is where a search
// that ended with scanned == false stopped; it is the zero Cursor otherwise.
func FindNextAt(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64, at Cursor) (m Match, found bool, scanned bool, next Cursor, err error) {
	return findNextSpan(s, q, from, dir, maxBytes, nextWindow, defaultHuge, at)
}

// findNext is FindNext with the window size as a parameter, so tests can make
// every window boundary matter.
func findNext(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64, window int) (Match, bool, bool, error) {
	m, found, scanned, _, err := findNextSpan(s, q, from, dir, maxBytes, window, defaultHuge, Cursor{})
	return m, found, scanned, err
}

// findNextAt is FindNextAt with the window size as a parameter.
func findNextAt(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64, window int, at Cursor) (Match, bool, bool, Cursor, error) {
	return findNextSpan(s, q, from, dir, maxBytes, window, defaultHuge, at)
}

// findNextSpan is FindNextAt with the window size and the windows of a huge line as
// parameters, so that tests can make every window boundary matter.
func findNextSpan(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64, window int, huge hugeSpan, at Cursor) (Match, bool, bool, Cursor, error) {
	if q.Text == "" {
		return Match{}, false, true, Cursor{}, nil
	}
	re, err := compile(q)
	if err != nil {
		return Match{}, false, false, Cursor{}, err
	}
	if maxBytes <= 0 {
		maxBytes = DefaultNextBudget
	}
	total := s.Lines()
	switch {
	case from.Line >= total:
		from = buffer.Pos{Line: total - 1, Col: math.MaxInt}
	case from.Line < 0:
		from = buffer.Pos{}
	}
	huge.window, huge.margin = max(huge.window, 1), max(huge.margin, 0)
	n := &nextSearch{s: s, re: re, q: q, from: from, dir: dir, window: max(window, 1), budget: maxBytes, total: total, huge: huge}
	// The document is searched in two phases: first the part on the far side of
	// from in the direction of travel, then, after wrapping, the rest. Lines on
	// from's own line are in both; the first phase keeps the matches on the
	// travel side of from and the second those on the other side.
	if dir == Backward {
		return n.run(at,
			phase{lo: 0, hi: from.Line + 1},
			phase{lo: from.Line, hi: total, wrapped: true},
			n.backward)
	}
	return n.run(at,
		phase{lo: from.Line, hi: total},
		phase{lo: 0, hi: from.Line + 1, wrapped: true},
		n.forward)
}

// nextSearch is the state of one FindNext call.
type nextSearch struct {
	s      *buffer.Snapshot
	re     *regexp.Regexp
	q      Query
	from   buffer.Pos
	window int
	budget int64
	spent  int64
	total  int // lines in the snapshot
	dir    Dir
	stop   int // the line a step that ran out of budget stopped at
	// stopCol is the column of a huge line a step stopped at (see Cursor.col).
	stopCol int
	huge    hugeSpan
}

// phase is a range of lines [lo, hi) to search. wrapped marks the second
// phase, which is after the wrap around the document.
type phase struct {
	lo, hi  int
	wrapped bool
	resumed bool // the phase goes on from a Cursor
	// col is the column the first line lo goes on from, and colHi the column line hi-1 is
	// searched up to, when the phase goes on inside a huge line. Zero means the whole line.
	col, colHi int
}

// run searches the two phases in order with step, which searches one phase.
// step returns the match, whether it found one, and whether it searched the
// whole phase (false when the budget ran out, with n.stop the line it stopped
// at); a read error comes as err. A started cursor skips the phases before its
// own and narrows its phase to the part left. step moves up through the lines of
// a phase when its lo is the line to start at, and down when its hi is the end.
func (n *nextSearch) run(at Cursor, a, b phase, step func(phase) (Match, bool, bool, error)) (Match, bool, bool, Cursor, error) {
	phases := [2]phase{a, b}
	for i := at.phase; i < len(phases); i++ {
		p := phases[i]
		if at.started && i == at.phase {
			p.resumed = true
			if n.dir == Backward {
				p.hi, p.colHi = at.pos, at.col
			} else {
				p.lo, p.col = at.pos, at.col
			}
		}
		m, found, complete, err := step(p)
		if err != nil || found {
			return m, found, found && err == nil, Cursor{}, err
		}
		if !complete {
			return Match{}, false, false, Cursor{started: true, phase: i, pos: n.stop, col: n.stopCol}, nil
		}
	}
	return Match{}, false, true, Cursor{}, nil
}

// account charges the text of one window to the budget. It returns the error
// of the snapshot when a read of it failed: the lines that could not be read
// came back empty, so no answer drawn from them counts.
func (n *nextSearch) account(text string) error {
	n.spent += int64(len(text))
	return n.s.Err()
}

// locate returns the matches in the window, in order, with buffer positions.
func (n *nextSearch) locate(first int, text string) []hit {
	d := newWindow(first, text)
	locs := d.locate(n.re, n.q)
	out := make([]hit, len(locs))
	for i, loc := range locs {
		out[i] = d.hit(loc)
	}
	return out
}

// forward searches the lines of p from lo upwards for the first match on the
// travel side of from: starting at or after it, or before it when wrapped. The
// last result is false when the budget ran out before p was done.
func (n *nextSearch) forward(p phase) (Match, bool, bool, error) {
	for line := p.lo; line < p.hi; {
		if n.spent >= n.budget {
			n.stop, n.stopCol = line, 0
			return Match{}, false, false, nil
		}
		if runes, long := n.s.LongLine(line); long {
			m, found, done, err := n.forwardHuge(line, runes, p)
			if err != nil || found || !done {
				return m, found, done, err
			}
			line++
			continue
		}
		first, count, text := n.s.Chunk(line, n.window)
		if count == 0 {
			break
		}
		if err := n.account(text); err != nil {
			return Match{}, false, false, err
		}
		for _, h := range n.locate(first, text) {
			if h.Start.Less(n.from) != p.wrapped {
				if p.wrapped {
					break // every later match starts at or after from too
				}
				continue
			}
			return h.Match, true, true, nil
		}
		line = first + count
	}
	return Match{}, false, true, nil
}

// backward searches the lines of p from hi downwards for the last match on the
// travel side of from: starting before it, or at or after it when wrapped.
func (n *nextSearch) backward(p phase) (Match, bool, bool, error) {
	for to := p.hi; to > p.lo; {
		if n.spent >= n.budget {
			n.stop, n.stopCol = to, 0
			return Match{}, false, false, nil
		}
		if runes, long := n.s.LongLine(to - 1); long {
			colHi := 0
			if to == p.hi {
				colHi = p.colHi
			}
			m, found, done, err := n.backwardHuge(to-1, runes, p, colHi)
			if err != nil || found || !done {
				return m, found, done, err
			}
			to--
			continue
		}
		first, count, text := n.s.ChunkBefore(to, n.window)
		if count == 0 {
			break
		}
		if !p.wrapped && !p.resumed && to == p.hi && to < n.total {
			// The first window ends on from's line. Read on past it, so a
			// multi-line match that starts there, before from, is whole.
			if _, ahead, more := n.s.Chunk(to, n.window); ahead > 0 {
				text += "\n" + more
			}
		}
		if err := n.account(text); err != nil {
			return Match{}, false, false, err
		}
		hits := n.locate(first, text)
		for i := len(hits) - 1; i >= 0; i-- {
			h := hits[i]
			if h.Start.Less(n.from) == p.wrapped {
				if p.wrapped {
					break // every earlier match starts before from too
				}
				continue
			}
			return h.Match, true, true, nil
		}
		to = first
	}
	return Match{}, false, true, nil
}
