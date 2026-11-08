package uiadapter

import (
	"github.com/tbxark/gvim/internal/highlight"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
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
