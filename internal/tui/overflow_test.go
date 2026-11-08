package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
)

// twelveTabs names twelve tabs, the last one active.
func twelveTabs() []string {
	names := make([]string, 12)
	for i := range names {
		names[i] = "file" + string(rune('a'+i)) + ".go"
	}
	return names
}

// stripModel is a model with the given tabs in a w by 30 frame, the explorer hidden.
func stripModel(t *testing.T, w int, names ...string) (*Model, *tabFake) {
	t.Helper()
	tf := newTabFake(names...)
	m := New(Deps{Editor: tf, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
	return m, tf
}

// tabsOnScreen lists the tab indexes that have a visible tab run in the strip.
func tabsOnScreen(m *Model) map[int]bool {
	on := map[int]bool{}
	for _, r := range m.layout().Strip.Runs {
		if r.Kind == hitTab {
			on[r.Index] = true
		}
	}
	return on
}

// plusColumn is the column of the pinned "+" run, or -1.
func plusColumn(m *Model) int {
	for _, r := range m.layout().Strip.Runs {
		if r.Kind == hitTabNew {
			return r.X
		}
	}
	return -1
}

func wheelRow(m *Model, up bool) {
	btn := tea.MouseWheelDown
	if up {
		btn = tea.MouseWheelUp
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 30, Y: 1, Button: btn}))
}

// The active tab of a twelve-tab strip at 60 columns is on screen, and the strip
// starts at the window that shows it.
func TestOverflowShowsTheActiveTab(t *testing.T) {
	m, _ := stripModel(t, 60, twelveTabs()...)
	m.Update(palette.ActivateBufferMsg{Index: 11})
	if !tabsOnScreen(m)[11] {
		t.Fatalf("active tab 11 is not on screen; first = %d", m.tabFirst)
	}
	if plus := plusColumn(m); plus != 59 {
		t.Fatalf("plus at column %d, want the last column 59", plus)
	}
}

// Each wheel notch over the tab row moves the strip by one tab, both ways.
func TestWheelOverTabRowScrollsByOneTab(t *testing.T) {
	m, _ := stripModel(t, 60, twelveTabs()...)
	start := m.layout().Strip.First
	wheelRow(m, false)
	if got := m.layout().Strip.First; got != start+1 {
		t.Fatalf("wheel down: first = %d, want %d", got, start+1)
	}
	wheelRow(m, true)
	wheelRow(m, true)
	if got := m.layout().Strip.First; got != max(start-1, 0) {
		t.Fatalf("wheel up twice: first = %d, want %d", got, max(start-1, 0))
	}
}

// A click on ">" scrolls by one tab, the "+" stays on screen, and clicking it opens the
// menu of new tabs, whose New File row opens an untitled tab.
func TestClickRightMarkerScrollsAndPlusOpensItsMenu(t *testing.T) {
	m, tf := stripModel(t, 60, twelveTabs()...)
	tf.active = 0
	tf.sync()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	before := m.layout().Strip.First
	markerX := -1
	for _, r := range m.layout().Strip.Runs {
		if r.Kind == hitTabScrollAfter {
			markerX = r.X
		}
	}
	if markerX < 0 {
		t.Fatal("no right marker drawn for a twelve-tab strip")
	}
	m.Update(click(markerX, 1, tea.MouseLeft))
	if got := m.layout().Strip.First; got != before+1 {
		t.Fatalf("click on > moved first from %d to %d, want one tab", before, got)
	}
	if plus := plusColumn(m); plus != 59 {
		t.Fatalf("plus at column %d after scrolling, want 59", plus)
	}
	_, cmd := m.Update(click(59, 1, tea.MouseLeft))
	drain(t, m, cmd)
	if n := len(tf.names); n != 12 {
		t.Fatalf("clicking + changed the tabs to %v, want the menu only", tf.names)
	}
	chooseRow(t, m, "New File")
	if n := len(tf.names); n != 13 || tf.names[12] != "Untitled-13" {
		t.Fatalf("New File left tabs %v, want an untitled tab added", tf.names)
	}
	if !tabsOnScreen(m)[12] {
		t.Fatal("the new untitled tab is not on screen")
	}
}

// Activating a tab that is far to the left scrolls the strip back to it.
func TestActivatingFarLeftTabScrollsIntoView(t *testing.T) {
	m, _ := stripModel(t, 60, twelveTabs()...)
	m.Update(palette.ActivateBufferMsg{Index: 11})
	if m.layout().Strip.First == 0 {
		t.Fatal("the strip did not scroll to the last tab; the test needs an offset")
	}
	m.Update(palette.ActivateBufferMsg{Index: 0})
	if !tabsOnScreen(m)[0] {
		t.Fatalf("tab 0 is not on screen after activating it; first = %d", m.tabFirst)
	}
	if got := m.layout().Strip.First; got != 0 {
		t.Fatalf("first = %d, want the strip at its start for tab 0", got)
	}
}

// Markers show only while the tabs overflow the row.
func TestMarkersAppearOnlyWhenOverflowing(t *testing.T) {
	m, _ := stripModel(t, 100, "a.go", "b.go", "c.go")
	if s := m.layout().Strip; s.Overflow || s.Before || s.After {
		t.Fatalf("three tabs at 100 columns: %+v", s)
	}
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 30})
	if s := m.layout().Strip; !s.Overflow {
		t.Fatal("three tabs at 20 columns do not overflow")
	}
}

// Adding and closing tabs keep the active tab on screen. The + menu's New File row adds
// the tab.
func TestAddingATabKeepsTheNewTabVisible(t *testing.T) {
	m, _ := stripModel(t, 60, twelveTabs()...)
	_, cmd := m.Update(click(59, 1, tea.MouseLeft))
	drain(t, m, cmd)
	chooseRow(t, m, "New File")
	if !tabsOnScreen(m)[12] {
		t.Fatalf("the new tab 12 is not on screen; first = %d", m.tabFirst)
	}
}

// A wheel scroll survives an unrelated key press, which changes no tab.
func TestWheelScrollIsNotUndoneByAnUnrelatedKey(t *testing.T) {
	m, _ := stripModel(t, 60, twelveTabs()...)
	wheelRow(m, false)
	wheelRow(m, false)
	first := m.layout().Strip.First
	press(m, typed("x"))
	if got := m.layout().Strip.First; got != first {
		t.Fatalf("a key press moved the strip from %d to %d", first, got)
	}
}

// A resize shows the active tab again, since the frame it was scrolled in changed.
func TestResizeRevealsTheActiveTab(t *testing.T) {
	m, _ := stripModel(t, 60, twelveTabs()...)
	for range 4 {
		wheelRow(m, false)
	}
	if tabsOnScreen(m)[0] {
		t.Fatal("the wheel did not scroll the active tab 0 off screen")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	if !tabsOnScreen(m)[0] {
		t.Fatalf("after a resize the active tab 0 is not on screen; first = %d", m.tabFirst)
	}
}
