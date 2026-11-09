package searchpanel

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// blockingContextPreviewer records the context and blocks until the panel cancels it, so
// a test can prove the in-flight read is actually stopped (not merely its answer dropped).
type blockingContextPreviewer struct {
	contexts chan context.Context
}

func (b *blockingContextPreviewer) Preview(PreviewRequest) (PreviewResult, error) {
	return PreviewResult{}, nil
}

func (b *blockingContextPreviewer) PreviewContext(ctx context.Context, _ PreviewRequest) (PreviewResult, error) {
	b.contexts <- ctx
	<-ctx.Done()
	return PreviewResult{}, ctx.Err()
}

func newCancelPreviewPanel(t *testing.T) (*Panel, *blockingContextPreviewer) {
	t.Helper()
	cp := &blockingContextPreviewer{contexts: make(chan context.Context, 4)}
	s := &fakeSearcher{result: sampleResult()}
	o := searchOptions(s)
	o.Previewer = cp
	p := New(o)
	search(t, p, "foo")
	return p, cp
}

// TestPreviewCancelsOnSelectionChange proves a new selected row cancels the previous
// authority/file read instead of leaving it running while only dropping its answer.
func TestPreviewCancelsOnSelectionChange(t *testing.T) {
	t.Parallel()
	p, cp := newCancelPreviewPanel(t)
	var cmd tea.Cmd
	for i := 0; i < 3; i++ {
		_, cmd = p.Update(specialKey(tea.KeyTab))
	}
	if cmd == nil {
		t.Fatal("entering the results started no preview")
	}
	done := make(chan struct{})
	go func() {
		_ = cmd()
		close(done)
	}()
	ctx := <-cp.contexts
	if ctx.Err() != nil {
		t.Fatal("the preview context was already cancelled")
	}
	// Moving to the next match supersedes the preview request; the old read is cancelled
	// before the new one starts. The first Down may land on the same match (no new
	// request), so step until a new preview command appears.
	var next tea.Cmd
	for i := 0; i < 3 && next == nil; i++ {
		_, next = p.Update(specialKey(tea.KeyDown))
	}
	if next == nil {
		t.Fatal("the new selection started no preview")
	}
	<-ctx.Done()
	<-done
}

// TestPreviewCancelsOnDismiss proves closing the panel stops an in-flight preview read.
func TestPreviewCancelsOnDismiss(t *testing.T) {
	t.Parallel()
	p, cp := newCancelPreviewPanel(t)
	var cmd tea.Cmd
	for i := 0; i < 3; i++ {
		_, cmd = p.Update(specialKey(tea.KeyTab))
	}
	if cmd == nil {
		t.Fatal("entering the results started no preview")
	}
	done := make(chan struct{})
	go func() {
		_ = cmd()
		close(done)
	}()
	ctx := <-cp.contexts
	p.Dismiss()
	<-ctx.Done()
	<-done
}

// TestExportCancelsPreviewRead: Alt+Q closes the result view into the quickfix list, so
// exportCmd cancels an in-flight preview read before it returns.
func TestExportCancelsPreviewRead(t *testing.T) {
	t.Parallel()
	p, cp := newCancelPreviewPanel(t)
	var cmd tea.Cmd
	for i := 0; i < 3; i++ {
		_, cmd = p.Update(specialKey(tea.KeyTab))
	}
	if cmd == nil {
		t.Fatal("entering the results started no preview")
	}
	done := make(chan struct{})
	go func() {
		_ = cmd()
		close(done)
	}()
	ctx := <-cp.contexts
	if p.exportCmd() == nil {
		t.Fatal("the export returned no message with matches")
	}
	<-ctx.Done()
	<-done
}
