package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestTerminalTabCommands drives the dock's tab commands through the palette and
// the tab quick-switch keys on a model.
func TestTerminalTabCommands(t *testing.T) {
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
	dockRun(t, m, "open_terminal")
	if got := m.dock.Count(); got != 1 {
		t.Fatalf("tabs = %d, want 1", got)
	}
	dockRun(t, m, "terminal_tab_new")
	if got := m.dock.Count(); got != 2 || len(starter.dirs) != 2 {
		t.Fatalf("tabs = %d, starts = %d, want 2 and 2", got, len(starter.dirs))
	}
	if got := m.dock.ActiveIndex(); got != 1 {
		t.Fatalf("active tab = %d, want the new one", got)
	}
	dockRun(t, m, "terminal_tab_previous")
	if got := m.dock.ActiveIndex(); got != 0 {
		t.Fatalf("active tab after previous = %d, want 0", got)
	}
	dockRun(t, m, "terminal_tab_next")
	if got := m.dock.ActiveIndex(); got != 1 {
		t.Fatalf("active tab after next = %d, want 1", got)
	}
	// The documented quick-switch keys work while the dock has the keyboard.
	dockSettle(t, m, press(m, tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModCtrl}))
	if got := m.dock.ActiveIndex(); got != 0 {
		t.Fatalf("active tab after Ctrl+PgUp = %d, want 0", got)
	}
	dockSettle(t, m, press(m, tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModCtrl}))
	if got := m.dock.ActiveIndex(); got != 1 {
		t.Fatalf("active tab after Ctrl+PgDown = %d, want 1", got)
	}
	// Rename the active tab through its prompt.
	dockRun(t, m, "terminal_tab_rename")
	press(m, typed("build"))
	dockSettle(t, m, press(m, special(tea.KeyEnter)))
	if names := m.dock.TabNames(); len(names) < 2 || names[1] != "build" {
		t.Fatalf("tab names = %q, want the renamed active tab", names)
	}
	// Close the active tab: its shell is closed and the tab is gone.
	dockRun(t, m, "terminal_tab_close")
	if got := m.dock.Count(); got != 1 {
		t.Fatalf("tabs after close = %d, want 1", got)
	}
	if !starter.terms[1].closed {
		t.Fatal("closing the tab did not close its shell")
	}
	dockRun(t, m, "terminal_tab_close")
	if got := m.dock.Count(); got != 0 || m.dock.Open() {
		t.Fatalf("tabs = %d open = %v after closing the last tab", got, m.dock.Open())
	}
}

func TestTerminalTabReorderCommands(t *testing.T) {
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
	dockRun(t, m, "open_terminal")
	dockRun(t, m, "terminal_tab_new")
	dockRun(t, m, "terminal_tab_new")
	if m.dock.Count() != 3 || len(starter.dirs) != 3 {
		t.Fatalf("tabs = %d starts = %d, want 3", m.dock.Count(), len(starter.dirs))
	}
	// Give the tabs distinct names so the order is observable (the default names are
	// positional and stay "Terminal N" after a reorder).
	for i, name := range []string{"one", "two", "three"} {
		m.dock.SetActive(i)
		dockRun(t, m, "terminal_tab_rename")
		press(m, typed(name))
		dockSettle(t, m, press(m, special(tea.KeyEnter)))
	}
	names := func() string { return strings.Join(m.dock.TabNames(), ",") }
	if got := names(); got != "one,two,three" {
		t.Fatalf("names = %q", got)
	}
	// The third tab is active; move it left, then back right.
	dockRun(t, m, "terminal_tab_move_left")
	if got := names(); got != "one,three,two" {
		t.Fatalf("order after move left = %q", got)
	}
	if m.dock.ActiveIndex() != 1 {
		t.Fatalf("active after move left = %d", m.dock.ActiveIndex())
	}
	dockRun(t, m, "terminal_tab_move_right")
	if got := names(); got != "one,two,three" {
		t.Fatalf("order after move right = %q", got)
	}
	// Keyboard chord: Ctrl+Shift+PgUp moves the active tab left.
	m.dock.SetActive(1)
	dockSettle(t, m, press(m, tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModCtrl | tea.ModShift}))
	if got := names(); got != "two,one,three" {
		t.Fatalf("order after Ctrl+Shift+PgUp = %q", got)
	}
	if m.dock.ActiveIndex() != 0 {
		t.Fatalf("active after Ctrl+Shift+PgUp = %d", m.dock.ActiveIndex())
	}
	// At the first tab a move left is a no-op, and no shell was touched.
	dockRun(t, m, "terminal_tab_move_left")
	if got := names(); got != "two,one,three" {
		t.Fatalf("edge move changed the order: %q", got)
	}
	if len(starter.dirs) != 3 {
		t.Fatalf("reorder started shells: %d", len(starter.dirs))
	}
	if !m.dock.Running() {
		t.Fatal("reorder stopped the running shell")
	}
}
