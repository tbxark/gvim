package palette

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// blockingContextGrepper blocks until the palette cancels its context, so a test can prove
// the remote search is actually stopped (not merely its answer dropped).
type blockingContextGrepper struct {
	started chan context.Context
}

func (b *blockingContextGrepper) Search(GrepRequest) ([]GrepHit, error) {
	return nil, nil
}

func (b *blockingContextGrepper) SearchContext(ctx context.Context, _ GrepRequest) (GrepResult, error) {
	b.started <- ctx
	<-ctx.Done()
	return GrepResult{}, ctx.Err()
}

func newBlockingGrepPalette(t *testing.T) (*Palette, *blockingContextGrepper) {
	t.Helper()
	cg := &blockingContextGrepper{started: make(chan context.Context, 2)}
	p := New(Options{
		Live:  true,
		Root:  "/w",
		Files: &fakeFiles{files: []string{"a.go"}},
		Grep:  cg,
	})
	draw(p, 120, 30)
	return p, cg
}

// TestLiveGrepCancelsContextOnQueryChange: a newer query cancels the previous remote search
// context before the next command starts.
func TestLiveGrepCancelsContextOnQueryChange(t *testing.T) {
	t.Parallel()
	p, cg := newBlockingGrepPalette(t)
	cmd := p.update(typed("a"))
	if cmd == nil {
		t.Fatal("typing started no search command")
	}
	go func() { _ = cmd() }()
	first := <-cg.started
	if first.Err() != nil {
		t.Fatal("the first search context was already canceled")
	}
	// The next keystroke supersedes the first search; the old context must be canceled.
	p.update(typed("b"))
	select {
	case <-first.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a query change did not cancel the previous search")
	}
}

// TestLiveGrepCancelsContextOnDismiss: closing the palette cancels the in-flight search.
func TestLiveGrepCancelsContextOnDismiss(t *testing.T) {
	t.Parallel()
	p, cg := newBlockingGrepPalette(t)
	cmd := p.update(typed("a"))
	if cmd == nil {
		t.Fatal("typing started no search command")
	}
	go func() { _ = cmd() }()
	ctx := <-cg.started
	p.Dismiss()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("dismissing the palette did not cancel the Live Grep search")
	}
}

// TestLiveGrepCancelsContextOnRowOpen: choosing a row closes the palette and cancels any
// still-running search it no longer needs.
func TestLiveGrepCancelsContextOnRowOpen(t *testing.T) {
	t.Parallel()
	p, cg := newBlockingGrepPalette(t)
	cmd := p.update(typed("a"))
	if cmd == nil {
		t.Fatal("typing started no search command")
	}
	go func() { _ = cmd() }()
	ctx := <-cg.started
	// Add a row directly so Enter has something to open; the search itself stays blocked.
	p.rows = []row{{kind: rowLine, line: 1, col: 1}}
	p.nav.SetSize(len(p.rows), 10)
	p.nav.Select(0)
	p.update(key(tea.KeyEnter))
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("opening a row did not cancel the Live Grep search")
	}
}
