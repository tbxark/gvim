package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// TestCrossWorkspaceStaleGrepResultIsDropped: a palette result from an older workspace
// model generation must not replace the active model's rows even when its seq/text
// happen to match; without the owner guard the palette would accept it.
func TestCrossWorkspaceStaleGrepResultIsDropped(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "fresh.go", Line: 1, Col: 1, Text: "todo fresh"}}}
	m, _ := newLiveModel(t, g)
	m.SetWorkspaceOwner("ws-b", 2)
	must(t, m.Run("open_live_grep"))
	var cmd tea.Cmd
	for _, c := range "todo" {
		_, cmd = m.Update(typed(string(c)))
	}
	if cmd == nil {
		t.Fatal("typing started no search")
	}
	fresh, ok := cmd().(palette.GrepResultMsg)
	if !ok {
		t.Fatalf("search returned %T, want palette.GrepResultMsg", cmd())
	}
	if fresh.Owner != m.WorkspaceOwner() {
		t.Fatalf("fresh owner = %+v, want this model %+v", fresh.Owner, m.WorkspaceOwner())
	}
	stale := fresh
	stale.Owner = palette.Owner{Workspace: "ws-a", Generation: 1}
	stale.Hits = []palette.GrepHit{{Rel: "stale.go", Line: 9, Col: 1, Text: "todo stale"}}
	m.Update(stale)
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "stale.go") {
		t.Fatalf("stale cross-workspace result was painted:\n%s", view)
	}
	m.Update(fresh)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "fresh.go") {
		t.Fatalf("fresh result was not painted after the stale one was dropped:\n%s", view)
	}
}

// TestCrossWorkspaceStaleOpenTerminalIsDropped: OpenTerminalMsg's ID is model-local, so a
// message from another model generation must be dropped before it can focus a colliding
// tab in this model. A same-owner message is still handled normally.
func TestCrossWorkspaceStaleOpenTerminalIsDropped(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(1), 80, 24)
	m.SetWorkspaceOwner("ws-b", 2)
	m.Update(palette.OpenTerminalMsg{Owner: palette.Owner{Workspace: "ws-a", Generation: 1}, ID: 7})
	if m.notice != "" {
		t.Fatalf("stale open-terminal message was applied: notice %q", m.notice)
	}
	m.Update(palette.OpenTerminalMsg{Owner: m.WorkspaceOwner(), ID: 7})
	if m.notice == "" {
		t.Fatal("a same-owner open-terminal message was not handled")
	}
}

// blockingGrep is a cancellable Live Grep port that stays blocked until its context is
// canceled, so a test can prove CancelTransientWork reaches the in-flight search.
type blockingGrep struct {
	started  chan struct{}
	canceled chan struct{}
}

func (b *blockingGrep) Search(palette.GrepRequest) ([]palette.GrepHit, error) {
	return nil, nil
}

func (b *blockingGrep) SearchContext(ctx context.Context, _ palette.GrepRequest) (palette.GrepResult, error) {
	close(b.started)
	<-ctx.Done()
	close(b.canceled)
	return palette.GrepResult{}, ctx.Err()
}

// TestCancelTransientWorkCancelsLiveGrep: the workspace manager's switch/evict/close
// hook reaches the palette's in-flight Live Grep command, so it does not keep running
// after its model is hidden.
func TestCancelTransientWorkCancelsLiveGrep(t *testing.T) {
	t.Parallel()
	g := &blockingGrep{started: make(chan struct{}), canceled: make(chan struct{})}
	m, _ := newLiveModel(t, g)
	m.SetWorkspaceOwner("ws-b", 2)
	must(t, m.Run("open_live_grep"))
	_, cmd := m.Update(typed("t"))
	if cmd == nil {
		t.Fatal("typing started no search")
	}
	m.CancelTransientWork()
	msg := cmd()
	select {
	case <-g.canceled:
	default:
		t.Fatal("CancelTransientWork did not cancel the in-flight Live Grep context")
	}
	if res, ok := msg.(palette.GrepResultMsg); !ok || res.Err != context.Canceled {
		t.Fatalf("canceled search returned %#v, want a context.Canceled GrepResultMsg", msg)
	}
}

// TestCancelTransientWorkCancelsProjectReplacement: the project-wide replacement runs
// outside the overlay stack, so the workspace switch/evict/close hook must cancel it
// explicitly too.
func TestCancelTransientWorkCancelsProjectReplacement(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(1), 80, 24)
	canceled := false
	m.proj.replaceCancel = func() { canceled = true }
	m.CancelTransientWork()
	if !canceled || m.proj.replaceCancel != nil {
		t.Fatal("CancelTransientWork did not cancel the project replacement")
	}
}
