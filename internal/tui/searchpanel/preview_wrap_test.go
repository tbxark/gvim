package searchpanel

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// previewWrapPanel is previewPanel with the preview soft wrap on from the start.
func previewWrapPanel(t *testing.T, pv *fakePreviewer) (*Panel, *fakeSearcher) {
	t.Helper()
	s := &fakeSearcher{result: sampleResult()}
	o := searchOptions(s)
	o.Previewer = pv
	o.PreviewWrap = true
	p := New(o)
	search(t, p, "foo")
	return p, s
}

// previewWrapWidth returns the cells a wrapped preview row has for text, and the
// geometry. The line-number gutter is one digit for the small fixtures used here.
func previewWrapWidth(t *testing.T, p *Panel) (geometry, int) {
	t.Helper()
	geo := previewGeo(p)
	if !geo.preview {
		t.Fatal("no preview column")
	}
	width := geo.prevEnd - (geo.prevX + 1 + 3)
	if width < 1 {
		t.Fatalf("preview text width = %d", width)
	}
	return geo, width
}

// probeWrapWidth builds a panel with a simple preview and returns the wrapped row width.
func probeWrapWidth(t *testing.T) int {
	t.Helper()
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"x"}}}
	p, _ := previewWrapPanel(t, pv)
	focusResultsFeeding(t, p)
	_, width := previewWrapWidth(t, p)
	return width
}

// previewTextRow returns the cells of one preview row as text, starting after its gutter.
func previewTextRow(g *grid.Grid, geo geometry, gutter, y int) string {
	start := geo.prevX + gutter + 3
	var b strings.Builder
	for x := start; x < geo.prevEnd; x++ {
		b.WriteString(g.At(x, y).Text)
	}
	return strings.TrimRight(b.String(), " ")
}

// TestPreviewWrapWrapsLongLinesWithSpans checks the soft-wrapped preview: a line wider
// than the column continues on the next display rows, the syntax spans stay on their
// runes across the wrap, and the continuation row has no line number.
func TestPreviewWrapWrapsLongLinesWithSpans(t *testing.T) {
	t.Parallel()
	width := probeWrapWidth(t)
	long := strings.Repeat("ab", width) // two wrapped rows
	pv := &fakePreviewer{result: PreviewResult{
		Start: 0,
		Lines: []string{"before", long},
		Spans: []PreviewSpan{{Line: 1, Start: width + 2, End: width + 6, Kind: "keyword"}},
	}}
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 9,
		Matches: []Match{{Line: 2, Col: 3, Len: 3, Text: long}},
	}}}}
	o := searchOptions(s)
	o.Previewer = pv
	o.PreviewWrap = true
	o.SyntaxStyles = map[string]grid.Style{"keyword": {FG: color.RGBA{R: 1, A: 255}}}
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	g := draw(p, 100, 30)
	geo, _ := previewWrapWidth(t, p)
	gutter := 1
	firstRow := geo.resultsTop + 1
	row0 := previewTextRow(g, geo, gutter, firstRow+1)
	row1 := previewTextRow(g, geo, gutter, firstRow+2)
	if row0+row1 != long {
		t.Fatalf("wrapped rows = %q + %q, want the whole line in order", row0, row1)
	}
	start := geo.prevX + gutter + 3
	if got := g.At(start+2, firstRow+2).Style.FG; got != (color.RGBA{R: 1, A: 255}) {
		t.Fatalf("span cell on the wrapped row fg = %v, want the keyword style", got)
	}
	if got := g.At(start, firstRow+2).Style.FG; got == (color.RGBA{R: 1, A: 255}) {
		t.Fatal("the cell before the span has the keyword style")
	}
	for x := geo.prevX; x < start; x++ {
		if txt := g.At(x, firstRow+2).Text; txt != " " && txt != "│" {
			t.Fatalf("continuation row gutter cell %d = %q, want blanks", x, txt)
		}
	}
}

// TestPreviewWrapCapsLongLinesAtTheRowCap checks the bounded wrap: a very long line
// takes at most paintPreviewMaxRows display rows, the last one ending in an ellipsis,
// and the line after it still paints.
func TestPreviewWrapCapsLongLinesAtTheRowCap(t *testing.T) {
	t.Parallel()
	width := probeWrapWidth(t)
	huge := strings.Repeat("x", width*(paintPreviewMaxRows+1))
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"before", huge, "after"}}}
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 9,
		Matches: []Match{{Line: 2, Col: 1, Len: 1, Text: huge}},
	}}}}
	o := searchOptions(s)
	o.Previewer = pv
	o.PreviewWrap = true
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	g := draw(p, 100, 30)
	geo, _ := previewWrapWidth(t, p)
	gutter := 1
	firstRow := geo.resultsTop + 1 + 1 // "before" then the huge match line
	lastCapped := firstRow + paintPreviewMaxRows - 1
	if lastCapped >= geo.resultsTop+geo.results {
		t.Fatalf("the preview window is too short for the row-cap test (%d rows)", geo.results)
	}
	if got := previewTextRow(g, geo, gutter, lastCapped); !strings.HasSuffix(got, "…") {
		t.Fatalf("capped row = %q, want the ellipsis", got)
	}
	if got := previewTextRow(g, geo, gutter, lastCapped+1); !strings.HasPrefix(got, "after") {
		t.Fatalf("row after the capped line = %q, want the next file line", got)
	}
}

// TestPreviewWrapToggleAndSelection checks the Alt+P toggle: wrap turns on per display
// row and off back to clipping; the match line's wrapped rows both carry the selected
// background and the matched runes continue bold.
func TestPreviewWrapToggleAndSelection(t *testing.T) {
	t.Parallel()
	width := probeWrapWidth(t)
	long := strings.Repeat("z", width+10)
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"before", long}}}
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 9,
		Matches: []Match{{Line: 2, Col: 1, Len: width + 2, Text: long}},
	}}}}
	o := searchOptions(s)
	o.Previewer = pv
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	gutter := 1
	// Wrap off (the default): the long line is clipped to one row.
	g := draw(p, 100, 30)
	geo, _ := previewWrapWidth(t, p)
	firstRow := geo.resultsTop + 1
	if got := previewTextRow(g, geo, gutter, firstRow+2); got != "" {
		t.Fatalf("row after the clipped match line = %q, want nothing (clipped)", got)
	}
	// Turn wrap on: the same line now takes a second display row, both selected and bold.
	p.Update(altKey('p'))
	if !p.PreviewWrap() {
		t.Fatal("Alt+P did not turn wrap on")
	}
	g = draw(p, 100, 30)
	if got := previewTextRow(g, geo, gutter, firstRow+2); got == "" {
		t.Fatal("the wrapped preview has no second row")
	}
	for _, y := range []int{firstRow + 1, firstRow + 2} {
		if got, want := g.At(geo.prevX, y).Style.BG, p.styles.selected.BG; got != want {
			t.Fatalf("wrapped match row %d background = %v, want %v", y, got, want)
		}
	}
	start := geo.prevX + gutter + 3
	if !g.At(start, firstRow+2).Style.Bold {
		t.Fatal("the match continues bold on the second wrapped row")
	}
	// Toggle off: clipped again.
	p.Update(altKey('p'))
	if p.PreviewWrap() {
		t.Fatal("Alt+P did not turn wrap off")
	}
	g = draw(p, 100, 30)
	if got := previewTextRow(g, geo, gutter, firstRow+2); got != "" {
		t.Fatalf("row after the clipped match line = %q after turning wrap off", got)
	}
}
