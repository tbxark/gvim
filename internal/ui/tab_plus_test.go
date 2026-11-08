package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// plusCell returns the frame column of the "+" new-tab button on the tab strip.
func plusCell(t *testing.T, m *Model) (int, int) {
	t.Helper()
	l := m.layout()
	for x := range m.width {
		if l.hit(x, l.stripY).Kind == hitTabNew {
			return x, l.stripY
		}
	}
	t.Fatal("no + button on the tab strip")
	return 0, 0
}

// TestPlusButtonOffersNewTerminalAndNewFile: as Fresh's "+" does, a click opens a small
// menu with New Terminal and New File, and choosing New File opens a new tab.
func TestPlusButtonOffersNewTerminalAndNewFile(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt")
	before := len(ed.Tabs())
	x, y := plusCell(t, m)
	mouseDownAt(m, x, y)
	mouseUpAt(m, x, y)
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("a click on + opened no menu")
	}
	if got := menuLabels(menu); !slices.Equal(got, []string{"New Terminal", "New File"}) {
		t.Fatalf("the + menu is %v, want New Terminal and New File", got)
	}
	if len(ed.Tabs()) != before {
		t.Fatalf("the click opened a tab before a row was chosen")
	}
	chooseRow(t, m, "New File")
	if n := len(ed.actions); n == 0 || ed.actions[n-1] != "new" {
		t.Errorf("editor actions = %v, want new last", ed.actions)
	}
}

// TestPlusMenuEscapeClosesWithoutAction keeps the menu from opening a tab when dismissed.
func TestPlusMenuEscapeClosesWithoutAction(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt")
	before := len(ed.Tabs())
	x, y := plusCell(t, m)
	mouseDownAt(m, x, y)
	mouseUpAt(m, x, y)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if ctxTop(m) != nil || len(ed.Tabs()) != before {
		t.Errorf("after Esc: menu open %v, tabs %d (was %d)", ctxTop(m) != nil, len(ed.Tabs()), before)
	}
}
