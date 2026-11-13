package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// dockBodyPoint returns a pane cell in the dock's shell rows.
func dockBodyPoint(m *Model, x, row int) (int, int) {
	l := m.layout()
	return x, l.DockY + 1 + row
}

func TestDockDragSelectsAndEntersScrollback(t *testing.T) {
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 80, liveCellsForDock())
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	x, y := dockBodyPoint(m, 1, 0)
	x2, y2 := dockBodyPoint(m, 3, 1)
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	dockSettle(t, m, cmd)
	if m.dock.Scrolling() || m.dock.Selection() {
		t.Fatalf("a press without a drag: scrolling %v selection %v", m.dock.Scrolling(), m.dock.Selection())
	}
	_, cmd = m.Update(tea.MouseMotionMsg(tea.Mouse{X: x2, Y: y2, Button: tea.MouseLeft}))
	dockSettle(t, m, cmd)
	if !m.dock.Scrolling() || !m.dock.Selection() {
		t.Fatalf("after the drag: scrolling %v selection %v, want both", m.dock.Scrolling(), m.dock.Selection())
	}
}

func TestDockShiftDragSelectsInsteadOfForwarding(t *testing.T) {
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 80, liveCellsForDock())
	starter.term.mouse = true
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	x, y := dockBodyPoint(m, 1, 0)
	x2, y2 := dockBodyPoint(m, 3, 1)
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModShift}))
	dockSettle(t, m, cmd)
	_, cmd = m.Update(tea.MouseMotionMsg(tea.Mouse{X: x2, Y: y2, Button: tea.MouseLeft, Mod: tea.ModShift}))
	dockSettle(t, m, cmd)
	if len(starter.term.mice) != 0 {
		t.Fatalf("the Shift+drag was forwarded: %+v", starter.term.mice)
	}
	if !m.dock.Selection() {
		t.Fatal("the Shift+drag did not select")
	}
}

func TestDeadShellTakesFocusAndScrollback(t *testing.T) {
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 80, liveCellsForDock())
	starter.term.history = []string{"line-1", "line-2"}
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	close(starter.term.done)
	time.Sleep(10 * time.Millisecond)
	dockSettle(t, m, nil)
	if !m.dock.Exited() {
		t.Fatalf("the shell did not exit: exited %v", m.dock.Exited())
	}
	if m.dock.Focus() {
		t.Fatal("an exited dock kept the focus")
	}
	// A press on the frozen screen takes the focus.
	x, y := dockBodyPoint(m, 1, 0)
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	dockSettle(t, m, cmd)
	if !m.dock.Focus() {
		t.Fatal("a press did not focus the exited dock")
	}
	// The exit already opened the read-only pager over its frozen screen and
	// history (Fresh's read-only exited terminal); Ctrl+Space returns to the frozen
	// screen and opens the pager again.
	if !m.dock.Scrolling() {
		t.Fatal("the exit did not show the read-only transcript automatically")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	dockSettle(t, m, cmd)
	if m.dock.Scrolling() {
		t.Fatal("Ctrl+Space did not return to the frozen live screen")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	dockSettle(t, m, cmd)
	if !m.dock.Scrolling() {
		t.Fatal("Ctrl+Space did not reopen the exited shell's scrollback")
	}
}

func TestTerminalStateRestoresThroughTheModel(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	st := m.TerminalState()
	if st.Version != terminal.SavedStateVersion {
		t.Fatalf("terminal state version = %d", st.Version)
	}
	m.RestoreTerminal(terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Open: true, Cwd: "/restored", Rows: []string{"old"}}}})
	if !m.dock.Restored() {
		t.Fatal("RestoreTerminal did not put the dock in the restored state")
	}
}

// liveCellsForDock is a fake screen with the rows live-1 to live-4.
func liveCellsForDock() []terminal.Cell {
	cells := make([]terminal.Cell, 4*80)
	for r := 0; r < 4; r++ {
		for i, ch := range []rune("live-" + string(rune('1'+r))) {
			cells[r*80+i] = terminal.Cell{Rune: ch}
		}
	}
	return cells
}
