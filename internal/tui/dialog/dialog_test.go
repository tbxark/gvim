package dialog

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// fakeClock is a Clock that only moves when the test advances it.
type fakeClock struct{ now time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_000_000, 0)}
}

// Now is the Clock method value the dialogs read.
func (c *fakeClock) Now() time.Time { return c.now }

// advance moves the clock forward by d.
func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// afterGrace moves the clock past the grace period of a dialog opened now.
func (c *fakeClock) afterGrace() { c.advance(graceQuiet) }

// key returns the key press named by name: "enter", "esc", "tab", "shift+tab",
// "left", "right", "up", "down", "backspace", or one typed character.
func key(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

// altKey returns Alt plus the letter.
func altKey(letter rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: letter, Mod: tea.ModAlt}
}

// click returns a left click at (x, y).
func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// motion returns a mouse move to (x, y).
func motion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y}
}

// answer runs cmd and returns the Result it carries, if any.
func answer(cmd tea.Cmd) (Result, bool) {
	if cmd == nil {
		return Result{}, false
	}
	r, ok := cmd().(Result)
	return r, ok
}

// typeText sends each rune of s as a key press.
func typeText(t *testing.T, d Overlay, s string) Overlay {
	t.Helper()
	for _, r := range s {
		d, _ = d.Update(key(string(r)))
	}
	return d
}

// draw paints d into a new w by h grid.
func draw(d Overlay, w, h int) *grid.Grid {
	g := grid.New(w, h)
	d.Draw(g)
	return g
}

// screen returns the plain text of g, one line per row.
func screen(g *grid.Grid) string {
	return strings.Join(g.Plain(), "\n")
}

// find returns the cell column and row of the first occurrence of s in g.
func find(g *grid.Grid, s string) (int, int, bool) {
	for y, line := range g.Plain() {
		before, _, ok := strings.Cut(line, s)
		if ok {
			return utf8.RuneCountInString(before), y, true
		}
	}
	return 0, 0, false
}

// mustFind is find, failing the test when s is not drawn.
func mustFind(t *testing.T, g *grid.Grid, s string) (int, int) {
	t.Helper()
	x, y, ok := find(g, s)
	if !ok {
		t.Fatalf("%q is not drawn in:\n%s", s, screen(g))
	}
	return x, y
}

// mustClose asserts that d answered with the choice and text, and is closed.
func mustClose(t *testing.T, d Overlay, cmd tea.Cmd, want Choice, text string) {
	t.Helper()
	r, ok := answer(cmd)
	if !ok {
		t.Fatalf("no Result command, want %v", want)
	}
	if r.Choice != want || r.Text != text {
		t.Fatalf("Result = {%v %q}, want {%v %q}", r.Choice, r.Text, want, text)
	}
	if next, _ := d.Update(key("tab")); next != nil {
		t.Fatalf("a closed dialog still returns itself")
	}
	if d.Active() {
		t.Fatalf("Active() = true after the answer")
	}
}

// mustOpen asserts that d is still open and produced no command.
func mustOpen(t *testing.T, d Overlay, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		t.Fatalf("unexpected command from an open dialog")
	}
	if !d.Active() {
		t.Fatalf("dialog closed, want open")
	}
}
