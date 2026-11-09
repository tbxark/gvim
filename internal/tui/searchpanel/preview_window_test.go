package searchpanel

import (
	"testing"
)

// fakeWindowPreviewer records the window seam; Preview is a fallback that must not be
// called when the window seam is available.
type fakeWindowPreviewer struct {
	fakePreviewer
	windowCalls int
}

func (f *fakeWindowPreviewer) PreviewWindow(req PreviewRequest) (PreviewResult, error) {
	f.windowCalls++
	return PreviewResult{Start: req.Line - 1, Lines: []string{"window line"}, Total: 1, Window: true}, nil
}

func TestPreviewPrefersTheWindowSeam(t *testing.T) {
	t.Parallel()
	pv := &fakeWindowPreviewer{fakePreviewer: fakePreviewer{}}
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 1,
		Matches: []Match{{Line: 2, Col: 3, Len: 3, Text: "func foo() {"}},
	}}}}
	o := searchOptions(s)
	o.Previewer = pv
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	if pv.windowCalls != 1 || len(pv.reqs) != 0 {
		t.Fatalf("window calls = %d, fallback calls = %d; want only the window seam", pv.windowCalls, len(pv.reqs))
	}
	if !hasRow(draw(p, 100, 30), "window line") {
		t.Fatal("the window preview was not painted")
	}
}
