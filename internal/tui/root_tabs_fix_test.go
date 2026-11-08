package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
)

// tabsEditor is a fake editor with tabs that can be activated. It records each
// ActivateTab. With no tabs it has no active document.
type tabsEditor struct {
	*fakeEditor
	activated []int
}

func (e *tabsEditor) Active() Document {
	if len(e.tabs) == 0 {
		return nil
	}
	return e.fakeEditor.Active()
}

func (e *tabsEditor) ActivateTab(i int) {
	e.activated = append(e.activated, i)
	for j := range e.tabs {
		e.tabs[j].Active = j == i
	}
}

func (e *tabsEditor) CloseTab(int, bool) error            { return nil }
func (e *tabsEditor) CloseNeedsConfirm(int) (bool, error) { return false, nil }
func (e *tabsEditor) Quit(bool) []editor.Dirty            { return nil }

// newTabsModel returns a model over three tabs: a.txt, the active one, b.txt, and a
// preview tab named c.txt (preview).
func newTabsModel(t *testing.T) (*Model, *tabsEditor) {
	t.Helper()
	ed := &tabsEditor{fakeEditor: newFakeEditor("x")}
	ed.tabs = []TabInfo{
		{Name: "a.txt", Active: true},
		{Name: "b.txt"},
		{Name: "c.txt (preview)", Preview: true},
	}
	return newRevertModel(t, ed), ed
}

// TestSwitchToTabByNameListsTheTabs checks that the palette command opens a list of the
// tab names, in tab order, instead of failing for want of a name.
func TestSwitchToTabByNameListsTheTabs(t *testing.T) {
	t.Parallel()
	m, _ := newTabsModel(t)
	feedMessages(m, rootPaletteRun(m, "switch_to_tab_by_name"))
	rows, ok := overlayRows(m)
	if !ok {
		t.Fatal("Switch to Tab by Name did not open a list overlay")
	}
	sameLines(t, "tab list", rows, []string{"a.txt=", "b.txt=", "c.txt (preview)="})
}

// TestSwitchToTabByNameChoosingATabActivatesIt checks that Enter on a row switches to
// that tab, and closes the list.
func TestSwitchToTabByNameChoosingATabActivatesIt(t *testing.T) {
	t.Parallel()
	m, ed := newTabsModel(t)
	feedMessages(m, rootPaletteRun(m, "switch_to_tab_by_name"))
	press(m, special(tea.KeyDown))
	feedMessages(m, press(m, special(tea.KeyEnter)))
	if len(ed.activated) != 1 || ed.activated[0] != 1 {
		t.Fatalf("ActivateTab calls = %v, want [1]", ed.activated)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("the list is still open after a choice: %d overlays", len(m.overlays))
	}
}

// TestSwitchToTabByNameKeepsTheNameArgument checks that a command that names the tab
// still goes to the editor, as scripts and key bindings with a name argument expect.
func TestSwitchToTabByNameKeepsTheNameArgument(t *testing.T) {
	t.Parallel()
	m, ed := newTabsModel(t)
	m.dispatch(Command{Action: "switch_to_tab_by_name", Args: map[string]any{"name": "b.txt"}})
	if len(ed.actions) != 1 || ed.actions[0] != "switch_to_tab_by_name" || ed.args[0]["name"] != "b.txt" {
		t.Fatalf("editor calls = %v with %v, want switch_to_tab_by_name with name b.txt", ed.actions, ed.args)
	}
	if len(m.overlays) != 0 {
		t.Fatal("a command with a name opened the list")
	}
}

// TestSwitchToTabByNameWithNoTabsSaysSo checks that the list is not opened over nothing.
func TestSwitchToTabByNameWithNoTabsSaysSo(t *testing.T) {
	t.Parallel()
	ed := &tabsEditor{fakeEditor: newFakeEditor("x")}
	ed.tabs = nil
	m := newRevertModel(t, ed)
	feedMessages(m, rootPaletteRun(m, "switch_to_tab_by_name"))
	if len(m.overlays) != 0 {
		t.Fatal("the tab list opened with no tabs")
	}
	if m.notice != "No open tabs to switch to" {
		t.Fatalf("notice = %q, want %q", m.notice, "No open tabs to switch to")
	}
}
