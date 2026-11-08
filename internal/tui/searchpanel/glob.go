package searchpanel

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// globMatcher is the panel's parsed file-glob filter. Patterns are comma or space
// separated; a pattern without a slash matches the file's basename at any depth, and a
// pattern with a slash matches the whole root-relative slash path. In a pattern "*" and
// "?" do not cross a slash, and "**" does: "*.go" and "src/**/test_*.go" both work. The
// empty glob matches every file.
type globMatcher struct {
	patterns []globPattern
}

// globPattern is one compiled pattern.
type globPattern struct {
	re *regexp.Regexp
	// path is true when the pattern names a path (it has a slash), so it is matched
	// against the whole relative path; a basename pattern is matched against the last
	// segment only.
	path bool
}

// parseGlob compiles a glob filter. It returns a nil matcher for an empty glob, which
// matches every file.
func parseGlob(glob string) (*globMatcher, error) {
	fields := strings.FieldsFunc(glob, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(fields) == 0 {
		return nil, nil
	}
	m := &globMatcher{patterns: make([]globPattern, 0, len(fields))}
	for _, field := range fields {
		p, err := compileGlob(field)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", field, err)
		}
		m.patterns = append(m.patterns, p)
	}
	return m, nil
}

// compileGlob compiles one pattern: a basename pattern when it has no slash, otherwise a
// whole-path pattern.
func compileGlob(pattern string) (globPattern, error) {
	p := globPattern{path: strings.Contains(pattern, "/")}
	body, err := globRegexp(pattern)
	if err != nil {
		return globPattern{}, err
	}
	expr := "^" + body + "$"
	if !p.path {
		// A basename pattern matches the last path segment at any depth.
		expr = "^(?:.*/)?" + body + "$"
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return globPattern{}, err
	}
	p.re = re
	return p, nil
}

// globRegexp translates a glob pattern to a regular expression: "**" crosses slashes,
// "*" and "?" do not, a "[...]" class is a character class, and everything else is
// literal. A "**/" may match no directory at all, so "**/test_*.go" matches "test_x.go"
// too. An unterminated class is an error.
func globRegexp(pattern string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			i++
			if i+1 < len(pattern) && pattern[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?")
				continue
			}
			b.WriteString(".*")
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				return "", fmt.Errorf("missing ]")
			}
			class := pattern[i+1 : i+1+end]
			if class == "" || class == "^" || class == "!" {
				return "", fmt.Errorf("empty character class")
			}
			b.WriteString("[")
			for k := 0; k < len(class); k++ {
				switch ch := class[k]; ch {
				case '\\', ']':
					b.WriteByte('\\')
					b.WriteByte(ch)
				default:
					b.WriteByte(ch)
				}
			}
			b.WriteString("]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String(), nil
}

// match reports whether the root-relative slash path rel matches the filter. It is
// called on a nil matcher too: no filter keeps every file.
func (m *globMatcher) match(rel string) bool {
	if m == nil {
		return true
	}
	rel = strings.TrimPrefix(rel, "./")
	base := path.Base(rel)
	for _, p := range m.patterns {
		target := rel
		if !p.path {
			target = base
		}
		if p.re.MatchString(target) {
			return true
		}
	}
	return false
}

// filter returns the files of rels that the matcher keeps. A nil matcher keeps every
// file.
func (m *globMatcher) filter(rels []string) []string {
	if m == nil {
		return rels
	}
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		if m.match(rel) {
			out = append(out, rel)
		}
	}
	return out
}
