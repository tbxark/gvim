package palette

import "testing"

// TestInvalidatePreviewsDropsCacheAndDisplay: a remote carrier change must not keep a
// pre-disconnect preview painted or cached; the next selection reads again.
func TestInvalidatePreviewsDropsCacheAndDisplay(t *testing.T) {
	p := New(Options{})
	p.preview.cache = map[string]Preview{"a.go": {Lines: []string{"old"}}}
	p.preview.order = []string{"a.go"}
	p.preview.seq = 5
	p.preview.rel, p.preview.target, p.preview.ready, p.preview.loading = "a.go", 1, true, false
	p.InvalidatePreviews()
	if p.preview.cache != nil || p.preview.order != nil || p.preview.rel != "" || p.preview.ready || p.preview.loading {
		t.Fatalf("preview after invalidation = %+v cache %v order %v", p.preview, p.preview.cache, p.preview.order)
	}
	if p.preview.seq != 6 {
		t.Fatalf("preview seq = %d, want a bumped generation", p.preview.seq)
	}
	// Even if the cleared key is reused, a command from the old generation must not paint.
	p.preview.rel = "a.go"
	p.applyPreview(previewMsg{Seq: 5, Rel: "a.go", Preview: Preview{Lines: []string{"stale"}}})
	if p.preview.ready || len(p.preview.data.Lines) != 0 {
		t.Fatalf("stale preview applied after invalidation: %+v", p.preview.data)
	}
}
