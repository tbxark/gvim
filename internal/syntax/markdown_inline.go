package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// link reads a link or an image whose "[" is at i. The text is painted as a string and the
// destination as a link. It returns where reading goes on and whether it found one.
func (p *mdInline) link(i, hi int, base Kind, flush func(int), seg *int) (int, bool) {
	s := p.s
	closeB := matchBracket(s, i, hi)
	if closeB < 0 || closeB+1 >= hi {
		return 0, false
	}
	after := closeB + 1
	switch s[after] {
	case '(':
		closeP := matchParen(s, after, hi)
		if closeP < 0 {
			return 0, false
		}
		flush(i)
		p.emit(i, i+1, base)
		p.parse(i+1, closeB, KindString)
		p.emit(closeB, after+1, base)
		dest := s[after+1 : closeP]
		ds := after + 1 + leadingBlanks(dest)
		de := ds
		for de < closeP && s[de] != ' ' && s[de] != '\t' {
			de++
		}
		p.emit(ds, de, KindLink)
		if strings.TrimSpace(s[de:closeP]) != "" {
			p.emit(de, closeP, KindString)
		}
		p.emit(closeP, closeP+1, base)
		*seg = closeP + 1
		return closeP + 1, true
	case '[':
		closeR := strings.IndexByte(s[after+1:hi], ']')
		if closeR < 0 {
			return 0, false
		}
		closeR += after + 1
		flush(i)
		p.emit(i, i+1, base)
		p.parse(i+1, closeB, KindString)
		p.emit(closeB, after+1, base)
		p.emit(after+1, closeR, KindLink)
		p.emit(closeR, closeR+1, base)
		*seg = closeR + 1
		return closeR + 1, true
	}
	return 0, false
}

// matchBracket returns the index of the "]" that closes the "[" at i, or -1. Brackets
// nest, and an escaped one and one in a code span do not count.
func matchBracket(s string, i, hi int) int {
	depth := 0
	for j := i; j < hi; j++ {
		switch s[j] {
		case '\\':
			j++
		case '`':
			n := runLength(s, j, hi, '`')
			if k := findRun(s, j+n, hi, '`', n); k >= 0 {
				j = k + n - 1
			} else {
				j += n - 1
			}
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// matchParen returns the index of the ")" that closes the "(" at i, or -1.
func matchParen(s string, i, hi int) int {
	depth := 0
	for j := i; j < hi; j++ {
		switch s[j] {
		case '\\':
			j++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// emphasis reads the emphasis or strong text that opens with the run of "*" or "_" at i. It
// finds the closing run by the flanking rules of CommonMark, paints the marks and the text
// with the kind of the nesting, and returns where reading goes on.
func (p *mdInline) emphasis(i, hi int, base Kind, flush func(int), seg *int) (int, bool) {
	s := p.s
	c := s[i]
	n := runLength(s, i, hi, c)
	if open, _ := flanking(s, i, i+n, hi, c); !open {
		return 0, false
	}
	j := closingRun(s, i, n, hi, c)
	if j < 0 {
		return 0, false
	}
	m := runLength(s, j, hi, c)
	use := min(n, m, 3)
	kind := combine(base, use)
	flush(i)
	p.emit(i, i+n-use, base)
	p.emit(i+n-use, i+n, kind)
	p.parse(i+n, j, kind)
	p.emit(j, j+use, kind)
	*seg = j + use
	return j + use, true
}

// closingRun returns the index of the run of c that closes the run of n at i, or -1. A run
// that can only open starts a nested span, which is skipped with its own closer, so
// that "**a *b* c**" is closed by the last run and not by the "*" after "b".
func closingRun(s string, i, n, hi int, c byte) int {
	for j := i + n; j < hi; {
		switch s[j] {
		case '\\':
			j += 2
			continue
		case '`':
			m := runLength(s, j, hi, '`')
			if k := findRun(s, j+m, hi, '`', m); k >= 0 {
				j = k + m
			} else {
				j += m
			}
			continue
		case c:
			m := runLength(s, j, hi, c)
			opens, closes := flanking(s, j, j+m, hi, c)
			if closes && j > i+n {
				return j
			}
			if opens {
				if k := closingRun(s, j, m, hi, c); k >= 0 {
					j = k + min(m, runLength(s, k, hi, c))
					continue
				}
			}
			j += m
			continue
		}
		j++
	}
	return -1
}

// combine returns the kind of text inside marks of the given strength, within text of kind
// base: one mark is emphasis, two are strong, three are both, and emphasis in strong text
// or the other way round is both.
func combine(base Kind, strength int) Kind {
	switch strength {
	case 1:
		if base == KindStrong || base == KindStrongEmphasis {
			return KindStrongEmphasis
		}
		return KindEmphasis
	case 2:
		if base == KindEmphasis || base == KindStrongEmphasis {
			return KindStrongEmphasis
		}
		return KindStrong
	}
	return KindStrongEmphasis
}

// flanking reports whether the run of c in s[start:end] can open and can close emphasis,
// by the rules of CommonMark. A run at either edge of the text counts as next to a blank.
func flanking(s string, start, end, hi int, c byte) (opens, closes bool) {
	before, after := ' ', ' '
	if start > 0 {
		before, _ = utf8.DecodeLastRuneInString(s[:start])
	}
	if end < hi {
		after, _ = utf8.DecodeRuneInString(s[end:])
	}
	beforeSpace, afterSpace := unicode.IsSpace(before), unicode.IsSpace(after)
	beforePunct, afterPunct := isPunct(before), isPunct(after)
	left := !afterSpace && (!afterPunct || beforeSpace || beforePunct)
	right := !beforeSpace && (!beforePunct || afterSpace || afterPunct)
	if c == '_' {
		return left && (!right || beforePunct), right && (!left || afterPunct)
	}
	return left, right
}

func isPunct(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

func isASCIIPunct(b byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", b) >= 0
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

// runLength is the number of c bytes in s[i:hi] from i on.
func runLength(s string, i, hi int, c byte) int {
	n := 0
	for i+n < hi && s[i+n] == c {
		n++
	}
	return n
}

// findRun returns the index at or after from of a run of exactly n bytes c, or -1.
func findRun(s string, from, hi int, c byte, n int) int {
	for j := from; j < hi; {
		if s[j] != c {
			j++
			continue
		}
		m := runLength(s, j, hi, c)
		if m == n {
			return j
		}
		j += m
	}
	return -1
}

// inlineMark is an inline construct that starts with "<": how far it runs, and its kind.
type inlineMark struct {
	end  int
	kind Kind
}

// inlineHTML returns the HTML comment, autolink or tag that s begins with. end is 0 when
// there is none.
func inlineHTML(s string) inlineMark {
	if strings.HasPrefix(s, "<!--") {
		if k := strings.Index(s, "-->"); k >= 0 {
			return inlineMark{k + 3, KindComment}
		}
		return inlineMark{len(s), KindComment}
	}
	if m := mdAutolink.FindString(s); m != "" {
		return inlineMark{len(m), KindLink}
	}
	if m := mdHTMLTag.FindString(s); m != "" {
		return inlineMark{len(m), KindKeyword}
	}
	return inlineMark{}
}

// bareURLLength returns the length of the URL s begins with, without the punctuation that
// ends a sentence and a closing bracket that has no opening one.
func bareURLLength(s string) int {
	m := mdBareURL.FindString(s)
	for m != "" {
		last := m[len(m)-1]
		switch {
		case strings.IndexByte(".,:;!?*_~'\"", last) >= 0:
		case last == ')' && strings.Count(m, ")") > strings.Count(m, "("):
		default:
			if m == "www." || strings.HasSuffix(m, "://") {
				return 0
			}
			return len(m)
		}
		m = m[:len(m)-1]
	}
	return 0
}
