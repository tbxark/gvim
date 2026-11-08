package buffer

import (
	"container/list"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// materializeCap is MaxMaterialize; tests lower it.
var materializeCap int64 = MaxMaterialize

// errClosed freezes a closed buffer: it serves cached data only.
var errClosed = errors.New("buffer: closed")

// statCounters are the work counters of one large buffer. They are atomic
// because index scans and snapshots read the source on other goroutines.
type statCounters struct {
	readBytes, cacheBytes, materialized, overCap atomic.Int64
	decodedRunes, lineCalls                      atomic.Int64
}

// baseReader reads the lines of a source. It holds only data that is safe to
// share: a value copy of the line index, the page cache and the counters. It
// never touches the UI-goroutine state of bigStore, so a copy with a frozen
// index can be used on another goroutine.
type baseReader struct {
	src     *srcRef
	g       gridView
	pages   *pageCache
	ranges  *rangeMemo
	huges   *hugeMemo // the huge indexes of this source, see large_huge.go
	stats   *statCounters
	maxLine int64
	// frozen, when set, is the error that stops further source reads; cached
	// pages keep being served.
	frozen error
}

// bigStore is the state behind a large Buffer. Everything in it is used on
// the UI goroutine only, except through baseReader copies and IndexScans.
//
// The document is the overlay st over the source's base lines: the in-order
// pieces of st.root, then base lines [st.tailFirst, baseLines).
type bigStore struct {
	baseReader
	budget *PageBudget
	dec    *lineCache
	huge   bool // a line longer than MaxLine has been seen
	// strideOver and chunkOver replace the stride and the chunk of huge line indexes, in tests.
	strideOver, chunkOver int
	err                   error
	// refusal is the reason the last read or edit was refused (Refusal).
	refusal error
	grew    bool
	closed  bool

	st   bigState
	tree treeOps
	// Caps on the estimated memory held by unsaved edits and by history, and
	// on the number of undo groups.
	maxOverlay, maxHistory int64
	maxGroups              int
}

// fail records the first source error and freezes reads that would need the
// source. Cached data keeps being served.
func (s *bigStore) fail(err error) {
	if errors.Is(err, ErrSourcePending) {
		// A window miss is retryable: it is loaded in a command and the next
		// paint reads it. Do not freeze the buffer for it.
		return
	}
	if s.err == nil {
		s.err = err
		s.frozen = err
	}
}

// docLines is the number of document lines known: the overlay plus the base
// lines it does not cover. It is 0 only while the first line is provisional.
func (s *bigStore) docLines() int {
	return s.st.root.size() + s.g.baseLines() - s.st.tailFirst
}

// lines is the number of document lines: at least one, growing at the end
// while the index is built.
func (s *bigStore) lines() int { return max(1, s.docLines()) }

func (s *bigStore) clampLine(i int) int { return min(max(i, 0), s.lines()-1) }

// lineRef says where a document line comes from: a literal line of the
// overlay, or a base line of the source.
type lineRef struct {
	lit   []rune // literal text; valid when isLit
	isLit bool
	base  int   // base line number when !isLit
	nb    int   // UTF-8 length of lit
	eol   uint8 // terminator flag of lit
}

// ref locates document line i, which must be in range.
func (s *bigStore) ref(i int) lineRef { return refAt(s.st, i) }

// refAt locates document line i of state st, which must be in range.
func refAt(st bigState, i int) lineRef {
	if p, idx, ok := at(st.root, i); ok {
		if p.isRun() {
			return lineRef{base: p.first + idx}
		}
		return lineRef{isLit: true, lit: p.lit[idx], nb: p.nb[idx], eol: p.eolAt(idx)}
	}
	return lineRef{base: st.tailFirst + i - st.root.size()}
}

// decLine is a decoded base line. invalid reports bytes that are not valid
// UTF-8 (each shows as U+FFFD) and huge a line cut at MaxLine.
type decLine struct {
	base          int
	runes         []rune
	huge, invalid bool
}

func (d *decLine) cost() int64 { return int64(len(d.runes))*4 + 64 }

// lineCache is a byte-budgeted LRU of decoded base lines keyed by base line
// number. Base lines never change, so entries are never invalidated. UI
// goroutine only.
type lineCache struct {
	budget, used int64
	byLine       map[int]*list.Element
	lru          *list.List
}

func newLineCache(budget int64) *lineCache {
	return &lineCache{budget: budget, byLine: make(map[int]*list.Element), lru: list.New()}
}

func (c *lineCache) get(base int) *decLine {
	e, ok := c.byLine[base]
	if !ok {
		return nil
	}
	c.lru.MoveToFront(e)
	return e.Value.(*decLine)
}

func (c *lineCache) put(d *decLine) {
	if d.cost() > c.budget {
		return
	}
	c.byLine[d.base] = c.lru.PushFront(d)
	c.used += d.cost()
	for c.used > c.budget {
		old := c.lru.Remove(c.lru.Back()).(*decLine)
		delete(c.byLine, old.base)
		c.used -= old.cost()
	}
}

func (c *lineCache) remove(base int) {
	if e, ok := c.byLine[base]; ok {
		c.used -= c.lru.Remove(e).(*decLine).cost()
		delete(c.byLine, base)
	}
}

func (c *lineCache) clear() {
	c.byLine = make(map[int]*list.Element)
	c.lru.Init()
	c.used = 0
}

// decodeRunes converts text bytes to runes the way []rune(string(data)) does:
// every invalid byte becomes U+FFFD.
func decodeRunes(data []byte) (runes []rune, invalid bool) {
	runes = make([]rune, 0, len(data))
	for len(data) > 0 {
		r, n := utf8.DecodeRune(data)
		if r == utf8.RuneError && n == 1 {
			invalid = true
		}
		runes = append(runes, r)
		data = data[n:]
	}
	return runes, invalid
}

// validString returns data as a string, with U+FFFD for each invalid byte.
func validString(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	return string([]rune(string(data)))
}

// decodedBase returns base line i decoded to runes, from the cache or the
// source. On a read failure it records the error and returns an empty line
// that is not cached.
func (s *bigStore) decodedBase(i int) (*decLine, error) {
	if d := s.dec.get(i); d != nil {
		return d, nil
	}
	data, huge, err := s.lineBytes(i)
	if err != nil {
		s.fail(err)
		return &decLine{base: i}, err
	}
	runes, invalid := decodeRunes(data)
	s.stats.decodedRunes.Add(int64(len(runes)))
	d := &decLine{base: i, runes: runes, huge: huge || (i == 0 && s.g.provisional()), invalid: invalid}
	s.dec.put(d)
	return d, nil
}

// lineInfo is the text of a document line and what limits editing it.
type lineInfo struct {
	runes []rune
	// huge: the line is cut at MaxLine or is the provisional first line.
	// invalid: the source line holds bytes that are not valid UTF-8.
	huge, invalid bool
}

// info returns document line i (clamped), decoded. On a read failure it
// returns the empty line and the error.
func (s *bigStore) info(i int) (lineInfo, error) {
	r := s.ref(s.clampLine(i))
	if r.isLit {
		return lineInfo{runes: r.lit}, nil
	}
	d, err := s.decodedBase(r.base)
	return lineInfo{runes: d.runes, huge: d.huge, invalid: d.invalid}, err
}

// runesOf returns the runes of line i, empty when the source cannot be read.
func (s *bigStore) runesOf(i int) []rune {
	li, _ := s.info(i)
	return li.runes
}

// lineString returns document line i as text. A base line that is not cached
// is read but not decoded into the cache.
func (s *bigStore) lineString(i int) (string, error) {
	r := s.ref(s.clampLine(i))
	if r.isLit {
		return string(r.lit), nil
	}
	if d := s.dec.get(r.base); d != nil {
		return string(d.runes), nil
	}
	data, _, err := s.lineBytes(r.base)
	if err != nil {
		s.fail(err)
		return "", err
	}
	return validString(data), nil
}

func (s *bigStore) line(i int) string {
	out, _ := s.lineString(i)
	return out
}

func (s *bigStore) lineLen(i int) int {
	if h := s.hugeOf(i); h != nil {
		return h.runes
	}
	return len(s.runesOf(i))
}

func (s *bigStore) runeAt(p Pos) (rune, bool) {
	if p.Line < 0 || p.Line >= s.lines() {
		return 0, false
	}
	if h := s.hugeOf(p.Line); h != nil {
		if p.Col < 0 || p.Col >= h.runes {
			return 0, false
		}
		r, _ := utf8.DecodeRuneInString(s.hugeSlice(h, p.Col, 1))
		return r, true
	}
	line := s.runesOf(p.Line)
	if p.Col < 0 || p.Col >= len(line) {
		return 0, false
	}
	return line[p.Col], true
}

func (s *bigStore) clamp(p Pos) Pos {
	p.Line = s.clampLine(p.Line)
	if p.Col <= 0 {
		// Column 0 is valid on every line; do not read the line to find out.
		p.Col = 0
		return p
	}
	p.Col = min(p.Col, s.lineLen(p.Line))
	return p
}

func (s *bigStore) end() Pos {
	last := s.lines() - 1
	return Pos{last, s.lineLen(last)}
}

// lineBytesLen is the stored UTF-8 length of line i without its terminator,
// found from the index without reading a base line. Invalid bytes count as
// one byte each. A line longer than MaxLine counts as its runes, which are
// read once to find out (see large_huge.go), and as MaxLine while the first
// line is not yet terminated.
func (s *bigStore) lineBytesLen(i int) int {
	r := s.ref(s.clampLine(i))
	if r.isLit {
		return r.nb
	}
	if h := s.hugeBase(r.base); h != nil {
		return h.runes
	}
	start, end, _, err := s.lineRange(r.base)
	if err != nil {
		s.fail(err)
		return 0
	}
	return int(min(end-start, s.maxLine))
}

// lineCut reports whether line i is shown cut at MaxLine. It reads no line:
// the length comes from the index.
func (s *bigStore) lineCut(i int) bool {
	r := s.ref(s.clampLine(i))
	if r.isLit {
		return false
	}
	if r.base == 0 && s.g.provisional() {
		return true
	}
	start, end, _, err := s.lineRange(r.base)
	return err == nil && end-start > s.maxLine
}

// lineUnfinished reports whether line i is the first line of a source whose index has not
// found its end: only a prefix of it can be read until the index goes on. It reads no line.
func (s *bigStore) lineUnfinished(i int) bool {
	r := s.ref(s.clampLine(i))
	return !r.isLit && r.base == 0 && s.g.provisional()
}

// lineSlice returns runes [col, col+n) of line i. A base line that is not
// cached is decoded only as far as col+n and is not cached.
func (s *bigStore) lineSlice(i, col, n int) string {
	r := s.ref(s.clampLine(i))
	col, n = max(col, 0), max(n, 0)
	runes := r.lit
	if !r.isLit {
		if h := s.hugeBase(r.base); h != nil {
			return s.hugeSlice(h, col, n)
		}
		d := s.dec.get(r.base)
		if d == nil {
			return s.baseSlice(r.base, col, n)
		}
		runes = d.runes
	}
	col = min(col, len(runes))
	return string(runes[col:min(col+n, len(runes))])
}

// baseSlice decodes runes [col, col+n) of base line b without caching it.
func (s *bigStore) baseSlice(b, col, n int) string {
	// col+n runes of at most utf8.UTFMax bytes each.
	data, err := s.linePrefix(b, int64(col+n)*utf8.UTFMax)
	if err != nil {
		s.fail(err)
		return ""
	}
	out := make([]rune, 0, min(n, len(data)))
	decoded := 0
	for len(data) > 0 && decoded < col+n {
		r, size := utf8.DecodeRune(data)
		if decoded >= col {
			out = append(out, r)
		}
		decoded++
		data = data[size:]
	}
	s.stats.decodedRunes.Add(int64(decoded))
	return string(out)
}

// baseBytes is the text length of base lines [a, b] joined by newlines,
// counting every terminator as one byte.
func (s *bigStore) baseBytes(a, b int) (int64, error) {
	start, _, _, err := s.lineRange(a)
	if err != nil {
		return 0, err
	}
	_, end, _, err := s.lineRange(b)
	return end - start, err
}

// spanBytes estimates the text length of document lines [i, j] joined by
// newlines, from the index and the overlay alone, counting every base line
// terminator as it is in the file (a CRLF is two bytes). It stops early once
// the total passes limit.
func (s *bigStore) spanBytes(i, j int, limit int64) (int64, error) {
	var total int64
	for first := true; i <= j && total <= limit; first = false {
		if !first {
			total++ // newline between pieces
		}
		p, idx, ok := at(s.st.root, i)
		if !ok {
			off := s.st.tailFirst - s.st.root.size()
			n, err := s.baseBytes(i+off, j+off)
			return total + n, err
		}
		n := min(p.length()-idx, j-i+1)
		if p.isRun() {
			nb, err := s.baseBytes(p.first+idx, p.first+idx+n-1)
			if err != nil {
				return 0, err
			}
			total += nb
		} else {
			for _, nb := range p.nb[idx : idx+n] {
				total += int64(nb)
			}
			total += int64(n - 1)
		}
		i += n
	}
	return total, nil
}

// rangeLimited returns the text between from and to, with the same result as a
// memory buffer holding the same lines. It returns ErrOverCap, and no text,
// when the result would exceed limit bytes, and the source error when a line
// cannot be read.
func (s *bigStore) rangeLimited(from, to Pos, limit int64) (string, error) {
	from, to = s.clamp(from), s.clamp(to)
	if to.Less(from) {
		from, to = to, from
	}
	if from == to {
		return "", nil
	}
	if from.Line == to.Line {
		if h := s.hugeOf(from.Line); h != nil {
			if int64(to.Col-from.Col) > limit {
				return "", ErrOverCap
			}
			out := s.hugeSlice(h, from.Col, to.Col-from.Col)
			if s.err != nil {
				return "", s.err
			}
			if int64(len(out)) > limit {
				return "", ErrOverCap
			}
			return out, nil
		}
		li, err := s.info(from.Line)
		if err != nil {
			return "", err
		}
		out := string(li.runes[from.Col:to.Col])
		if int64(len(out)) > limit {
			return "", ErrOverCap
		}
		return out, nil
	}
	if int64(to.Line-from.Line) > limit {
		return "", ErrOverCap
	}
	if !s.hugeSeen() {
		// The estimate counts a CRLF as two bytes, but the text joins lines
		// with one newline: take a byte per terminator off so it stays a
		// lower bound and never refuses text that fits.
		if n, err := s.spanBytes(from.Line, to.Line, limit); err == nil && n-int64(to.Line-from.Line) > limit {
			return "", ErrOverCap
		}
	}
	var sb strings.Builder
	// A column of 0 takes the line whole, so it is read but not decoded.
	if h := s.hugeOf(from.Line); h != nil {
		if int64(h.runes-from.Col) > limit {
			return "", ErrOverCap
		}
		sb.WriteString(s.hugeSlice(h, from.Col, h.runes-from.Col))
		if s.err != nil {
			return "", s.err
		}
	} else if from.Col == 0 {
		text, err := s.lineString(from.Line)
		if err != nil {
			return "", err
		}
		sb.WriteString(text)
	} else {
		li, err := s.info(from.Line)
		if err != nil {
			return "", err
		}
		sb.WriteString(string(li.runes[from.Col:]))
	}
	for i := from.Line + 1; i < to.Line; i++ {
		sb.WriteByte('\n')
		if h := s.hugeOf(i); h != nil {
			if int64(h.runes) > limit {
				return "", ErrOverCap
			}
			sb.WriteString(s.hugeSlice(h, 0, h.runes))
			if s.err != nil {
				return "", s.err
			}
			continue
		}
		text, err := s.lineString(i)
		if err != nil {
			return "", err
		}
		sb.WriteString(text)
		if int64(sb.Len()) > limit {
			return "", ErrOverCap
		}
	}
	sb.WriteByte('\n')
	if h := s.hugeOf(to.Line); h != nil {
		if int64(to.Col) > limit {
			return "", ErrOverCap
		}
		sb.WriteString(s.hugeSlice(h, 0, to.Col))
		if s.err != nil {
			return "", s.err
		}
	} else if to.Col > 0 {
		tail, err := s.info(to.Line)
		if err != nil {
			return "", err
		}
		sb.WriteString(string(tail.runes[:to.Col]))
	}
	if int64(sb.Len()) > limit {
		return "", ErrOverCap
	}
	return sb.String(), nil
}

// rangeText is rangeLimited under the materialization cap, with the read
// counted in the stats.
func (s *bigStore) rangeText(from, to Pos) (string, error) {
	out, err := s.rangeLimited(from, to, materializeCap)
	switch {
	case errors.Is(err, ErrOverCap):
		s.stats.overCap.Add(1)
	case err == nil:
		s.stats.materialized.Add(int64(len(out)))
	}
	return out, err
}

func (s *bigStore) text() (string, error) {
	return s.rangeText(Pos{}, Pos{Line: s.lines() - 1, Col: math.MaxInt})
}

// hugeSeen reports whether any line is longer than MaxLine, or may be.
func (s *bigStore) hugeSeen() bool { return s.huge || s.g.provisional() }

// textPrefix returns the first maxBytes bytes of the text, cut back to a whole
// rune. It reads only the lines it needs.
func (s *bigStore) textPrefix(maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	var sb strings.Builder
	for i, n := 0, s.lines(); i < n && sb.Len() < maxBytes; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if h := s.hugeOf(i); h != nil {
			sb.WriteString(s.hugeSlice(h, 0, maxBytes-sb.Len()))
			continue
		}
		text, err := s.lineString(i)
		if err != nil {
			break
		}
		sb.WriteString(text)
	}
	out := sb.String()
	if len(out) <= maxBytes {
		return out
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut]
}

// close releases the buffer's reference to the source and drops its caches.
// Index scans and snapshots that hold their own reference keep reading.
func (s *bigStore) close() {
	if s.closed {
		return
	}
	s.closed = true
	s.fail(errClosed)
	s.pages.clear()
	s.dec.clear()
	s.budget.stores.Add(-1)
	s.src.release()
}
