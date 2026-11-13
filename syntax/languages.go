package syntax

import (
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
)

// maxLanguageName is longer than every lexer name and alias of chroma. lexers.Get
// matches a name against every file name glob, which costs time in proportion to
// its length, so Lookup refuses a longer name before the search.
const maxLanguageName = 32

// Languages returns the names of the languages that have a lexer, sorted without
// regard to case. The plain-text lexers are left out: plain text is the absence of
// a language. A name from the list is accepted by Lookup.
func Languages() []string {
	var names []string
	for _, n := range lexers.Names(false) {
		if !IsPlain(n) {
			names = append(names, n)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return names
}

// Lookup returns the name of the language that name, or one of its aliases, stands
// for, as Languages spells it. ok is false for a name no lexer answers to and for
// the plain-text names.
func Lookup(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxLanguageName {
		return "", false
	}
	lx := lexers.Get(name)
	if lx == nil {
		return "", false
	}
	canonical := lx.Config().Name
	if canonical == "" || IsPlain(canonical) {
		return "", false
	}
	return canonical, true
}
