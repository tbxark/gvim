package explorer

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// gitignoreFile is the name of the per-directory ignore file.
const gitignoreFile = ".gitignore"

// IgnorePattern is one parsed .gitignore rule scoped to a directory domain
// (path segments relative to the workspace root, nil for the root itself).
type IgnorePattern struct {
	domain   []string
	segs     []string
	anchored bool
	dirOnly  bool
	negate   bool
	valid    bool
}

// IgnoreMatcher evaluates an ordered list of .gitignore patterns. Later
// patterns take precedence over earlier ones, and any ignored ancestor
// directory ignores everything beneath it.
type IgnoreMatcher struct {
	patterns []IgnorePattern
}

// NewIgnoreMatcher builds a matcher from patterns in evaluation order.
func NewIgnoreMatcher(patterns []IgnorePattern) *IgnoreMatcher {
	return &IgnoreMatcher{patterns: patterns}
}

// Match reports whether path (segments relative to the workspace root) is
// ignored. Any ignored ancestor directory ignores all entries beneath it.
func (m *IgnoreMatcher) Match(path []string, isDir bool) bool {
	if m == nil || len(m.patterns) == 0 || len(path) == 0 {
		return false
	}
	for k := 1; k < len(path); k++ {
		if m.matchEntry(path[:k], true) {
			return true
		}
	}
	return m.matchEntry(path, isDir)
}

func (m *IgnoreMatcher) matchEntry(path []string, isDir bool) bool {
	for i := len(m.patterns) - 1; i >= 0; i-- {
		if m.patterns[i].matches(path, isDir) {
			return !m.patterns[i].negate
		}
	}
	return false
}

func (p IgnorePattern) matches(path []string, isDir bool) bool {
	if !p.valid {
		return false
	}
	if p.dirOnly && !isDir {
		return false
	}
	if len(path) <= len(p.domain) {
		return false
	}
	for i, d := range p.domain {
		if path[i] != d {
			return false
		}
	}
	rel := path[len(p.domain):]
	if !p.anchored {
		return matchGlob(p.segs[0], rel[len(rel)-1])
	}
	return matchSegments(p.segs, rel)
}

// readIgnore parses the .gitignore in the directory dir below root, where dir
// is given as path segments relative to root (nil for the root itself). The
// patterns are scoped to dir, so they only match entries at or below it. A
// missing or unreadable file yields no patterns.
func readIgnore(root string, dir []string) []IgnorePattern {
	parts := make([]string, 0, len(dir)+2)
	parts = append(parts, root)
	parts = append(parts, dir...)
	parts = append(parts, gitignoreFile)
	data, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		return nil
	}
	return ParseIgnore(data, dir)
}

// ParseIgnore parses .gitignore text for the directory dir, given as path
// segments relative to the tree root (nil for the root itself), into patterns
// scoped to dir. Empty lines and #-comments are skipped. The remote explorer
// reads a remote .gitignore through the authority and calls this, so local and
// remote filtering share one parser (root, nested, negation and anchored
// patterns included).
func ParseIgnore(data []byte, dir []string) []IgnorePattern {
	var ps []IgnorePattern
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if p := ParseIgnorePattern(sc.Text(), dir); p.valid {
			ps = append(ps, p)
		}
	}
	return ps
}

// ParseIgnorePattern parses a single .gitignore line scoped to domain.
// Blank lines and unescaped #-comments return an invalid pattern that never
// matches.
func ParseIgnorePattern(line string, domain []string) IgnorePattern {
	line = strings.TrimRight(line, "\r")
	line = trimTrailingUnescapedSpaces(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return IgnorePattern{}
	}

	var p IgnorePattern
	if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
		if line == "" {
			return IgnorePattern{}
		}
	}
	if endsWithUnescapedSlash(line) {
		p.dirOnly = true
		for endsWithUnescapedSlash(line) {
			line = line[:len(line)-1]
		}
		if line == "" {
			return IgnorePattern{}
		}
	}
	if strings.HasPrefix(line, "/") {
		p.anchored = true
		line = strings.TrimLeft(line, "/")
		if line == "" {
			return IgnorePattern{}
		}
	}

	rawSegs := strings.Split(line, "/")
	if len(rawSegs) > 1 {
		p.anchored = true
	}
	segs := make([]string, 0, len(rawSegs))
	for _, seg := range rawSegs {
		if seg == "" {
			continue
		}
		if seg == "**" && len(segs) > 0 && segs[len(segs)-1] == "**" {
			continue
		}
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return IgnorePattern{}
	}
	if len(segs) == 1 && segs[0] == "**" {
		p.anchored = true
	}
	if len(domain) > 0 {
		p.domain = slices.Clone(domain)
	}
	p.segs = segs
	p.valid = true
	return p
}

func trimTrailingUnescapedSpaces(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		bs := 0
		for i := len(s) - 2; i >= 0 && s[i] == '\\'; i-- {
			bs++
		}
		if bs%2 == 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func endsWithUnescapedSlash(s string) bool {
	if len(s) == 0 || s[len(s)-1] != '/' {
		return false
	}
	bs := 0
	for i := len(s) - 2; i >= 0 && s[i] == '\\'; i-- {
		bs++
	}
	return bs%2 == 0
}

func matchSegments(pat, path []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				// Trailing "**" matches everything inside the prefix (at least one segment).
				return len(path) >= 1
			}
			for i := 0; i <= len(path); i++ {
				if matchSegments(pat[1:], path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 || !matchGlob(pat[0], path[0]) {
			return false
		}
		pat = pat[1:]
		path = path[1:]
	}
	return len(path) == 0
}

// matchGlob matches one path segment against a gitignore glob pattern
// supporting '*', '?', '[...]' character classes (with '!' or '^' negation),
// and '\' escapes.
func matchGlob(pat, s string) bool {
	pi, si := 0, 0
	starP, starS := -1, -1
	for si < len(s) {
		if pi < len(pat) {
			switch pat[pi] {
			case '*':
				starP = pi
				pi++
				starS = si
				continue
			case '?':
				_, w := utf8.DecodeRuneInString(s[si:])
				pi++
				si += w
				continue
			case '[':
				sr, sw := utf8.DecodeRuneInString(s[si:])
				if matched, consumed, ok := matchCharClass(pat[pi:], sr); ok {
					if matched {
						pi += consumed
						si += sw
						continue
					}
				} else if sr == '[' {
					pi++
					si += sw
					continue
				}
			case '\\':
				if pi+1 < len(pat) {
					pr, pw := utf8.DecodeRuneInString(pat[pi+1:])
					sr, sw := utf8.DecodeRuneInString(s[si:])
					if pr == sr {
						pi += 1 + pw
						si += sw
						continue
					}
				} else if s[si] == '\\' {
					pi++
					si++
					continue
				}
			default:
				pr, pw := utf8.DecodeRuneInString(pat[pi:])
				sr, sw := utf8.DecodeRuneInString(s[si:])
				if pr == sr {
					pi += pw
					si += sw
					continue
				}
			}
		}
		if starP >= 0 {
			_, w := utf8.DecodeRuneInString(s[starS:])
			starS += w
			si = starS
			pi = starP + 1
			continue
		}
		return false
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}

func matchCharClass(pat string, r rune) (matched bool, consumed int, ok bool) {
	if len(pat) < 2 || pat[0] != '[' {
		return false, 0, false
	}
	i := 1
	neg := false
	if i < len(pat) && (pat[i] == '!' || pat[i] == '^') {
		neg = true
		i++
	}
	first := true
	for i < len(pat) {
		if pat[i] == ']' && !first {
			if neg {
				matched = !matched
			}
			return matched, i + 1, true
		}
		first = false
		lo, next := decodeClassRune(pat, i)
		i = next
		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			hi, afterHi := decodeClassRune(pat, i+1)
			i = afterHi
			if lo <= r && r <= hi {
				matched = true
			}
		} else if r == lo {
			matched = true
		}
	}
	return false, 0, false
}

func decodeClassRune(pat string, i int) (rune, int) {
	if pat[i] == '\\' && i+1 < len(pat) {
		r, w := utf8.DecodeRuneInString(pat[i+1:])
		return r, i + 1 + w
	}
	r, w := utf8.DecodeRuneInString(pat[i:])
	return r, i + w
}
