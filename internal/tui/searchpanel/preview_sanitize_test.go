package searchpanel

import (
	"strings"
	"testing"
)

// TestPreviewSanitizesC1AndFormatText: the preview column never paints C1 controls, bidi
// overrides, zero-width format characters or a BOM; the match highlight is remapped onto
// the cleaned text instead of shifting off the readable query.
func TestPreviewSanitizesC1AndFormatText(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{
		"x",
		"func \u202Efoo\u0080()\u200B {",
		"y",
	}}}
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 1,
		Matches: []Match{{Line: 2, Col: 7, Len: 3, Text: "func foo() {"}},
	}}}}
	o := searchOptions(s)
	o.Previewer = pv
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	g := draw(p, 100, 30)
	plain := strings.Join(g.Plain(), "\n")
	if strings.ContainsAny(plain, "\u202e\u0080\u200b\ufeff") {
		t.Fatalf("preview painted spoof bytes: %q", plain)
	}
	if !hasRow(g, "func foo() {") {
		t.Fatalf("readable preview text missing:\n%s", g)
	}
	// The match on the raw line starts after the removed bidi rune; the old col would
	// bold the wrong cell. Assert the bold run lands on "foo".
	geo := previewGeo(p)
	y := geo.resultsTop + 1 + 1
	start := previewTextStart(geo, 2)
	for i := 5; i < 8; i++ {
		if !g.At(start+i, y).Style.Bold {
			t.Fatalf("match rune %d is not bold at %d,%d", i, start+i, y)
		}
	}
	if g.At(start+4, y).Style.Bold || g.At(start+8, y).Style.Bold {
		t.Fatalf("highlight runs past the match")
	}
}

// TestMatchTextSanitizesFormatRunes: a result row never carries bidi or zero-width
// runes of the matched line, and its bold span stays on the readable match.
func TestMatchTextSanitizesFormatRunes(t *testing.T) {
	t.Parallel()
	text, from, to := matchText(Match{Line: 1, Col: 7, Len: 3, Text: "func \u202efoo\u200b() {"})
	if text != "func foo() {" {
		t.Fatalf("text = %q", text)
	}
	if got := text[from:to]; got != "foo" {
		t.Fatalf("match span = %q (%d..%d), want foo", got, from, to)
	}
}
