package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// dockFileEditor is the fake editor with the file methods the root opens a path
// through; it records the paths it was asked to open.
type dockFileEditor struct {
	*fakeEditor
	opened []string
}

func (e *dockFileEditor) OpenFile(path string) error { e.opened = append(e.opened, path); return nil }
func (e *dockFileEditor) OpenPreview(path string) error {
	e.opened = append(e.opened, path)
	return nil
}
func (e *dockFileEditor) GotoLine(line, col int) error { return nil }

// dockHistory is a history of n numbered lines for the model-level tests.
func dockHistory(n int) []string {
	history := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		history = append(history, "line-"+itoa(i))
	}
	return history
}

// itoa formats a small positive number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestDockCtrlSpaceEntersScrollback(t *testing.T) {
	starter := newFakeStarter()
	starter.term.history = dockHistory(30)
	starter.term.screen = terminal.NewScreen(4, 80, nil)
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	if !m.dock.Focus() {
		t.Fatal("the dock did not take the focus")
	}
	// Ctrl+Space enters the scrollback view; the terminal_escape binding on the
	// same key only runs from Ctrl+] then.
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}))
	dockSettle(t, m, cmd)
	if !m.dock.Scrolling() {
		t.Fatal("Ctrl+Space did not enter the scrollback view")
	}
	if !m.dock.Focus() {
		t.Fatal("entering scrollback gave up the dock focus")
	}
	// Esc returns to the live screen.
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	dockSettle(t, m, cmd)
	if m.dock.Scrolling() {
		t.Fatal("Esc did not return to the live screen")
	}
	// And the keys reach the shell again: a typed key is written, not consumed.
	before := len(starter.term.writes)
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	dockSettle(t, m, cmd)
	if len(starter.term.writes) <= before {
		t.Fatal("the live view did not send the key to the shell")
	}
}

func TestDockWheelUpEntersScrollback(t *testing.T) {
	starter := newFakeStarter()
	starter.term.history = dockHistory(30)
	starter.term.screen = terminal.NewScreen(4, 80, nil)
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	l := m.layout()
	_, cmd := m.Update(tea.MouseWheelMsg(tea.Mouse{X: 3, Y: l.DockY + 1, Button: tea.MouseWheelUp}))
	dockSettle(t, m, cmd)
	if !m.dock.Scrolling() {
		t.Fatal("an upward wheel on the live screen did not enter scrollback")
	}
	if m.dock.ScrollTop() == 0 {
		t.Fatal("the wheel did not scroll the window up")
	}
}

func TestDockCtrlClickOpensAPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The fake screen is wider than the dock so the whole path is readable.
	cells := make([]terminal.Cell, 4*200)
	for i, r := range []rune("see " + path) {
		if i < 200 {
			cells[i] = terminal.Cell{Rune: r}
		}
	}
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 200, cells)
	ed := &dockFileEditor{fakeEditor: newFakeEditor("hello")}
	m := newDockModel(t, ed, DockPorts{Starter: starter})
	openDock(t, m)
	l := m.layout()
	x := strings.Index("see "+path, path) + 2
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: l.DockY + 1, Button: tea.MouseLeft, Mod: tea.ModCtrl}))
	dockSettle(t, m, cmd)
	if len(ed.opened) != 1 || ed.opened[0] != path {
		t.Fatalf("opened = %v, want %s", ed.opened, path)
	}
	if m.dock.Focus() {
		t.Fatal("the dock kept the focus after the link opened")
	}
}

func TestDockMouseForwardedFromTheRoot(t *testing.T) {
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 80, nil)
	starter.term.mouse = true
	ed := newFakeEditor("hello")
	m := newDockModel(t, ed, DockPorts{Starter: starter})
	openDock(t, m)
	l := m.layout()
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: l.DockY + 2, Button: tea.MouseLeft}))
	dockSettle(t, m, cmd)
	if len(starter.term.mice) != 1 {
		t.Fatalf("forwarded = %+v, want the shell's press", starter.term.mice)
	}
	if got := starter.term.mice[0]; got.X != 4 || got.Y != 1 || got.Kind != terminal.MousePress {
		t.Fatalf("press = %+v, want (4,1) into the shell", got)
	}
	// The press did not reach the editor behind the dock: no action ran there.
	if len(ed.actions) != 0 {
		t.Fatalf("the dock press reached the editor: %v", ed.actions)
	}
}

func TestDockShiftClickIsNotForwarded(t *testing.T) {
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 80, nil)
	starter.term.mouse = true
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	l := m.layout()
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: l.DockY + 1, Button: tea.MouseLeft, Mod: tea.ModShift}))
	dockSettle(t, m, cmd)
	if len(starter.term.mice) != 0 {
		t.Fatalf("a Shift+Click was forwarded: %+v", starter.term.mice)
	}
}

func TestDockCloseOnExitHidesTheDock(t *testing.T) {
	starter := newFakeStarter()
	starter.term.screen = terminal.NewScreen(4, 80, nil)
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{
		Starter: starter,
		Effects: DockEffects{CloseOnExit: true},
	})
	openDock(t, m)
	if !m.dock.Open() {
		t.Fatal("the dock did not open")
	}
	close(starter.term.done)
	// The exit watcher parks in the pump until the channel closes; the next
	// settle delivers the exit message.
	time.Sleep(10 * time.Millisecond)
	dockSettle(t, m, nil)
	if m.dock.Open() {
		t.Fatal("terminal.close_on_exit did not close the dock")
	}
}
