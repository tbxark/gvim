package explorer

import (
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// PatternMatcher reports whether rel matches any parsed wildcard pattern.
type PatternMatcher func(rel string, isDir bool) bool

// NewPatternMatcher returns a PatternMatcher for pats. If pats is empty, returns nil.
func NewPatternMatcher(pats []string) PatternMatcher {
	var rules []gitignore.Pattern
	for _, p := range pats {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		rules = append(rules, gitignore.ParsePattern(p, nil))
	}
	if len(rules) == 0 {
		return nil
	}
	m := gitignore.NewMatcher(rules)
	return func(rel string, isDir bool) bool {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || rel == "" {
			return false
		}
		parts := strings.Split(rel, "/")
		for i := 1; i <= len(parts); i++ {
			if m.Match(parts[:i], i < len(parts) || isDir) {
				return true
			}
		}
		return false
	}
}
