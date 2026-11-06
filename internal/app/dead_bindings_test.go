package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
	"github.com/tbxark/gvim/internal/ui/menubar"
)

// unhandledActions lists the actions that the embedded keymaps bind but the port
// does not run yet. Pressing their keys shows "Not supported in this port". The
// list is the gap list from migration/BLUEPRINT.md §1.3. When a handler lands,
// TestEveryBoundActionRunsOrIsListed fails until its entry is removed here.
var unhandledActions = map[string]bool{
	"file_explorer_search_backspace": true,
}

// presetNames are the embedded keymaps. keymap does not export the list.
var presetNames = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// probedContexts are the when-clauses a model with no overlay open can reach.
// Bindings in other contexts (prompt, popup, menu, settings, terminal, and so
// on) belong to an overlay, so running them on a bare model proves nothing.
var probedContexts = []keymap.Context{
	keymap.ContextGlobal, keymap.ContextNormal, keymap.ContextFileExplorer,
}

// boundActions returns every action named by a probed binding in any embedded
// keymap, sorted and without repeats.
func boundActions(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, name := range presetNames {
		km, err := keymap.ByName(name)
		if err != nil {
			t.Fatalf("ByName(%q): %v", name, err)
		}
		for _, b := range km.Bindings() {
			if slices.Contains(probedContexts, b.When) {
				seen[b.Action] = true
			}
		}
	}
	actions := make([]string, 0, len(seen))
	for a := range seen {
		actions = append(actions, a)
	}
	slices.Sort(actions)
	return actions
}

// newProbeModel returns a model with a document and the explorer open on dir.
// Each probe gets its own model, so an action's side effects do not reach the next.
func newProbeModel(t *testing.T, dir string) *ui.Model {
	t.Helper()
	path := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	tree, err := explorer.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	deps := newUIDeps(ed, keymap.Default(), theme.Theme{}, tree, nil)
	deps.ShowExplorer = true
	return ui.New(deps)
}

func TestEveryBoundActionRunsOrIsListed(t *testing.T) {
	dir := t.TempDir()
	var unhandled []string
	for _, action := range boundActions(t) {
		err := newProbeModel(t, dir).Run(action)
		isUnhandled := errors.Is(err, menubar.ErrUnsupported)
		if isUnhandled {
			unhandled = append(unhandled, action)
		}
		switch {
		case isUnhandled && !unhandledActions[action]:
			t.Errorf("%s is bound but has no handler; implement it or add it to unhandledActions", action)
		case !isUnhandled && unhandledActions[action]:
			t.Errorf("%s now has a handler; remove it from unhandledActions", action)
		}
	}
	t.Logf("unhandled bound actions: %d: %v", len(unhandled), unhandled)
}

// unhandledPaletteActions lists the actions of the builtin palette commands that the port
// does not run yet. Running one shows "Not supported in this port". This list must shrink:
// TestEveryPaletteActionRunsOrIsListed fails when an entry gains a handler until its entry
// is removed here, and it fails on a new command with no handler until it is listed. An
// action that answers with a notice (a command the port does not apply, with the reason
// in the notice) is handled, so it does not belong here.
var unhandledPaletteActions = map[string]bool{}

// TestEveryPaletteActionRunsOrIsListed runs the action of every builtin palette command on
// the default keymap's probe model, and checks it against unhandledPaletteActions.
func TestEveryPaletteActionRunsOrIsListed(t *testing.T) {
	dir := t.TempDir()
	seen := map[string]bool{}
	var unhandled []string
	for _, e := range palette.Commands(keymap.Default()) {
		if e.Action == "" || seen[e.Action] {
			continue
		}
		seen[e.Action] = true
		isUnhandled := errors.Is(newProbeModel(t, dir).Run(e.Action), menubar.ErrUnsupported)
		if isUnhandled {
			unhandled = append(unhandled, e.Action)
		}
		switch {
		case isUnhandled && !unhandledPaletteActions[e.Action]:
			t.Errorf("palette command %q runs %s, which has no handler; implement it or add it to unhandledPaletteActions", e.Name, e.Action)
		case !isUnhandled && unhandledPaletteActions[e.Action]:
			t.Errorf("%s now has a handler; remove it from unhandledPaletteActions", e.Action)
		}
	}
	slices.Sort(unhandled)
	t.Logf("unhandled palette actions: %d: %v", len(unhandled), unhandled)
}
