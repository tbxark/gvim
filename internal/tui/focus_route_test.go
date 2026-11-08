package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/browser"
)

// TestTabStripFocusTypesNoText checks that the tab strip takes the keys that would
// type text, shifted letters included, so no text reaches the editor while the
// strip has focus.
func TestTabStripFocusTypesNoText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "shifted letter", key: tea.KeyPressMsg{Code: 'a', Mod: tea.ModShift, Text: "A"}},
		{name: "plain letter", key: typed("a")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := menuModel(t, true)
			press(m, special(tea.KeyF6))
			if m.stage() != stageTabs {
				t.Fatalf("setup: F6 reached stage %v, want the tab strip", m.stage())
			}
			ed := m.ed.(*fakeEditor)
			press(m, tt.key)
			if slices.Contains(ed.actions, editor.Action("insert_char")) {
				t.Errorf("tab strip focus ran insert_char for %q: actions %v", tt.key.Text, ed.actions)
			}
		})
	}
}

// TestExplorerFocusSavesWithCtrlS checks that a key the explorer keymap does not
// bind still runs the application-wide action the normal keymap binds to it.
func TestExplorerFocusSavesWithCtrlS(t *testing.T) {
	t.Parallel()
	m, ed, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	if !m.explorerFocus {
		t.Fatal("setup: explorer not focused")
	}
	press(m, ctrl('s'))
	if !slices.Contains(ed.actions, editor.Action("save")) {
		t.Errorf("Ctrl+S with the explorer focused ran %v, want save", ed.actions)
	}
}

// TestExplorerFocusDropsNormalKeysOutsideFreshSet checks the other side of the
// fall-through: a normal binding that is not application-wide, such as New, is
// still dropped while the explorer has focus.
func TestExplorerFocusDropsNormalKeysOutsideFreshSet(t *testing.T) {
	t.Parallel()
	m, ed, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	press(m, ctrl('n'))
	if len(ed.actions) != 0 {
		t.Errorf("Ctrl+N with the explorer focused ran %v, want nothing", ed.actions)
	}
	if !m.explorerFocus {
		t.Error("Ctrl+N dropped by the explorer moved the focus")
	}
}

// TestExplorerFocusOpensThePaletteWithCtrlP checks a UI key that Fresh lets through
// from the explorer: the palette opens over the frame.
func TestExplorerFocusOpensThePaletteWithCtrlP(t *testing.T) {
	t.Parallel()
	m, _, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	press(m, ctrl('p'))
	if len(m.overlays) != 1 {
		t.Errorf("Ctrl+P with the explorer focused opened %d overlays, want the palette", len(m.overlays))
	}
}

// TestBrowserFileChoiceFocusesTheEditor checks that a file picked in the file
// browser gives the keyboard to the editor, as a palette pick does.
func TestBrowserFileChoiceFocusesTheEditor(t *testing.T) {
	t.Parallel()
	m, _, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	if m.stage() != stageExplorer {
		t.Fatalf("setup: stage %v, want the explorer", m.stage())
	}
	m.Update(browser.Open{Path: "/ws/alpha.txt"})
	if m.stage() != stageEditor {
		t.Errorf("stage %v after a browser file choice, want the editor", m.stage())
	}
}

// TestNewTabFocusesTheEditor checks that creating a tab leaves neither the
// explorer nor the tab strip with the keyboard, for the "+" button and for the
// New command from the tab strip.
func TestNewTabFocusesTheEditor(t *testing.T) {
	t.Parallel()
	t.Run("plus button from the explorer", func(t *testing.T) {
		t.Parallel()
		m, ed, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
		m.Update(click(plusColumn(m), m.layout().StripY, tea.MouseLeft))
		chooseRow(t, m, "New File")
		if !slices.Contains(ed.actions, editor.Action("new")) {
			t.Fatalf("the + button ran %v, want new", ed.actions)
		}
		if m.stage() != stageEditor {
			t.Errorf("stage %v after the + button, want the editor", m.stage())
		}
	})
	t.Run("New from the tab strip", func(t *testing.T) {
		t.Parallel()
		m := menuModel(t, true)
		press(m, special(tea.KeyF6))
		if m.stage() != stageTabs {
			t.Fatalf("setup: stage %v, want the tab strip", m.stage())
		}
		ed := m.ed.(*fakeEditor)
		press(m, ctrl('n'))
		if !slices.Contains(ed.actions, editor.Action("new")) {
			t.Fatalf("Ctrl+N ran %v, want new", ed.actions)
		}
		if m.stage() != stageEditor {
			t.Errorf("stage %v after New, want the editor", m.stage())
		}
	})
}
