package search

import (
	"errors"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
)

// Limits of FindRange and FindLines. A caller that wants more asks again for
// the part of the document that is left.
const (
	// DefaultFindMax is the number of matches FindRange returns when max is 0
	// or less, and MaxFindMax the most it ever returns.
	DefaultFindMax = 1000
	MaxFindMax     = 100_000
	// MaxRangeBytes is the longest range FindRange reads, in bytes of text.
	MaxRangeBytes = 16 << 20
	// MaxRegexWork bounds a regular expression search: the length of the text
	// times the number of instructions of the compiled program. A program is
	// about 3 to 6 instructions per simple element, and the standard matcher
	// spends roughly 6 ns on one unit, so the bound is about a quarter of a
	// second. A pattern that is a plain string is exempt.
	MaxRegexWork = 40_000_000
)

// ErrTooCostly is returned when the search would exceed MaxRegexWork. A smaller
// range, or a line by line search (FindLines), may succeed.
var ErrTooCostly = errors.New("search: pattern too costly for this much text")

// ErrRangeTooLarge is returned by FindRange for a range over MaxRangeBytes.
var ErrRangeTooLarge = errors.New("search: range too large")

// FindRange returns at most max matches of q that lie inside from..to, in
// document order, and stops scanning as soon as max is reached. truncated says
// that the range holds more. A max of 0 or less means DefaultFindMax, and one
// over MaxFindMax is MaxFindMax. from and to are clamped and put in order.
//
// The range is read once, up to MaxRangeBytes, and is the text the pattern sees:
// ^ matches at its start, and nothing outside it is looked at. A plain string
// query is found with a string search and costs no more than the text. A
// regular expression is subject to MaxRegexWork, except one that is a plain
// string. A large buffer returns ErrLargeBuffer without reading anything. An
// empty query matches nothing. Positions are rune positions of b, as in Find.
func FindRange(b *buffer.Buffer, q Query, from, to buffer.Pos, max int) (ms []Match, truncated bool, err error) {
	if b.Large() {
		return nil, false, ErrLargeBuffer
	}
	if q.Text == "" {
		return nil, false, nil
	}
	switch {
	case max <= 0:
		max = DefaultFindMax
	case max > MaxFindMax:
		max = MaxFindMax
	}
	from, to = b.Clamp(from), b.Clamp(to)
	if to.Less(from) {
		from, to = to, from
	}
	if rangeBytes(b, from, to) > MaxRangeBytes {
		return nil, false, ErrRangeTooLarge
	}
	var next func(text string, limit int) ([][2]int, error)
	if q.Regex {
		re, err := compile(q)
		if err != nil {
			return nil, false, err
		}
		// The cost is known from the size of the range, before any text is read.
		if _, complete := re.LiteralPrefix(); !complete && int64(rangeBytes(b, from, to))*int64(programSize(re)) > MaxRegexWork {
			return nil, false, ErrTooCostly
		}
		next = func(text string, limit int) ([][2]int, error) { return regexOffsets(re, q, text, limit) }
	} else {
		next = func(text string, limit int) ([][2]int, error) { return literalOffsets(q, text, limit), nil }
	}
	text, err := b.RangeChecked(from, to)
	if err != nil {
		return nil, false, err
	}
	offs, err := next(text, max+1)
	if err != nil {
		return nil, false, err
	}
	if len(offs) > max {
		offs, truncated = offs[:max], true
	}
	if len(offs) == 0 {
		return nil, false, nil
	}
	conv := newOffsetPos(text, from)
	ms = make([]Match, len(offs))
	for i, o := range offs {
		ms[i] = Match{Start: conv.pos(o[0]), End: conv.pos(o[1])}
	}
	return ms, truncated, nil
}

// rangeBytes returns the length in bytes of the text between from and to, found
// from the line lengths without building any text.
func rangeBytes(b *buffer.Buffer, from, to buffer.Pos) int {
	if from.Line == to.Line {
		return b.ByteCol(to) - b.ByteCol(from)
	}
	n := b.LineBytes(from.Line) - b.ByteCol(from) + 1 + b.ByteCol(to)
	for l := from.Line + 1; l < to.Line; l++ {
		n += b.LineBytes(l) + 1
	}
	return n
}

// maxRegexCandidates bounds the raw matches of a whole-word regular expression
// search that are looked at to fill the limit: a filter that rejects more than
// this many fails the call with ErrTooCostly instead of building their list.
const maxRegexCandidates = 1 << 17

// regexOffsets returns up to limit matches of the regular expression of q in
// text, as byte offsets. A match that fails the whole-word test of q is skipped
// and does not count, and more than maxRegexCandidates of them are ErrTooCostly.
// The caller has checked the work rule of MaxRegexWork.
func regexOffsets(re *regexp.Regexp, q Query, text string, limit int) ([][2]int, error) {
	// Ask for the wanted number of matches, and when whole words are filtered
	// and too few survive, once more for the most that is allowed.
	ask := limit
	for {
		all := re.FindAllStringIndex(text, ask)
		out := make([][2]int, 0, min(len(all), limit))
		for _, loc := range all {
			if q.WholeWord && !isWholeWord(text, loc[0], loc[1]) {
				continue
			}
			out = append(out, [2]int{loc[0], loc[1]})
			if len(out) == limit {
				return out, nil
			}
		}
		if !q.WholeWord || len(all) < ask {
			return out, nil
		}
		if ask >= maxRegexCandidates {
			return nil, ErrTooCostly
		}
		ask = maxRegexCandidates
	}
}

// programSize returns the number of instructions of the program the regexp
// package runs for re. It is 1 for a program that cannot be rebuilt.
func programSize(re *regexp.Regexp) int {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return 1
	}
	prog, err := syntax.Compile(parsed.Simplify())
	if err != nil {
		return 1
	}
	return len(prog.Inst)
}

// literalOffsets returns up to limit occurrences of the plain text of q in text,
// as byte offsets. They do not overlap, and with q.WholeWord only those with a
// word boundary on each side are kept, as in Find.
func literalOffsets(q Query, text string, limit int) [][2]int {
	return newLiteralSearch(q).offsets(text, limit)
}

// literalSearch is a plain text query prepared to be run on many texts.
type literalSearch struct {
	q  Query
	sc *foldScan // for a query that ignores case
}

func newLiteralSearch(q Query) *literalSearch {
	l := &literalSearch{q: q}
	if !q.CaseSensitive {
		l.sc = newFoldNeedle(q.Text).scan("", 0)
	}
	return l
}

func (l *literalSearch) offsets(text string, limit int) [][2]int {
	q := l.q
	var out [][2]int
	if l.sc != nil {
		// One pass: after a rejected candidate the scan goes on from where it
		// is, which finds the matches that overlap it, as Find does.
		l.sc.start(text, 0)
		for len(out) < limit {
			s, e := l.sc.next()
			if s < 0 {
				break
			}
			if !q.WholeWord || isWholeWord(text, s, e) {
				out = append(out, [2]int{s, e})
				l.sc.reset(e)
			}
		}
		return out
	}
	for pos := 0; pos <= len(text) && len(out) < limit; {
		s := strings.Index(text[pos:], q.Text)
		if s < 0 {
			break
		}
		s, e := pos+s, pos+s+len(q.Text)
		if !q.WholeWord || isWholeWord(text, s, e) {
			out = append(out, [2]int{s, e})
			pos = e
			continue
		}
		_, size := utf8.DecodeRuneInString(text[s:])
		pos = s + size
	}
	return out
}

// foldNeedle is a needle prepared for a search that ignores case: its runes in
// canonical case, and the failure table of the Knuth-Morris-Pratt search, which
// reads the text once however repetitive the needle and the text are.
type foldNeedle struct {
	want []rune
	fail []int // fail[k]: the longest proper border of want[:k]
}

func newFoldNeedle(needle string) *foldNeedle {
	want := []rune(needle)
	for i, r := range want {
		want[i] = foldCanon(r)
	}
	fail := make([]int, len(want)+1)
	for i, k := 1, 0; i < len(want); i++ {
		for k > 0 && want[i] != want[k] {
			k = fail[k]
		}
		if want[i] == want[k] {
			k++
		}
		fail[i+1] = k
	}
	return &foldNeedle{want: want, fail: fail}
}

// asciiCanon is foldCanon for the runes below utf8.RuneSelf.
var asciiCanon = func() (t [utf8.RuneSelf]rune) {
	for i := range t {
		t[i] = foldCanon(rune(i))
	}
	return t
}()

// foldCanon returns the least rune of the folding orbit of r (unicode.SimpleFold),
// so two runes are equal ignoring case exactly when their canon is equal.
func foldCanon(r rune) rune {
	least := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		least = min(least, f)
	}
	return least
}

// foldScan is a search of a text for a foldNeedle.
type foldScan struct {
	n      *foldNeedle
	text   string
	at     int   // byte offset of the next rune to read
	k      int   // runes of the needle matched ending at at
	seen   int   // runes read since the last reset
	starts []int // byte offsets of the last len(want) runes, a ring
}

// scan starts a search of text from byte offset from.
func (n *foldNeedle) scan(text string, from int) *foldScan {
	s := &foldScan{n: n, text: text}
	if len(n.want) > 0 {
		s.starts = make([]int, len(n.want))
	}
	s.reset(from)
	return s
}

// start points the search at another text, from byte offset from.
func (s *foldScan) start(text string, from int) {
	s.text = text
	s.reset(from)
}

// reset restarts the search at byte offset from, forgetting any partial match.
func (s *foldScan) reset(from int) { s.at, s.k, s.seen = from, 0, 0 }

// next returns the byte offsets of the next occurrence, or -1. Occurrences that
// overlap are all found, one by one; reset skips past a match.
func (s *foldScan) next() (start, end int) {
	m := len(s.n.want)
	if m == 0 {
		return s.at, s.at
	}
	for s.at < len(s.text) {
		var r rune
		size := 1
		if c := s.text[s.at]; c < utf8.RuneSelf {
			r = asciiCanon[c]
		} else {
			r, size = utf8.DecodeRuneInString(s.text[s.at:])
			r = foldCanon(r)
		}
		s.starts[s.seen%m] = s.at
		s.seen++
		s.at += size
		for s.k > 0 && (s.k == m || r != s.n.want[s.k]) {
			s.k = s.n.fail[s.k]
		}
		if r == s.n.want[s.k] {
			s.k++
		}
		if s.k == m {
			return s.starts[(s.seen-m)%m], s.at
		}
	}
	return -1, -1
}

// offsetPos turns byte offsets of a text into buffer positions. Offsets must be
// asked for in non-decreasing order: it keeps its place and counts forward, so
// converting all the offsets costs one pass over the text. Columns count runes, and
// a match on the line of the one before it adds only the runes between the two.
type offsetPos struct {
	text    string
	off     int
	at      buffer.Pos
	counted int64 // runes counted
}

func newOffsetPos(text string, start buffer.Pos) *offsetPos {
	return &offsetPos{text: text, at: start}
}

func (o *offsetPos) pos(off int) buffer.Pos {
	seg := o.text[o.off:off]
	if n := strings.Count(seg, "\n"); n > 0 {
		o.at.Line += n
		o.at.Col = 0
		seg = seg[strings.LastIndexByte(seg, '\n')+1:]
	}
	n := utf8.RuneCountInString(seg)
	o.counted += int64(n)
	o.at.Col += n
	o.off = off
	return o.at
}
