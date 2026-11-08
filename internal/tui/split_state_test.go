package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/theme"
)

func TestSplitTabMappingResolvesStableIdentity(t *testing.T) {
	t.Parallel()
	files := []session.FileState{{Path: "/w/a.txt"}, {Path: "/w/b.txt"}, {Path: "/w/a.txt"}}
	tabs := []frame.TabInfo{{Path: "/w/a.txt"}, {Path: "/w/b.txt"}, {Path: "/w/a.txt"}}
	if got := splitTabMapping(files, tabs); len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("mapping = %v", got)
	}
	// A missing file maps to -1 rather than to another tab; duplicate occurrences
	// keep their own ranks (a.txt rank 0 -> first a tab, rank 1 -> second).
	missing := []frame.TabInfo{{Path: "/w/a.txt"}, {Path: "/w/a.txt"}}
	if got := splitTabMapping(files, missing); got[0] != 0 || got[1] != -1 || got[2] != 1 {
		t.Fatalf("mapping with a missing file = %v", got)
	}
	// Restored order is not assumed: the path is the identity, not the index.
	reordered := []frame.TabInfo{{Path: "/w/b.txt"}, {Path: "/w/a.txt"}, {Path: "/w/a.txt"}}
	if got := splitTabMapping(files, reordered); got[0] != 1 || got[1] != 0 || got[2] != 2 {
		t.Fatalf("reordered mapping = %v", got)
	}
	// An empty path (unnamed buffer) has no identity.
	if got := splitTabMapping([]session.FileState{{Path: ""}}, []frame.TabInfo{{Path: ""}}); got[0] != -1 {
		t.Fatalf("unnamed mapping = %v", got)
	}
}

func TestSplitStateSinglePaneAndInvalidRestore(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("hello"), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := m.SplitState(); got != nil {
		t.Fatalf("a single-pane model captured a layout: %+v", got)
	}
	// A nil or invalid layout is ignored, and the model keeps its one pane.
	if m.RestoreSplits(nil, nil) {
		t.Fatal("a nil layout was applied")
	}
	bad := &session.SplitLayout{Root: session.SplitNode{Dir: "diagonal", A: &session.SplitNode{File: 0, Path: "/x"}, B: &session.SplitNode{File: 1, Path: "/y"}}}
	if m.RestoreSplits(bad, []session.FileState{{Path: "/x"}, {Path: "/y"}}) {
		t.Fatal("an invalid layout was applied")
	}
	if !m.splits.Root.Leaf() {
		t.Fatal("a rejected layout changed the split tree")
	}
}
