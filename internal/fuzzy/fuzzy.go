// Package fuzzy is the fzf-style scorer shared by the command palette and the
// file finder. It ports Fresh's matcher (crates/fresh-editor/src/input/fuzzy,
// matcher.rs and pattern.rs) so entries rank the way the legacy editor ranks
// them. Scores are integers with the same constants, so the ordering is
// identical, not merely similar.
//
// The package imports nothing from this module.
package fuzzy

import (
	"slices"
	"strings"
	"unicode"
)

// Score matches query against target. The query is split on whitespace and
// every term must match. score is the rank (higher is better). indexes are the
// rune positions in target that the query matched, in increasing order. ok is
// false when some term does not match. An empty or blank query matches every
// target with score 0 and nil indexes.
func Score(query, target string) (score int, indexes []int, ok bool) {
	r := fuzzyMatch(query, target)
	return r.score, r.positions, r.matched
}

// Score constants, from crates/fresh-editor/src/input/fuzzy/mod.rs.
const (
	scoreConsecutive        = 16
	scoreWordBoundary       = 32
	scoreStartOfString      = 48
	scoreCamelCase          = 24
	scoreGapPenalty         = -3
	scoreGapStartPenalty    = -5
	scoreExactMatch         = 100
	scoreExactBasenameMatch = 80
	scoreContiguousSubstr   = 64
	scoreBasenamePrefix     = 64
	scorePathSegmentPrefix  = 32
)

// fuzzyResult is the outcome of matching one query against one target.
type fuzzyResult struct {
	matched   bool
	score     int
	positions []int // rune indices in the target where query runes matched
}

// queryTerm is one whitespace-separated term of a query, lower-cased.
type queryTerm struct {
	lower []rune
}

// prepareQuery splits a query on whitespace (Rust split_whitespace) and
// lower-cases each term. An empty or blank query yields no terms.
func prepareQuery(query string) []queryTerm {
	fields := strings.Fields(query)
	terms := make([]queryTerm, len(fields))
	for i, f := range fields {
		terms[i] = queryTerm{lower: lowerRunes(f)}
	}
	return terms
}

func lowerRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		out = append(out, unicode.ToLower(r))
	}
	return out
}

// fuzzyMatch matches query against target. An empty query matches everything
// with score 0.
func fuzzyMatch(query, target string) fuzzyResult {
	return matchTerms(prepareQuery(query), target)
}

func matchTerms(terms []queryTerm, target string) fuzzyResult {
	if len(terms) == 0 {
		return fuzzyResult{matched: true}
	}
	tc := []rune(target)
	tl := lowerRunes(target)
	// Fast rejection: every term must be a subsequence of the target.
	for _, t := range terms {
		if !isSubsequence(t.lower, tl) {
			return fuzzyResult{}
		}
	}
	if len(terms) > 1 {
		return scoreMultiTerm(terms, tc, tl)
	}
	return scoreSingleTerm(terms[0].lower, tc, tl)
}

func isSubsequence(q, tl []rune) bool {
	if len(q) == 0 {
		return true
	}
	qi := 0
	for _, r := range tl {
		if r == q[qi] {
			qi++
			if qi == len(q) {
				return true
			}
		}
	}
	return false
}

// scoreSingleTerm picks the better of the DP alignment and the best contiguous
// substring, then adds the bonuses for contiguity, exact and prefix matches,
// and path-segment starts.
func scoreSingleTerm(q, tc, tl []rune) fuzzyResult {
	dpPos, dpScore, dpOK := findBestMatch(q, tc, tl)
	subPos, subScore, subOK := findContiguousMatch(q, tc, tl)

	var positions []int
	var final int
	switch {
	case dpOK && subOK:
		if subScore >= dpScore {
			positions, final = subPos, subScore
		} else {
			positions, final = dpPos, dpScore
		}
	case dpOK:
		positions, final = dpPos, dpScore
	case subOK:
		positions, final = subPos, subScore
	default:
		return fuzzyResult{}
	}

	qLen, tLen := len(q), len(tl)
	contiguous := len(positions) == qLen && isConsecutive(positions)
	if contiguous {
		final += scoreContiguousSubstr
	}

	credited := false
	if qLen == tLen {
		final += scoreExactMatch
	} else if tLen > qLen && contiguous {
		prefix := true
		for i, p := range positions {
			if p != i {
				prefix = false
				break
			}
		}
		if prefix && qLen < len(tc) {
			switch tc[qLen] {
			case '.':
				final += scoreExactMatch
				credited = true
			case '-', '_', ' ':
				final += scoreExactBasenameMatch
				credited = true
			}
		}
	}

	if contiguous && !credited && len(positions) > 0 && len(positions) == qLen {
		start := positions[0]
		startsSegment := start == 0 || (start > 0 && tc[start-1] == '/')
		if startsSegment {
			basenameStart := 0
			if i := lastIndex(tc, '/'); i >= 0 {
				basenameStart = i + 1
			}
			if start == basenameStart {
				final += scoreBasenamePrefix
			} else {
				final += scorePathSegmentPrefix
			}
		}
	}

	return fuzzyResult{matched: true, score: final, positions: positions}
}

// scoreMultiTerm scores each term independently, sums the scores, and adds a
// tight-span bonus when the terms appear in order and close together.
func scoreMultiTerm(terms []queryTerm, tc, tl []rune) fuzzyResult {
	total := 0
	var all []int
	for _, t := range terms {
		r := scoreSingleTerm(t.lower, tc, tl)
		if !r.matched {
			return fuzzyResult{}
		}
		total += r.score
		all = append(all, r.positions...)
	}
	slices.Sort(all)
	all = slices.Compact(all)

	if start, end, ok := findSequentialTermSpan(tl, terms); ok {
		termLenSum := 0
		for _, t := range terms {
			termLenSum += len(t.lower)
		}
		gapExcess := max(end-start-termLenSum, 0)
		maxGapExcess := max(len(terms)-1, 0) * 4
		if gapExcess <= maxGapExcess {
			total += scoreExactMatch
			all = all[:0]
			for i := start; i < end && i < len(tc); i++ {
				all = append(all, i)
			}
		}
	}
	return fuzzyResult{matched: true, score: total, positions: all}
}

// findSequentialTermSpan finds the earliest occurrence of each term as a
// contiguous substring, in query order, and returns the span from the first
// term's start to the last term's end.
func findSequentialTermSpan(tl []rune, terms []queryTerm) (start, end int, ok bool) {
	searchFrom := 0
	first := -1
	lastEnd := 0
	for _, t := range terms {
		needle := t.lower
		if len(needle) == 0 {
			continue
		}
		if searchFrom+len(needle) > len(tl) {
			return 0, 0, false
		}
		found := -1
		for s := searchFrom; s <= len(tl)-len(needle); s++ {
			if slices.Equal(tl[s:s+len(needle)], needle) {
				found = s
				break
			}
		}
		if found < 0 {
			return 0, 0, false
		}
		if first < 0 {
			first = found
		}
		searchFrom = found + len(needle)
		lastEnd = searchFrom
	}
	if first < 0 {
		return 0, 0, false
	}
	return first, lastEnd, true
}

// chainNode is one backpointer in the DP arena: the target index matched for
// one query rune, and the arena index of the previous match (or -1).
type chainNode struct {
	ti   int
	prev int
}

// findBestMatch is the fzf-style DP. It tracks, for every query prefix length,
// the best-scoring chain of matches ending in the current target position.
func findBestMatch(q, tc, tl []rune) ([]int, int, bool) {
	n, m := len(tl), len(q)
	if m == 0 {
		return nil, 0, true
	}
	if n < m {
		return nil, 0, false
	}

	arena := make([]chainNode, 0, m*4)
	bestScore := make([]int, m+1)
	bestOK := make([]bool, m+1)
	bestNode := make([]int, m+1)
	for i := range bestNode {
		bestNode[i] = -1
	}
	bestOK[0] = true

	for ti := range n {
		// Reverse order: a value written for qi+1 in this pass must not feed qi.
		for qi := m - 1; qi >= 0; qi-- {
			if tl[ti] != q[qi] || !bestOK[qi] {
				continue
			}
			prevLast := -1
			hasPrev := bestNode[qi] >= 0
			if hasPrev {
				prevLast = arena[bestNode[qi]].ti
				if ti <= prevLast {
					continue
				}
			}

			matchScore := 0
			if ti == 0 {
				matchScore += scoreStartOfString
			}
			if ti > 0 && ti < len(tc) {
				prev := tc[ti-1]
				switch {
				case isWordSeparator(prev):
					matchScore += scoreWordBoundary
				case unicode.IsLower(prev) && unicode.IsUpper(tc[ti]):
					matchScore += scoreCamelCase
				}
			}
			if hasPrev {
				if ti == prevLast+1 {
					matchScore += scoreConsecutive
				} else {
					gap := ti - prevLast - 1
					matchScore += scoreGapStartPenalty
					matchScore += scoreGapPenalty * max(gap-1, 0)
				}
			}

			newScore := bestScore[qi] + matchScore
			if !bestOK[qi+1] || newScore > bestScore[qi+1] {
				arena = append(arena, chainNode{ti: ti, prev: bestNode[qi]})
				bestScore[qi+1] = newScore
				bestOK[qi+1] = true
				bestNode[qi+1] = len(arena) - 1
			}
		}
	}

	if !bestOK[m] {
		return nil, 0, false
	}
	positions := make([]int, m)
	for idx, node := m-1, bestNode[m]; node >= 0 && idx >= 0; idx-- {
		positions[idx] = arena[node].ti
		node = arena[node].prev
	}
	return positions, bestScore[m], true
}

// findContiguousMatch returns the best-scoring literal occurrence of q in the
// target. The first occurrence wins ties, as in the Rust scorer.
func findContiguousMatch(q, tc, tl []rune) ([]int, int, bool) {
	m, n := len(q), len(tl)
	if m == 0 || m > n {
		return nil, 0, false
	}
	var best []int
	bestScore := 0
	for start := 0; start <= n-m; start++ {
		if !slices.Equal(tl[start:start+m], q) {
			continue
		}
		s := 0
		if start == 0 {
			s += scoreStartOfString
		}
		if start > 0 && start < len(tc) {
			prev := tc[start-1]
			switch {
			case isWordSeparator(prev):
				s += scoreWordBoundary
			case unicode.IsLower(prev) && unicode.IsUpper(tc[start]):
				s += scoreCamelCase
			}
		}
		s += scoreConsecutive * (m - 1)
		if best == nil || s > bestScore {
			best = make([]int, m)
			for i := range best {
				best[i] = start + i
			}
			bestScore = s
		}
	}
	return best, bestScore, best != nil
}

func isWordSeparator(r rune) bool {
	switch r {
	case ' ', '_', '-', '/', '.':
		return true
	}
	return false
}

func isConsecutive(positions []int) bool {
	for i := 1; i < len(positions); i++ {
		if positions[i] != positions[i-1]+1 {
			return false
		}
	}
	return true
}

func lastIndex(rs []rune, r rune) int {
	for i, r0 := range slices.Backward(rs) {
		if r0 == r {
			return i
		}
	}
	return -1
}
