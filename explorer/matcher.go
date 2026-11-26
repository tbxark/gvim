package explorer

import (
	"path/filepath"
	"strings"
)

// PatternMatcher reports whether rel matches any parsed wildcard pattern.
type PatternMatcher func(rel string, isDir bool) bool

// ParsePatterns parses pats as gitignore-style patterns relative to the root:
// blank lines and #-comments are skipped. A pattern without a slash matches at
// any depth; one with a slash is anchored at the root.
func ParsePatterns(pats []string) []IgnorePattern {
	var rules []IgnorePattern
	for _, p := range pats {
		p = strings.TrimSpace(p)
		if pat := ParseIgnorePattern(p, nil); pat.valid {
			rules = append(rules, pat)
		}
	}
	return rules
}

// patternMatcherFor returns pats as a matcher, or nil when none parses.
func patternMatcherFor(pats []string) *IgnoreMatcher {
	if rules := ParsePatterns(pats); len(rules) > 0 {
		return NewIgnoreMatcher(rules)
	}
	return nil
}

// matchPrefixes reports whether parts — the path segments below the root — match
// m themselves or through a matching ancestor directory, so a directory-only
// pattern such as "build/" excludes the whole subtree.
func matchPrefixes(m *IgnoreMatcher, parts []string, isDir bool) bool {
	return m.Match(parts, isDir)
}

// NewPatternMatcher returns a PatternMatcher for pats. If pats is empty, returns nil.
func NewPatternMatcher(pats []string) PatternMatcher {
	m := patternMatcherFor(pats)
	if m == nil {
		return nil
	}
	return func(rel string, isDir bool) bool {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || rel == "" {
			return false
		}
		return matchPrefixes(m, strings.Split(rel, "/"), isDir)
	}
}

// scanMatchers is the file_scan_exclusions/file_scan_inclusions pair as
// matchers: an exclusion match drops an entry everywhere, an inclusion match
// clears a .gitignore match. Exclusions beat inclusions.
type scanMatchers struct {
	excl *IgnoreMatcher
	incl *IgnoreMatcher
}

// scanMatchersFor builds the matchers for the file_scan_exclusions and
// file_scan_inclusions pattern lists.
func scanMatchersFor(exclusions, inclusions []string) scanMatchers {
	return scanMatchers{excl: patternMatcherFor(exclusions), incl: patternMatcherFor(inclusions)}
}

// excluded reports whether the path parts below the root match a scan exclusion.
func (s scanMatchers) excluded(parts []string, isDir bool) bool {
	return s.excl != nil && matchPrefixes(s.excl, parts, isDir)
}

// included reports whether the path parts below the root match a scan inclusion.
func (s scanMatchers) included(parts []string, isDir bool) bool {
	return s.incl != nil && matchPrefixes(s.incl, parts, isDir)
}

// hasInclusions reports whether any inclusion pattern is configured, which is
// also the licence to descend into an ignored directory looking for matches.
func (s scanMatchers) hasInclusions() bool { return s.incl != nil }

// ScanMatcher is the exported form of scanMatchers for listings outside this
// package (the finder's git fast path, remote directory loaders). It matches
// root-relative paths; callers split on the path separator of their filesystem.
type ScanMatcher struct{ s scanMatchers }

// NewScanMatcher builds the file_scan_exclusions/file_scan_inclusions matchers.
func NewScanMatcher(exclusions, inclusions []string) ScanMatcher {
	return ScanMatcher{s: scanMatchersFor(exclusions, inclusions)}
}

// Excluded reports whether parts — the path segments below the root — match a
// scan exclusion, directly or through a matching ancestor directory.
func (m ScanMatcher) Excluded(parts []string, isDir bool) bool {
	return m.s.excluded(parts, isDir)
}

// ExcludedPath is Excluded for a slash-separated relative path.
func (m ScanMatcher) ExcludedPath(rel string, isDir bool) bool {
	return m.Excluded(relParts(rel), isDir)
}

// Included reports whether parts match a scan inclusion the same way.
func (m ScanMatcher) Included(parts []string, isDir bool) bool {
	return m.s.included(parts, isDir)
}

// IncludedPath is Included for a slash-separated relative path.
func (m ScanMatcher) IncludedPath(rel string, isDir bool) bool {
	return m.Included(relParts(rel), isDir)
}

// HasInclusions reports whether any inclusion pattern is configured.
func (m ScanMatcher) HasInclusions() bool { return m.s.hasInclusions() }

// HasExclusions reports whether any exclusion pattern is configured.
func (m ScanMatcher) HasExclusions() bool { return m.s.excl != nil }

// relParts splits a slash-separated relative path, nil for "." or "".
func relParts(rel string) []string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" {
		return nil
	}
	return strings.Split(rel, "/")
}
