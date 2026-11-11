package fuzzy

import "unicode"

// Matcher is a prepared query with reusable target scratch buffers, Fresh's
// FuzzyMatcher. It gives the same result as Pattern for the same query and target,
// but a file finder ranking thousands of paths reuses tc/tl instead of converting
// every path to runes for every term. It is not safe for concurrent use; the finder
// uses one matcher per Rank call.
type Matcher struct {
	terms []queryTerm
	tc    []rune // target runes, upper-case as written
	tl    []rune // target runes, lower-cased
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
	if m == nil {
		return 0, nil, false
	}
	if len(m.terms) == 0 {
		return 0, nil, true
	}
	if isASCIIString(target) {
		for _, t := range m.terms {
			if t.ascii == nil || !isSubsequenceASCIIString(t.ascii, target) {
				return 0, nil, false
			}
		}
	}
	// matchPrepared starts with the subsequence check, so a non-ASCII target is
	// rejected there, after one conversion.
	m.tc = appendRunes(m.tc[:0], target)
	m.tl = appendLowerRunes(m.tl[:0], target)
	r := matchPrepared(m.terms, m.tc, m.tl)
	return r.score, r.positions, r.matched
}

// isSubsequenceASCIIString is isSubsequenceBytes over a string, without building a
// byte slice.
func isSubsequenceASCIIString(q []byte, target string) bool {
	if len(q) == 0 {
		return true
	}
	qi := 0
	for i := 0; i < len(target); i++ {
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
