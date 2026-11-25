package fuzzy

import (
	"slices"
	"unicode"
)

// Matcher is a prepared query with reusable target scratch buffers, Fresh's
// FuzzyMatcher. It gives the same result as Pattern for the same query and target,
// but a file finder ranking thousands of paths reuses tc/tl instead of converting
// every path to runes for every term. It is not safe for concurrent use; the finder
// uses one matcher per Rank call.
type Matcher struct {
	terms []queryTerm
	tc    []rune    // target runes, upper-case as written
	tl    []rune    // target runes, lower-cased
	dp    dpScratch // reusable DP buffers across targets
}

// NewMatcher prepares query once. A blank query matches every target with score 0.
func NewMatcher(query string) *Matcher {
	return &Matcher{terms: prepareQuery(query)}
}

// Match scores target against the prepared query. It is Pattern.Match's result:
// score is the rank, indexes are the matched rune positions and ok is false when
// any whitespace-separated term is not a subsequence of target. An ASCII target
// that cannot contain an ASCII term is rejected without allocating or converting it.
func (m *Matcher) Match(target string) (score int, indexes []int, ok bool) {
	return m.match(target, true)
}

// MatchScore scores target against the prepared query identically to Match, but
// skips allocating and copying the matched rune positions slice.
func (m *Matcher) MatchScore(target string) (score int, ok bool) {
	score, _, ok = m.match(target, false)
	return score, ok
}

func (m *Matcher) match(target string, needPositions bool) (score int, indexes []int, ok bool) {
	if m == nil {
		return 0, nil, false
	}
	if len(m.terms) == 0 {
		return 0, nil, true
	}
	ascii := isASCIIString(target)
	if ascii {
		for _, t := range m.terms {
			if t.ascii == nil || !isSubsequenceASCIIString(t.ascii, target) {
				return 0, nil, false
			}
		}
		m.tc, m.tl = prepareTargetASCII(m.tc, m.tl, target)
	} else {
		// matchPreparedScratchOpts starts with the subsequence check for non-ASCII targets.
		m.tc = appendRunes(m.tc[:0], target)
		m.tl = appendLowerRunes(m.tl[:0], target)
	}
	r := matchPreparedScratchOpts(m.terms, m.tc, m.tl, &m.dp, ascii, needPositions)
	if !r.matched {
		return 0, nil, false
	}
	if needPositions && len(r.positions) > 0 {
		indexes = make([]int, len(r.positions))
		copy(indexes, r.positions)
	}
	return r.score, indexes, true
}

func prepareTargetASCII(tc, tl []rune, s string) ([]rune, []rune) {
	n := len(s)
	tc = slices.Grow(tc[:0], n)[:n]
	tl = slices.Grow(tl[:0], n)[:n]
	if n == 0 {
		return tc, tl
	}
	_ = tc[n-1]
	_ = tl[n-1]
	for i := range n {
		b := s[i]
		tc[i] = rune(b)
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		tl[i] = rune(b)
	}
	return tc, tl
}

// isSubsequenceASCIIString is isSubsequenceBytes over a string, without building a
// byte slice.
func isSubsequenceASCIIString(q []byte, target string) bool {
	if len(q) == 0 {
		return true
	}
	qi := 0
	for i := range len(target) {
		b := target[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if b == q[qi] {
			qi++
			if qi == len(q) {
				return true
			}
		}
	}
	return false
}

// appendRunes appends the runes of s to dst.
func appendRunes(dst []rune, s string) []rune {
	for _, r := range s {
		dst = append(dst, r)
	}
	return dst
}

// appendLowerRunes appends the lower-cased runes of s to dst.
func appendLowerRunes(dst []rune, s string) []rune {
	for _, r := range s {
		dst = append(dst, unicode.ToLower(r))
	}
	return dst
}
