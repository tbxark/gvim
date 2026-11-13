package search

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// MaxMatches is the most matches a Scan collects. A search that finds more stops
// there and reports Capped, so a pattern like "e" in a multi-gigabyte file ends
// with a list of bounded size.
const MaxMatches = 100_000

// ScanStats is what a Scan has done so far, for tests and for the status row.
type ScanStats struct {
	// RunesCounted is the number of runes counted to turn byte offsets into
	// columns. The count goes on from one match to the next on a line, so it
	// stays near the number of runes of the lines that hold matches, however
	// many matches there are.
	RunesCounted int64
	// BytesScanned is the length of the text handed to the matcher.
	BytesScanned int64
	// Elapsed is the time from NewScan to the end of the scan (or to now while
	// it runs, or to Close if it was closed early).
	Elapsed time.Duration
}

// Scan is a search of one snapshot for every match of a query, done in slices of
// time so that an interface can go on while a large file is searched. Make one
// with NewScan, call Step until it says done (each call from one goroutine at a
// time, which may be a different one each time, as when every call is a tea.Cmd),
// read Matches and Progress between calls, and Close it.
//
// The scan starts at a line (the caret) and goes to the end of the document, then
// wraps to the start and stops at that line, so the matches nearest the caret
// are found first. The text is read in windows of about 1 MiB of whole lines. As
// in FindNext, a multi-line match that crosses a window boundary is not found, and
// the lines on either side of the starting line are searched in different
// windows. A huge line, which is longer than the line cap of a large buffer, is
// searched in windows of runes of its own (see hugeSpan), so every match in it is found.
type Scan struct {
	snap    *buffer.Snapshot
	re      *regexp.Regexp
	q       Query
	version uint64
	start   int // line the scan began at
	total   int // lines in the snapshot
	window  int // bytes per window
	limit   int // MaxMatches; tests lower it
	partial bool
	huge    hugeSpan // the windows of a huge line; tests make them small

	line    int // next line to read
	col     int // the next column of the huge line sc.line to search
	end     int // end of the current part (exclusive)
	wrapped bool
	wrapAt  int // len(matches) when the wrap began
	matches []Match
	capped  bool
	covered bool // the whole document was searched
	done    bool
	err     error

	stats    ScanStats
	began    time.Time
	finished time.Time
}

// NewScan starts a search of snapshot s for q, beginning at line startLine
// (clamped into the document). Nothing is read until Step. An empty query gives
// a scan that is already done with no matches. A pattern that does not compile
// returns the error Find returns.
//
// On success the Scan owns s and Close releases it. On error s is not released;
// the caller still owns it.
func NewScan(s *buffer.Snapshot, q Query, startLine int) (*Scan, error) {
	return newScan(s, q, startLine, nextWindow, MaxMatches)
}

// newScan is NewScan with the window size and the match cap as parameters, so
// tests can make every window boundary and the cap matter.
func newScan(s *buffer.Snapshot, q Query, startLine, window, limit int) (*Scan, error) {
	var re *regexp.Regexp
	if q.Text != "" {
		var err error
		if re, err = compile(q); err != nil {
			return nil, err
		}
	}
	total := s.Lines()
	sc := &Scan{
		snap: s, re: re, q: q, version: s.Version(),
		start:   min(max(startLine, 0), max(total-1, 0)),
		total:   total,
		window:  max(window, 1),
		limit:   max(limit, 1),
		partial: !s.IndexComplete(),
		huge:    defaultHuge,
		began:   time.Now(),
	}
	sc.line, sc.end = sc.start, total
	if re == nil {
		sc.covered = true
		sc.finish()
	}
	return sc, nil
}

// Step searches for about budget, then returns. It always reads at least one
// window, so a budget of zero still makes progress, and a window is not cut
// short, so a call can take longer than budget on a slow pattern. done is true
// when the scan has ended: the document was searched, the match cap was reached,
// the snapshot failed to read (err is set then), or the scan was closed. Once done
// it does nothing and returns the same answer.
func (sc *Scan) Step(budget time.Duration) (done bool, err error) {
	if sc.done {
		return true, sc.err
	}
	begin := time.Now()
	for !sc.done {
		sc.window1()
		if time.Since(begin) >= budget {
			break
		}
	}
	return sc.done, sc.err
}

// window1 searches the next window, moving to the second part of the document
// or ending the scan when the current part is used up.
func (sc *Scan) window1() {
	for sc.line >= sc.end {
		if sc.wrapped || sc.start == 0 {
			sc.covered = true
			sc.finish()
			return
		}
		sc.wrapped, sc.wrapAt = true, len(sc.matches)
		sc.line, sc.end = 0, sc.start
	}
	if runes, long := sc.snap.LongLine(sc.line); long {
		sc.hugeWindow(runes)
		return
	}
	first, n, text := sc.snap.Chunk(sc.line, sc.window)
	if err := sc.snap.Err(); err != nil {
		// Lines that could not be read came back empty, so a search of them
		// would report "no match" where it did not look.
		sc.err = err
		sc.finish()
		return
	}
	if n == 0 {
		sc.line = sc.end
		sc.coverIfDone()
		return
	}
	if first+n > sc.end {
		// The second part ends before the line the scan began at; the window
		// is cut there.
		n = sc.end - first
		text = firstLines(text, n)
	}
	sc.line = first + n
	sc.stats.BytesScanned += int64(len(text))
	d := newWindow(first, text)
	cur := newOffsetPos(d.text, buffer.Pos{Line: d.first})
	for _, loc := range d.locate(sc.re, sc.q) {
		if len(sc.matches) >= sc.limit {
			sc.capped = true
			break
		}
		start := cur.pos(loc[0])
		sc.matches = append(sc.matches, Match{Start: start, End: cur.pos(loc[1])})
	}
	sc.stats.RunesCounted += cur.counted
	if sc.capped {
		sc.finish()
		return
	}
	sc.coverIfDone()
}

// hugeWindow searches the next window of the huge line sc.line, which has runes runes. When
// the window is the last of the line, the scan goes on to the next line.
func (sc *Scan) hugeWindow(runes int) {
	lo := sc.col
	hi := min(lo+sc.huge.window, runes)
	first, text := sc.huge.text(sc.snap, sc.line, runes, lo, hi)
	if err := sc.snap.Err(); err != nil {
		sc.err = err
		sc.finish()
		return
	}
	sc.stats.BytesScanned += int64(len(text))
	for _, m := range hugeMatches(sc.line, first, text, sc.re, sc.q, lo, hi) {
		if len(sc.matches) >= sc.limit {
			sc.capped = true
			break
		}
		sc.matches = append(sc.matches, m)
	}
	if sc.capped {
		sc.finish()
		return
	}
	if hi < runes {
		sc.col = hi
		return
	}
	sc.col = 0
	sc.line++
	sc.coverIfDone()
}

// coverIfDone ends the scan when the last part of the document is used up.
func (sc *Scan) coverIfDone() {
	if sc.line >= sc.end && (sc.wrapped || sc.start == 0) {
		sc.covered = true
		sc.finish()
	}
}

// firstLines returns the first n lines of text, which is lines joined by "\n".
func firstLines(text string, n int) string {
	off := 0
	for ; n > 0; n-- {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return text
		}
		off += i + 1
	}
	return text[:off-1]
}

// finish ends the scan and puts the matches in document order: the ones found
// after the wrap came from the lines above the starting line.
func (sc *Scan) finish() {
	if sc.done {
		return
	}
	sc.done = true
	sc.finished = time.Now()
	if sc.wrapped && sc.wrapAt < len(sc.matches) {
		sc.matches = slices.Concat(sc.matches[sc.wrapAt:], sc.matches[:sc.wrapAt])
	}
}

// Matches returns the matches found so far: in the order found, from the starting
// line to the end of the document and then from its start, while the scan runs; in
// document order once it is done. The slice is not changed by later Steps, and
// the caller must not change it.
func (sc *Scan) Matches() []Match { return slices.Clip(sc.matches) }

// Progress returns the part of the document searched, from 0 to 1.
func (sc *Scan) Progress() float64 {
	if sc.covered || sc.total <= 0 {
		return 1
	}
	read := sc.line - sc.start
	if sc.wrapped {
		read = sc.total - sc.start + sc.line
	}
	return float64(min(max(read, 0), sc.total)) / float64(sc.total)
}

// Done reports whether the scan has ended.
func (sc *Scan) Done() bool { return sc.done }

// Capped reports whether the scan stopped at the match cap, so there are more
// matches than Matches lists.
func (sc *Scan) Capped() bool { return sc.capped }

// Partial reports whether the snapshot was taken before the line index of a large
// buffer was complete, so that the scan covers only the part of the file that was
// indexed.
func (sc *Scan) Partial() bool { return sc.partial }

// Version returns the buffer version of the snapshot that is searched. When the
// buffer's version is another, the matches are of text that has changed.
func (sc *Scan) Version() uint64 { return sc.version }

// Stats returns what the scan has done so far.
func (sc *Scan) Stats() ScanStats {
	st := sc.stats
	if sc.done {
		st.Elapsed = sc.finished.Sub(sc.began)
	} else {
		st.Elapsed = time.Since(sc.began)
	}
	return st
}

// Close ends the scan and releases the snapshot. The matches found so far stay
// readable. It is safe to call more than once.
func (sc *Scan) Close() {
	sc.finish()
	if sc.snap != nil {
		sc.snap.Release()
		sc.snap = nil
	}
}
