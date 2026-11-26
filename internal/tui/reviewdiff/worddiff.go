package reviewdiff

import (
	"unicode"
	"unicode/utf8"
)

// Word-diff bounds keep inline refinement fast even on minified or generated lines.
const (
	maxWordTokens  = 256
	maxWordDPCells = 65536
)

type wordToken struct {
	text  string
	start int
	end   int
}

// WordDiffRanges returns the changed byte ranges on oldLine and newLine for a
// paired modified line. Equal lines return (nil, nil). Whitespace-only tokens
// are included when adjacent to a changed span or skipped when isolated.
// When either line has too many tokens for token LCS, it falls back to common
// prefix/suffix trimming so inline refinement always stays bounded.
func WordDiffRanges(oldLine, newLine string) (oldRanges, newRanges []ByteRange) {
	if oldLine == newLine {
		return nil, nil
	}
	if oldLine == "" {
		return nil, []ByteRange{{Start: 0, End: len(newLine)}}
	}
	if newLine == "" {
		return []ByteRange{{Start: 0, End: len(oldLine)}}, nil
	}
	oldToks := tokenizeLine(oldLine)
	newToks := tokenizeLine(newLine)
	m, n := len(oldToks), len(newToks)
	if m > maxWordTokens || n > maxWordTokens || (m+1)*(n+1) > maxWordDPCells {
		return prefixSuffixDiff(oldLine, newLine)
	}

	// Trim matching leading and trailing tokens to shrink the DP table.
	pre := 0
	for pre < m && pre < n && oldToks[pre].text == newToks[pre].text {
		pre++
	}
	om, nm := m, n
	for om > pre && nm > pre && oldToks[om-1].text == newToks[nm-1].text {
		om--
		nm--
	}
	am, bn := om-pre, nm-pre
	if am == 0 {
		return nil, collapseTokens(newToks[pre:nm])
	}
	if bn == 0 {
		return collapseTokens(oldToks[pre:om]), nil
	}

	stride := bn + 1
	dp := make([]int, (am+1)*stride)
	for i := 1; i <= am; i++ {
		ot := oldToks[pre+i-1].text
		row := i * stride
		prev := (i - 1) * stride
		for j := 1; j <= bn; j++ {
			if ot == newToks[pre+j-1].text {
				dp[row+j] = dp[prev+j-1] + 1
			} else if dp[prev+j] >= dp[row+j-1] {
				dp[row+j] = dp[prev+j]
			} else {
				dp[row+j] = dp[row+j-1]
			}
		}
	}

	matchedOld := make([]bool, am)
	matchedNew := make([]bool, bn)
	i, j := am, bn
	for i > 0 && j > 0 {
		if oldToks[pre+i-1].text == newToks[pre+j-1].text {
			matchedOld[i-1] = true
			matchedNew[j-1] = true
			i--
			j--
		} else if dp[(i-1)*stride+j] >= dp[i*stride+j-1] {
			i--
		} else {
			j--
		}
	}

	return unmatchedRanges(oldToks[pre:om], matchedOld), unmatchedRanges(newToks[pre:nm], matchedNew)
}

func tokenizeLine(s string) []wordToken {
	var out []wordToken
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		j := i + sz
		switch {
		case unicode.IsSpace(r):
			for j < len(s) {
				r2, sz2 := utf8.DecodeRuneInString(s[j:])
				if !unicode.IsSpace(r2) {
					break
				}
				j += sz2
			}
		case isWordRune(r):
			for j < len(s) {
				r2, sz2 := utf8.DecodeRuneInString(s[j:])
				if !isWordRune(r2) {
					break
				}
				j += sz2
			}
		}
		out = append(out, wordToken{text: s[i:j], start: i, end: j})
		i = j
	}
	return out
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func collapseTokens(toks []wordToken) []ByteRange {
	if len(toks) == 0 {
		return nil
	}
	out := make([]ByteRange, 0, len(toks))
	for _, tk := range toks {
		if n := len(out); n > 0 && out[n-1].End == tk.start {
			out[n-1].End = tk.end
		} else {
			out = append(out, ByteRange{Start: tk.start, End: tk.end})
		}
	}
	return out
}

func unmatchedRanges(toks []wordToken, matched []bool) []ByteRange {
	var unmatched []wordToken
	for i, tk := range toks {
		if !matched[i] {
			unmatched = append(unmatched, tk)
		}
	}
	return collapseTokens(unmatched)
}

func prefixSuffixDiff(a, b string) ([]ByteRange, []ByteRange) {
	// Common UTF-8 rune prefix.
	pa, pb := 0, 0
	for pa < len(a) && pb < len(b) {
		ra, sza := utf8.DecodeRuneInString(a[pa:])
		rb, szb := utf8.DecodeRuneInString(b[pb:])
		if ra != rb {
			break
		}
		pa += sza
		pb += szb
	}
	// Common UTF-8 rune suffix without overlapping the prefix.
	ea, eb := len(a), len(b)
	for ea > pa && eb > pb {
		ra, sza := utf8.DecodeLastRuneInString(a[pa:ea])
		rb, szb := utf8.DecodeLastRuneInString(b[pb:eb])
		if ra != rb {
			break
		}
		ea -= sza
		eb -= szb
	}
	var or, nr []ByteRange
	if ea > pa {
		or = []ByteRange{{Start: pa, End: ea}}
	}
	if eb > pb {
		nr = []ByteRange{{Start: pb, End: eb}}
	}
	return or, nr
}
