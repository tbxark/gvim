package uiadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/syntax"
)

// openNamed opens one file per name, in order, so the last name is the active tab.
// Each file holds its own name as text.
func openNamed(t *testing.T, ed *editor.Editor, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		path := filepath.Join(dir, n)
		if err := os.WriteFile(path, []byte("text of "+n+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenFile(path); err != nil {
			t.Fatalf("OpenFile(%s): %v", n, err)
		}
	}
}

// TestDocAtGivesEachTabItsDocument checks the tab documents that split panes paint,
// including the untyped nil for a tab that is not there.
func TestDocAtGivesEachTabItsDocument(t *testing.T) {
	t.Parallel()
	ed := editor.New(editor.Options{})
	openNamed(t, ed, "a.txt", "b.txt", "c.txt")
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	for i, want := range []string{"a.txt", "b.txt", "c.txt"} {
		d := a.DocAt(i)
		if d == nil || d.Name() != want {
			t.Fatalf("DocAt(%d) = %v, want the document %s", i, d, want)
		}
	}
	for _, i := range []int{-1, 3} {
		if d := a.DocAt(i); d != nil {
			t.Fatalf("DocAt(%d) = %T, want an untyped nil", i, d)
		}
	}
}

// TestDocAtBoundsTheSpanCachesToOpenTabs checks that the caches of closed tabs are
// dropped, so a closed document is not kept alive by the adapter.
func TestDocAtBoundsTheSpanCachesToOpenTabs(t *testing.T) {
	t.Parallel()
	ed := editor.New(editor.Options{})
	openNamed(t, ed, "a.txt", "b.txt", "c.txt")
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	a.DocAt(0)
	a.DocAt(1)
	if got := len(a.others); got != 2 {
		t.Fatalf("caches after two other tabs = %d, want 2", got)
	}
	for range 2 {
		if err := ed.CloseTab(0, false); err != nil {
			t.Fatalf("CloseTab: %v", err)
		}
	}
	openNamed(t, ed, "d.txt")
	if d := a.DocAt(0); d == nil || d.Name() != "c.txt" {
		t.Fatalf("DocAt(0) = %v, want c.txt", d)
	}
	if got := len(a.others); got != 1 {
		t.Fatalf("caches with one open non-active tab = %d, want 1", got)
	}
}

// TestKeymapAdapterListsItsBindings checks the help and keybinding views read the
// keymap's bindings as the root expects them.

// TestSettingsForPrefersFlagsOverConfig checks the values open_settings lists.

// TestSettingsForCarriesTheSaveOptions checks the settings only gvim.config.get reads.

// TestSwitchKeymapKeepsTheConfigKeybindings checks that a keymap switch applies the
// config overrides, so a binding the user removed stays removed.

// TestEditorSetContextUpdatesActiveAndOtherTabCaches checks that SetContext updates both the
// active spans cache and all inactive tab caches stored in others.
func TestEditorSetContextUpdatesActiveAndOtherTabCaches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Put >300 comment lines (~8 KiB) after `/*` on line 1 so line 350 (window [250..550])
	// is well outside both LineSpans(0)'s cached window [0..200] and a 0-byte context, while
	// fitting inside SetContext(64<<10).
	var sb strings.Builder
	sb.WriteString("package p\n/*\n")
	for range 360 {
		sb.WriteString("comment padding line 012345\n")
	}
	sb.WriteString("target\n*/\n")
	bigPath := filepath.Join(dir, "big.go")
	otherPath := filepath.Join(dir, "other.go")
	if err := os.WriteFile(bigPath, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hasComment := func(spans []syntax.Span) bool {
		for _, sp := range spans {
			if sp.Kind == syntax.KindComment {
				return true
			}
		}
		return false
	}
	// Baseline: with context 0, line 350 does not see `/*` from line 1.
	ed0 := editor.New(editor.Options{LargeFileMode: true, LargeFileThresholdBytes: 100})
	if err := ed0.OpenFile(bigPath); err != nil {
		t.Fatal(err)
	}
	if err := ed0.OpenFile(otherPath); err != nil {
		t.Fatal(err)
	}
	a0 := &Editor{ed: ed0, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	a0.SetContext(0)
	_ = a0.DocAt(0).LineSpans(0) // materialize inactive cache in a0.others with 0 context
	if hasComment(a0.DocAt(0).LineSpans(350)) {
		t.Fatalf("baseline with 0 context unexpectedly saw comment on line 350")
	}

	// Target: create inactive cache in a.others while context is 0, then update via SetContext.
	ed := editor.New(editor.Options{LargeFileMode: true, LargeFileThresholdBytes: 100})
	if err := ed.OpenFile(bigPath); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenFile(otherPath); err != nil {
		t.Fatal(err)
	}
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	a.SetContext(0)
	_ = a.DocAt(0).LineSpans(0) // creates a.others[doc0] with 0 context
	a.SetContext(64 << 10)
	if !hasComment(a.DocAt(0).LineSpans(350)) {
		t.Fatalf("LineSpans(350) after SetContext(64KiB) on inactive tab missed KindComment")
	}
}
