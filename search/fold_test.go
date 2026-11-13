package search

import (
	"reflect"
	"strings"
	"testing"
)

// TestFoldedLiteralMatchesTheRegexp checks that the folded search finds exactly the
// matches of the (?i) regexp it replaces, whole-word candidates and overlaps included.
func TestFoldedLiteralMatchesTheRegexp(t *testing.T) {
	t.Parallel()
	texts := []string{
		"Foo foo FOO fOo\nxfoo foox foo_ foo",
		"aaaa\nAAAA aA",
		"",
		"foo\n\nFOO\n",
		strings.Repeat("line of text\n", 50) + "NEEDLE\nneedle in a needlestack",
		"Kelvin K and k, ſ and s",
	}
	queries := []Query{
		{Text: "foo"}, {Text: "FOO", WholeWord: true}, {Text: "aa"}, {Text: "aA", WholeWord: true},
		{Text: "needle"}, {Text: "k"}, {Text: "S"}, {Text: "o\nf"}, {Text: ".*"},
	}
	for _, text := range texts {
		d := newWindow(0, text)
		for _, q := range queries {
			re, err := compile(q)
			if err != nil {
				t.Fatal(err)
			}
			got := d.locate(re, q)
			want := locateWithRegexp(d, re, q)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("locate(%+v) in %q = %v, want %v", q, text, got, want)
			}
		}
	}
}

// locateWithRegexp is the literal search of document.locate as it was before the
// folded search: every candidate comes from the regexp.
func locateWithRegexp(d document, re interface {
	FindStringIndex(string) []int
}, q Query) [][]int {
	var out [][]int
	for pos := 0; pos < len(d.text); {
		loc := re.FindStringIndex(d.text[pos:])
		if loc == nil {
			break
		}
		s, e := pos+loc[0], pos+loc[1]
		if !q.WholeWord || isWholeWord(d.text, s, e) {
			out = append(out, []int{s, e})
			pos = e
			continue
		}
		pos = s + 1
	}
	return out
}
