package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/theme"
)

// chromeKeys is testKeys with the chords the chrome tests press: Esc in the
// explorer, Ctrl+O and Ctrl+P, and Alt+PageUp and Alt+PageDown for the tab strip.
func chromeKeys() fakeKeymap {
	k := testKeys()
	k["file_explorer|Escape|"] = Command{Action: "file_explorer_search_clear"}
	k["normal|o|ctrl"] = Command{Action: "open"}
	k["normal|p|ctrl"] = Command{Action: "quick_open"}
	k["normal|PageUp|alt"] = Command{Action: "scroll_tabs_left"}
	k["normal|PageDown|alt"] = Command{Action: "scroll_tabs_right"}
	return k
}

func TestCtrlBHidesAnOpenExplorerWhenItHasNoFocus(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	m.keys = chromeKeys()
	press(m, ctrl('b'))
	press(m, ctrl('e')) // focus_editor while the explorer has focus
	if !m.explorerOpen || m.explorerFocus {
		t.Fatalf("setup: open %v focused %v, want open and unfocused", m.explorerOpen, m.explorerFocus)
	}
	press(m, ctrl('b'))
	if m.explorerOpen {
		t.Fatal("Ctrl+B on an open, unfocused explorer did not hide it")
	}
}

func TestEscInTheExplorerMovesFocusToTheEditorAndKeepsThePanel(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	m.keys = chromeKeys()
	press(m, ctrl('e'))
	press(m, special(tea.KeyEscape))
	if m.explorerFocus {
		t.Fatal("Esc left the explorer focused")
	}
	if !m.explorerOpen {
		t.Fatal("Esc hid the explorer")
	}
	if len(ed.actions) != 0 {
		t.Fatalf("Esc reached the editor: %v", ed.actions)
	}
}

func TestStatusRowGivesWayToThePalette(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, nil, 60, 20)
	m.keys = chromeKeys()
	shown := m.layout()
	press(m, ctrl('p'))
	l := m.layout()
	if len(l.Status) != 0 || l.BodyBottom != shown.BodyBottom+1 {
		t.Fatalf("palette open: %d status runs, body bottom %d; want none and %d",
			len(l.Status), l.BodyBottom, shown.BodyBottom+1)
	}
	if got := l.Hit(10, l.StatusY); got.Kind != hitEditorText {
		t.Fatalf("the status row hits %v with the palette open, want the body", got.Kind)
	}
	if ed.rows != l.BodyBottom-l.BodyTop+1 {
		t.Fatalf("the editor viewport has %d rows, want the %d body rows", ed.rows, l.BodyBottom-l.BodyTop+1)
	}
	press(m, special(tea.KeyEscape))
	if got := m.layout(); len(got.Status) == 0 || got.BodyBottom != shown.BodyBottom {
		t.Fatalf("after Esc: %d status runs, body bottom %d; want the status row back at %d",
			len(got.Status), got.BodyBottom, shown.BodyBottom)
	}
}

func TestStatusRowGivesWayToTheFileBrowser(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 60, 20)
	m.keys = chromeKeys()
	shown := m.layout()
	press(m, ctrl('o'))
	if l := m.layout(); len(l.Status) != 0 || l.BodyBottom != shown.BodyBottom+1 {
		t.Fatalf("browser open: %d status runs, body bottom %d; want none and %d",
			len(l.Status), l.BodyBottom, shown.BodyBottom+1)
	}
}

func TestEOLSegmentShowsTheDocumentLineEnding(t *testing.T) {
	ed := newFakeEditor("hello")
	ed.doc.eol = "CRLF"
	m := newTestModel(t, ed, nil, 80, 20)
	if got := m.statusView().EOL; got != "CRLF" {
		t.Fatalf("statusView eol = %q, want CRLF", got)
	}
	if row := m.paint().Plain()[m.layout().StatusY]; !strings.Contains(row, " CRLF ") {
		t.Fatalf("status row %q has no CRLF segment", row)
	}
}

func TestHiddenMenuBarLayoutMovesTheStripAndTheBodyUp(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{
		HideMenuBar: true,
		Lines:       4,
		Tabs:        []TabInfo{{Name: "a.txt", Active: true}},
	})
	if l.BodyTop != 1 || l.BodyBottom != 22 || l.StatusY != 23 {
		t.Fatalf("body %d..%d status %d, want 1..22 and 23", l.BodyTop, l.BodyBottom, l.StatusY)
	}
	if got := l.Hit(1, 0); got.Kind != hitTab {
		t.Fatalf("row 0 at the first tab hits %v, want the tab", got.Kind)
	}
	if got := l.Hit(l.TextX, 1); got.Kind != hitEditorText {
		t.Fatalf("row 1 at the text hits %v, want the editor text", got.Kind)
	}
}

func TestHiddenStatusRowGivesItsRowToTheBody(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{HideStatus: true, Lines: 4})
	if l.BodyBottom != 23 {
		t.Fatalf("body bottom %d, want 23 with the status row hidden", l.BodyBottom)
	}
	if len(l.Status) != 0 {
		t.Fatalf("%d status runs with the status row hidden", len(l.Status))
	}
	if got := l.Hit(l.TextX, 23); got.Kind != hitEditorText {
		t.Fatalf("row 23 hits %v, want the editor text", got.Kind)
	}
}

func TestExplorerRowsStartAtRowTwoWithoutMenuBar(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{ExplorerOpen: true, HideMenuBar: true, ExplorerRows: 10, Lines: 4})
	if got := l.Hit(3, 1); got.Kind != hitExplorerBody {
		t.Fatalf("the title row hits %v, want no tree row", got.Kind)
	}
	if got := l.Hit(3, 2); got.Kind != hitExplorerRow || got.Index != 0 {
		t.Fatalf("row 2 hits %+v, want the first tree row", got)
	}
	if got := l.Hit(3, 0); got.Kind != hitNone {
		t.Fatalf("row 0 over the explorer hits %v, want nothing", got.Kind)
	}
}

// With the gutter hidden the text starts PadX cells into the pane, not on its first column.
func TestHiddenLineNumbersLayoutHasNoGutter(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{HideLineNumbers: true, Lines: 4})
	if l.Gutter != 0 || l.TextX != space.PadX || l.TextW != 80-space.PadX {
		t.Fatalf("gutter %d textX %d textW %d, want 0, %d and %d", l.Gutter, l.TextX, l.TextW, space.PadX, 80-space.PadX)
	}
}

func TestToggleLineNumbersFlipsTheGutter(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 60, 20)
	if l := m.layout(); l.Gutter != 6 {
		t.Fatalf("gutter %d at start, want 6", l.Gutter)
	}
	if err := m.Run("toggle_line_numbers"); err != nil {
		t.Fatalf("Run(toggle_line_numbers) = %v", err)
	}
	if l := m.layout(); l.Gutter != 0 {
		t.Fatalf("gutter %d after the toggle, want 0", l.Gutter)
	}
	if row := m.paint().Plain()[m.layout().BodyTop]; !strings.HasPrefix(row, " hello") {
		t.Fatalf("body row %q does not start with the text", row)
	}
	if err := m.Run("toggle_line_numbers"); err != nil {
		t.Fatalf("second Run(toggle_line_numbers) = %v", err)
	}
	if l := m.layout(); l.Gutter != 6 {
		t.Fatalf("gutter %d after the second toggle, want 6", l.Gutter)
	}
}

func TestHideLineNumbersDepStartsWithoutGutter(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), HideLineNumbers: true, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if l := m.layout(); l.Gutter != 0 {
		t.Fatalf("gutter %d with HideLineNumbers, want 0", l.Gutter)
	}
}

func TestScrollTabsKeysMoveTheStripOneTab(t *testing.T) {
	m, _ := stripModel(t, 40, twelveTabs()...)
	m.keys = chromeKeys()
	pageUp := tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModAlt}
	pageDown := tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModAlt}
	for range 12 {
		press(m, pageUp)
	}
	if got := m.layout().Strip.First; got != 0 {
		t.Fatalf("Alt+PageUp left the strip at tab %d, want 0", got)
	}
	press(m, pageDown)
	if got := m.layout().Strip.First; got != 1 {
		t.Fatalf("one Alt+PageDown moved the strip to tab %d, want 1", got)
	}
	press(m, pageDown)
	press(m, pageUp)
	if got := m.layout().Strip.First; got != 1 {
		t.Fatalf("Alt+PageDown then Alt+PageUp left the strip at tab %d, want 1", got)
	}
}

func TestExplorerFollowKeepsTheSelectedRowInsideThePanelWithoutMenuBar(t *testing.T) {
	tree := newFakeTree(30)
	tree.selected = 20
	m := New(Deps{Editor: newFakeEditor("hello"), Explorer: tree, Keys: chromeKeys(), HideMenuBar: true, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	press(m, ctrl('e'))
	// The panel shows 20 tree rows, but scrolled down its first row is the pinned
	// ancestor, so the run is 19 rows and row 20 needs an offset of 2.
	if tree.offset != 2 {
		t.Fatalf("tree offset %d, want 2 so the selected row is inside the run under the pinned row", tree.offset)
	}
	off, pins := explorer.Viewport(tree.Rows(), tree.offset, m.explorerBodyRows())
	if run := m.explorerBodyRows() - len(pins); 20 < off || 20 >= off+run {
		t.Fatalf("selected row 20 is not in the run %d..%d (offset %d, %d pinned)", off, off+run-1, off, len(pins))
	}
}
