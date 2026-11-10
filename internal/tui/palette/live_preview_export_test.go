package palette

import (
	"context"
	"strings"
	"testing"
)

// liveWindowPreviewer is a Previewer + WindowPreviewer for Live Grep preview tests: it
// records the contexts it was called with, so a test can prove the palette cancels the
// old window read when the selection moves.
type liveWindowPreviewer struct {
	data      string
	firstLine int
	truncated bool
	contexts  []context.Context
	paths     []string
	reads     int
}

func (p *liveWindowPreviewer) ReadHead(string, int) ([]byte, bool, error) {
	return []byte(p.data), p.truncated, nil
}

func (p *liveWindowPreviewer) ReadWindow(ctx context.Context, path string, _ int, _ int) ([]byte, int, bool, error) {
	p.contexts = append(p.contexts, ctx)
	p.paths = append(p.paths, path)
	p.reads++
	return []byte(p.data), p.firstLine, p.truncated, nil
}

func livePreviewPalette(pv Previewer) *Palette {
	return New(Options{
		Live:    true,
		Root:    "/w",
		Files:   &fakeFiles{files: []string{"a.go", "b.go"}},
		Grep:    &fakeGrepper{},
		Preview: pv,
	})
}

// TestLiveGrepRowPreviewReadsWindowAndShowsTarget: a selected file hit reads a bounded
// line window around the match in a command, paints it with the correct line numbers,
// and previews only file hits (the buffer row has no column).
func TestLiveGrepRowPreviewReadsWindowAndShowsTarget(t *testing.T) {
	t.Parallel()
	pv := &liveWindowPreviewer{data: "six\nseven\nfunc todo() {}\nnine\nten\n", firstLine: 6}
	p := livePreviewPalette(pv)
	draw(p, 120, 30)
	p.seq = 1
	p.applyGrep(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: []GrepHit{
		{Rel: "a.go", Line: 8, Col: 6, Text: "func todo() {}"},
		{Rel: "b.go", Line: 2, Col: 1, Text: "todo buffer", Source: GrepBuffers, Index: 1},
	}})
	if !p.previewWanted() {
		t.Fatal("the selected file hit has no preview column")
	}
	cmd := p.previewCmd()
	if cmd == nil {
		t.Fatal("the selected file hit started no preview read")
	}
	msg := cmd()
	pm, ok := msg.(previewMsg)
	if !ok {
		t.Fatalf("preview command returned %T", msg)
	}
	p.applyPreview(pm)
	if pv.reads != 1 {
		t.Fatalf("window reads = %d, want 1", pv.reads)
	}
	lines := shownPreview(t, p)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "8 │ func todo() {}") {
		t.Fatalf("preview does not show the target line number:\n%s", joined)
	}
	if !strings.Contains(joined, "6 │ six") || !strings.Contains(joined, "10 │ ten") {
		t.Fatalf("preview window is not the returned lines:\n%s", joined)
	}
	// Moving to the buffer hit drops the preview column (only file hits preview).
	p.nav.Select(1)
	if p.previewWanted() {
		t.Fatal("a buffer hit reserved the file preview column")
	}
	p.previewCmd()
	if !p.preview.ready || p.preview.rel == "" {
		// reset is allowed; the important part is that the old file window is not shown
		if p.previewWanted() {
			t.Fatal("buffer hit kept a preview")
		}
	}
}

// TestLiveGrepPreviewCancelsWhenSelectionMoves: the old bounded window read's context is
// canceled when the highlight moves to another file hit.
func TestLiveGrepPreviewCancelsWhenSelectionMoves(t *testing.T) {
	t.Parallel()
	pv := &liveWindowPreviewer{data: "a\n", firstLine: 1}
	p := livePreviewPalette(pv)
	p.seq = 1
	p.applyGrep(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: []GrepHit{
		{Rel: "a.go", Line: 1, Col: 1, Text: "todo a"},
		{Rel: "b.go", Line: 1, Col: 1, Text: "todo b"},
	}})
	first := p.previewCmd()
	if first == nil {
		t.Fatal("first hit started no preview")
	}
	first() // the fake returns at once; its context is recorded
	if len(pv.contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(pv.contexts))
	}
	ctx := pv.contexts[0]
	p.nav.Select(1) // move to b.go; the next preview command cancels the old read
	second := p.previewCmd()
	if ctx.Err() == nil {
		t.Fatal("the previous Live Grep preview context was not canceled")
	}
	if second == nil {
		t.Fatal("the moved-to hit started no new preview")
	}
	second()
	if len(pv.contexts) != 2 {
		t.Fatalf("contexts = %d, want 2", len(pv.contexts))
	}
	p.Dismiss()
	if pv.contexts[1].Err() == nil {
		t.Fatal("dismissing the palette did not cancel the live preview context")
	}
}

// TestLiveGrepExportEmitsQuickfixMessage: Alt+Q hands the current hits to the root as a
// GrepExportMsg and closes the palette; an empty result leaves the palette open.
func TestLiveGrepExportEmitsQuickfixMessage(t *testing.T) {
	t.Parallel()
	p := livePreviewPalette(&liveWindowPreviewer{})
	p.seq = 1
	hits := []GrepHit{
		{Rel: "a.go", Line: 2, Col: 3, Text: "todo a"},
		{Rel: "b.go", Line: 1, Col: 1, Text: "todo b", Source: GrepBuffers, Index: 4},
	}
	p.update(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: hits})
	cmd := p.key(altKey('q'))
	if cmd == nil {
		t.Fatal("Alt+Q produced no export")
	}
	msg, ok := cmd().(GrepExportMsg)
	if !ok {
		t.Fatalf("export emitted %T", cmd())
	}
	if msg.Root != "/w" || msg.Query != "todo" || len(msg.Hits) != 2 || msg.Total != 2 {
		t.Fatalf("export message = %+v", msg)
	}
	if p.Active() {
		t.Fatal("the palette stayed open after the export")
	}

	empty := livePreviewPalette(&liveWindowPreviewer{})
	empty.seq = 1
	empty.update(GrepResultMsg{Token: empty.token, Seq: 1, Text: "todo"})
	if cmd := empty.key(altKey('q')); cmd != nil {
		t.Fatal("an empty result produced an export")
	}
	if !empty.Active() {
		t.Fatal("an empty export closed the palette")
	}
}

// TestLiveGrepExportAltMEmitsQuickfixMessage: Alt+M is Fresh's alternate shortcut
// for live_grep_export_quickfix, emitting the same GrepExportMsg as Alt+Q.
func TestLiveGrepExportAltMEmitsQuickfixMessage(t *testing.T) {
	t.Parallel()
	p := livePreviewPalette(&liveWindowPreviewer{})
	p.seq = 1
	p.update(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: []GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}})
	cmd := p.key(altKey('m'))
	if cmd == nil {
		t.Fatal("Alt+M produced no export")
	}
	msg, ok := cmd().(GrepExportMsg)
	if !ok || len(msg.Hits) != 1 {
		t.Fatalf("Alt+M emitted %+v", msg)
	}
	if p.Active() {
		t.Fatal("the palette stayed open after Alt+M export")
	}
}

// TestPreviewCleansC1AndFormatText: file-mode preview text removes C1 controls, bidi
// overrides, zero-width format characters and a BOM before it reaches the grid.
func TestPreviewCleansC1AndFormatText(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.go": {data: "\u202E\u0080package\u200B main\ufeff\n"},
	}}
	p := New(fileOptions("", []string{"a.go"}, pv))
	settle(t, p, p.Init(), 8)
	lines := shownPreview(t, p)
	joined := strings.Join(lines, "\n")
	if strings.ContainsAny(joined, "\u202e\u0080\u200b\ufeff") {
		t.Fatalf("preview kept spoof bytes: %q", joined)
	}
	if !strings.Contains(joined, "package main") {
		t.Fatalf("readable preview text was lost: %q", joined)
	}
}

// TestLiveGrepExportCap: the export hands the list overlay at most liveExportCap rows
// and carries the full hit count so the root can notice the truncation.
func TestLiveGrepExportCap(t *testing.T) {
	t.Parallel()
	hits := make([]GrepHit, liveExportCap+1)
	for i := range hits {
		hits[i] = GrepHit{Rel: "a.go", Line: i + 1, Col: 1, Text: "todo"}
	}
	p := livePreviewPalette(&liveWindowPreviewer{})
	draw(p, 120, 30)
	p.seq = 1
	p.applyGrep(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: hits})
	cmd := p.key(altKey('q'))
	if cmd == nil {
		t.Fatal("capped export produced no message")
	}
	msg := cmd().(GrepExportMsg)
	if len(msg.Hits) != liveExportCap || msg.Total != liveExportCap+1 {
		t.Fatalf("export = %d hits total %d, want %d and %d", len(msg.Hits), msg.Total, liveExportCap, liveExportCap+1)
	}
}

// TestRemoteLiveGrepPreviewUsesTheRemotePath: a remote palette row preview calls the
// window reader with the remote joined path (never a local filepath conversion).
func TestRemoteLiveGrepPreviewUsesTheRemotePath(t *testing.T) {
	t.Parallel()
	pv := &liveWindowPreviewer{data: "one\ntwo\n", firstLine: 1}
	p := New(Options{
		Live:    true,
		Root:    "/remote/w",
		Remote:  true,
		Files:   &fakeFiles{files: []string{"src/a.go"}},
		Grep:    &fakeGrepper{},
		Preview: pv,
	})
	draw(p, 120, 30)
	p.seq = 1
	p.applyGrep(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: []GrepHit{
		{Rel: "src/a.go", Line: 1, Col: 1, Text: "todo remote"},
	}})
	cmd := p.previewCmd()
	if cmd == nil {
		t.Fatal("remote row preview started no read")
	}
	p.applyPreview(cmd().(previewMsg))
	if len(pv.paths) != 1 || pv.paths[0] != "/remote/w/src/a.go" {
		t.Fatalf("preview paths = %v, want the remote path", pv.paths)
	}
}

// TestRemoteLiveGrepExportCarriesTheRemoteFlag: the quickfix message preserves Root/Remote
// so the root's list joins remote paths with the remote separator.
func TestRemoteLiveGrepExportCarriesTheRemoteFlag(t *testing.T) {
	t.Parallel()
	p := New(Options{
		Live:   true,
		Root:   "/remote/w",
		Remote: true,
		Files:  &fakeFiles{files: []string{"src/a.go"}},
		Grep:   &fakeGrepper{},
	})
	draw(p, 120, 30)
	p.seq = 1
	p.applyGrep(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: []GrepHit{
		{Rel: "src/a.go", Line: 2, Col: 1, Text: "todo remote"},
	}, Notice: "ignored scan incomplete", Truncated: true})
	cmd := p.key(altKey('q'))
	if cmd == nil {
		t.Fatal("remote export produced no message")
	}
	msg := cmd().(GrepExportMsg)
	if !msg.Remote || msg.Root != "/remote/w" || msg.Hits[0].Rel != "src/a.go" {
		t.Fatalf("remote export = %+v, want Remote true and the remote root", msg)
	}
	if !msg.Truncated || msg.Notice != "ignored scan incomplete" {
		t.Fatalf("remote export = %+v, want the completeness signal", msg)
	}
}
