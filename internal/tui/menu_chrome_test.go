package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/theme"
)

// menuKeys is testKeys with F10 and the Alt+letter chords of the default keymap,
// which open the dropdowns through menu_open with a name argument.
func menuKeys() fakeKeymap {
	k := testKeys()
	k["normal|F10|"] = Command{Action: "menu_activate"}
	for letter, name := range map[string]string{
		"f": "File", "e": "Edit", "v": "View", "s": "Selection", "g": "Go", "h": "Help", "x": "Explorer",
	} {
		args := map[string]any{"name": name}
		k["normal|"+letter+"|alt"] = Command{Action: "menu_open", Args: args}
		k["file_explorer|"+letter+"|alt"] = Command{Action: "menu_open", Args: args}
	}
	return k
}

// altKey is Alt with a letter.
func altKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt}
}

// menuModel is a model over one document in an 80 by 24 frame, with the menu bar
// shown or hidden.
func menuModel(t *testing.T, hideMenuBar bool) *Model {
	t.Helper()
	ed := newFakeEditor("hello")
	ed.quitAction = "quit"
	m := New(Deps{
		Editor: ed, Explorer: newFakeTree(3), Keys: menuKeys(), HideMenuBar: hideMenuBar,
		Theme: theme.Theme{}, Clock: testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func TestAltLetterOpensItsTopLevelDropdown(t *testing.T) {
	tests := []struct {
		letter rune
		title  string
	}{
		{'f', "File"}, {'e', "Edit"}, {'v', "View"}, {'s', "Selection"}, {'g', "Go"}, {'h', "Help"},
	}
	for _, tt := range tests {
		m := menuModel(t, false)
		press(m, altKey(tt.letter))
		if m.menu.Title() != tt.title {
			t.Errorf("Alt+%c opened %q, want %q", tt.letter, m.menu.Title(), tt.title)
		}
	}
}

func TestAltXOpensTheExplorerDropdownOnlyWhileTheExplorerHasFocus(t *testing.T) {
	m := menuModel(t, false)
	press(m, altKey('x'))
	if m.menu.IsOpen() {
		t.Fatalf("Alt+X opened %q with the explorer unfocused", m.menu.Title())
	}
	press(m, ctrl('e'))
	press(m, altKey('x'))
	if m.menu.Title() != "Explorer" {
		t.Fatalf("Alt+X with the explorer focused opened %q, want Explorer", m.menu.Title())
	}
}

func TestMenuOpenMatchesTheLabelWithoutCaseAndIgnoresAnUnknownName(t *testing.T) {
	m := menuModel(t, false)
	m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": "view"}})
	if m.menu.Title() != "View" {
		t.Fatalf("menu_open view opened %q, want View", m.menu.Title())
	}
	m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": "Nope"}})
	if m.menu.Title() != "View" {
		t.Fatalf("menu_open with an unknown name changed the dropdown to %q", m.menu.Title())
	}
}

// unlistedEditor reports every action as unknown, the way the app adapter does for
// an action that no layer runs.
type unlistedEditor struct{ *fakeEditor }

func (unlistedEditor) Do(editor.Action, map[string]any) editor.Result {
	return editor.Result{Err: editor.ErrUnknownAction}
}

func TestRunMenuOpenWithoutANameOpensNothing(t *testing.T) {
	m := New(Deps{Editor: unlistedEditor{newFakeEditor("hello")}, Keys: menuKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if err := m.Run("menu_open"); err != nil {
		t.Fatalf("Run(menu_open) = %v, want nil", err)
	}
	if m.menu.IsOpen() {
		t.Fatalf("Run(menu_open) opened %q", m.menu.Title())
	}
}

// TestAnOpenDropdownHangsBelowTheLabelRow keeps the dropdown off the menu bar: the
// box's top border is on the band's padding row, so the label row keeps every label.
func TestAnOpenDropdownHangsBelowTheLabelRow(t *testing.T) {
	m := menuModel(t, false)
	runAction(t, m, "menu_activate")
	l := m.layout()
	rows := m.paint().Plain()
	for _, label := range menubar.Labels(false) {
		if !strings.Contains(rows[l.MenuLabelY], label) {
			t.Errorf("the open dropdown covers the label %q: row %d = %q", label, l.MenuLabelY, rows[l.MenuLabelY])
		}
	}
	top := l.MenuLabelY + 1
	if !strings.Contains(rows[top], "╭") {
		t.Errorf("the dropdown top border is not on the padding row %d: %q", top, rows[top])
	}
	if strings.Contains(rows[l.MenuLabelY], "╭") {
		t.Errorf("the dropdown top border is on the label row: %q", rows[l.MenuLabelY])
	}
}

func TestHiddenMenuBarIsNotDrawnAndTheBodyStartsAtRowOne(t *testing.T) {
	m := menuModel(t, true)
	if l := m.layout(); l.BodyTop != 1 {
		t.Fatalf("body top %d with the menu bar hidden, want 1", l.BodyTop)
	}
	rows := m.paint().Plain()
	if strings.Contains(rows[0], "File") || strings.Contains(rows[0], "Help") {
		t.Fatalf("row 0 %q still shows menu labels", rows[0])
	}
	if !strings.Contains(rows[0], " a.txt ") {
		t.Fatalf("row 0 %q has no tab strip", rows[0])
	}
	if !strings.Contains(rows[1], "hello") {
		t.Fatalf("row 1 %q has no body text", rows[1])
	}
}

func TestHiddenMenuBarOpensNoDropdown(t *testing.T) {
	m := menuModel(t, true)
	press(m, special(tea.KeyF10))
	if m.menu.IsOpen() {
		t.Fatalf("F10 opened %q with the menu bar hidden", m.menu.Title())
	}
	press(m, altKey('f'))
	if m.menu.IsOpen() {
		t.Fatalf("Alt+F opened %q with the menu bar hidden", m.menu.Title())
	}
	m.Update(click(1, 0, tea.MouseLeft))
	if m.menu.IsOpen() {
		t.Fatalf("a press on row 0 opened %q with the menu bar hidden", m.menu.Title())
	}
	if err := m.Run("menu_activate"); err != nil || m.menu.IsOpen() {
		t.Fatalf("Run(menu_activate) = %v with the dropdown open %v, want nil and closed", err, m.menu.IsOpen())
	}
}

func TestHiddenMenuBarIsSkippedByF6(t *testing.T) {
	m := menuModel(t, true)
	press(m, special(tea.KeyF6))
	if m.stage() != stageTabs {
		t.Fatalf("F6 from the editor reached stage %v, want the tab strip", m.stage())
	}
	press(m, special(tea.KeyF6))
	if m.stage() != stageEditor {
		t.Fatalf("F6 from the tab strip reached stage %v, want the editor", m.stage())
	}
}

// The Open Menu Bar command says why nothing opened while the bar is hidden.
func TestOpenMenuBarCommandExplainsAHiddenBar(t *testing.T) {
	for _, action := range []string{"menu_activate", "menu_open"} {
		m := menuModel(t, true)
		m.dispatch(Command{Action: action, Args: map[string]any{"name": "File"}})
		if m.menu.IsOpen() || m.notice != menuHiddenNotice {
			t.Errorf("%s with the bar hidden: open %v, notice %q", action, m.menu.IsOpen(), m.notice)
		}
	}
	m := menuModel(t, false)
	runAction(t, m, "menu_activate")
	if !m.menu.IsOpen() || m.menu.Title() != "File" {
		t.Errorf("menu_activate opened %q, want File", m.menu.Title())
	}
}
