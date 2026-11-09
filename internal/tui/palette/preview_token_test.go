package palette

import "testing"

// TestPreviewFromAnotherPaletteInstanceIsDropped: a late read from palette A must not be
// applied or cached by a newly opened palette B even when Owner, Seq and Rel collide.
func TestPreviewFromAnotherPaletteInstanceIsDropped(t *testing.T) {
	a := New(fileOptions("a.go", []string{"a.go"}, &fakePreviewer{}))
	b := New(fileOptions("a.go", []string{"a.go"}, &fakePreviewer{}))
	b.preview.seq = 1
	b.preview.rel = "a.go"
	b.preview.loading = true
	b.applyPreview(previewMsg{Token: a.token, Seq: 1, Rel: "a.go", Preview: Preview{Lines: []string{"leaked"}}})
	if b.preview.ready || b.preview.rel != "a.go" || len(b.preview.cache) != 0 {
		t.Fatalf("foreign preview applied: state %+v cache %v", b.preview, b.preview.cache)
	}
	// Dismissing retires the instance: a command carrying the old token is dropped even
	// if the same key/seq were seen again.
	oldToken := a.token
	oldSeq := a.preview.seq
	a.Dismiss()
	if a.token == oldToken || a.preview.seq <= oldSeq {
		t.Fatalf("Dismiss did not retire the preview generation: token %d seq %d", a.token, a.preview.seq)
	}
	a.preview.rel, a.preview.seq, a.preview.loading = "a.go", 1, true
	a.applyPreview(previewMsg{Token: oldToken, Seq: 1, Rel: "a.go", Preview: Preview{Lines: []string{"late"}}})
	if a.preview.ready || len(a.preview.cache) != 0 {
		t.Fatalf("dismissed palette applied a late preview: %+v", a.preview)
	}
}

// TestAsyncMessagesFromAnotherPaletteInstanceAreDropped extends the instance token to the
// file poll tick and Live Grep results: neither A's timer nor A's search may mutate B.
func TestAsyncMessagesFromAnotherPaletteInstanceAreDropped(t *testing.T) {
	a := New(fileOptions("a.go", []string{"a.go"}, &fakePreviewer{}))
	b := New(fileOptions("a.go", []string{"a.go"}, &fakePreviewer{}))
	if cmd := b.update(fileTickMsg{Token: a.token, Seq: b.seq}); cmd != nil {
		t.Fatalf("foreign file tick started a ranking command: %v", cmd)
	}
	if cmd := b.update(fileTickMsg{Token: b.token, Seq: b.seq}); cmd == nil {
		t.Fatal("own file tick started no ranking command")
	}

	liveA := New(liveOptions("todo", &fakeFiles{files: []string{"a.go"}}, &fakeGrepper{}))
	liveB := New(liveOptions("todo", &fakeFiles{files: []string{"a.go"}}, &fakeGrepper{}))
	hits := []GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}
	liveB.seq = 1
	liveB.applyGrep(GrepResultMsg{Token: liveA.token, Seq: 1, Text: "todo", Hits: hits})
	if len(liveB.rows) != 0 {
		t.Fatalf("foreign Live Grep result filled %d rows", len(liveB.rows))
	}
	liveB.applyGrep(GrepResultMsg{Token: liveB.token, Seq: 1, Text: "todo", Hits: hits})
	if len(liveB.rows) != 1 {
		t.Fatalf("own Live Grep result rows = %d, want 1", len(liveB.rows))
	}
}
