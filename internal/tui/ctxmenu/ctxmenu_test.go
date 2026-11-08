package ctxmenu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

var items = []Item{
	{Label: "Close", Action: "close"},
	{Label: "Close Others", Action: "others", Disabled: true},
	{Separator: true},
	{Label: "Copy Path", Action: "copy_path", Data: 7},
}

func newMenu() *Menu {
	m := New(Styles{Selected: grid.Style{Bold: true}, Disabled: grid.Style{Faint: true}})
	m.Show(10, 5, 80, 24, items)
	return m
}

func motion(x, y int) tea.MouseMotionMsg { return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y}) }
func click(x, y int, b tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: b})
}

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

// result runs a command and returns its message.
func result(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestShowHighlightsTheFirstRowThatCanBeChosen(t *testing.T) {
	m := newMenu()
	if m.Selected() != 0 || !m.Active() {
		t.Fatalf("selected %d, active %v", m.Selected(), m.Active())
	}
	x, y, w, h := m.Box()
	if x != 10 || y != 5 || w != len("Close Others")+4 || h != len(items)+2 {
		t.Errorf("box = %d,%d %dx%d", x, y, w, h)
	}
}

func TestKeysSkipSeparatorsAndDisabledRows(t *testing.T) {
	m := newMenu()
	m.Update(key(tea.KeyDown))
	if m.Selected() != 3 {
		t.Fatalf("Down from the first row chose %d, want 3 (the rows between cannot be chosen)", m.Selected())
	}
	m.Update(key(tea.KeyDown))
	if m.Selected() != 3 {
		t.Errorf("Down at the end moved to %d", m.Selected())
	}
	m.Update(key(tea.KeyUp))
	if m.Selected() != 0 {
		t.Errorf("Up chose %d, want 0", m.Selected())
	}
	m.Update(key(tea.KeyEnd))
	if m.Selected() != 3 {
		t.Errorf("End chose %d", m.Selected())
	}
	msg := result(m.Update(key(tea.KeyEnter)))
	c, ok := msg.(Chosen)
	if !ok || c.Item.Action != "copy_path" || c.Item.Data != 7 {
		t.Fatalf("Enter sent %#v", msg)
	}
	if m.Active() {
		t.Error("the menu is still open after a choice")
	}
}

// TestHoverOnAnAreaThatIsNotARowClearsTheHighlight is the menu-bar rule for the
// context menu: the highlight follows the pointer and never sticks.
func TestHoverOnAnAreaThatIsNotARowClearsTheHighlight(t *testing.T) {
	x, y, w, h := newMenu().Box()
	cases := []struct {
		name string
		x, y int
		want int
	}{
		{"first row", x + 3, y + 1, 0},
		{"disabled row", x + 3, y + 2, -1},
		{"separator", x + 3, y + 3, -1},
		{"last row", x + 3, y + 4, 3},
		{"top border", x + 3, y, -1},
		{"bottom border", x + 3, y + h - 1, -1},
		{"left border", x, y + 1, -1},
		{"right border", x + w - 1, y + 1, -1},
		{"outside", 60, 20, -1},
	}
	for _, c := range cases {
		m := newMenu()
		m.Update(motion(x+3, y+4)) // the last row is highlighted first
		m.Update(motion(c.x, c.y))
		if m.Selected() != c.want {
			t.Errorf("%s: highlight %d, want %d", c.name, m.Selected(), c.want)
		}
		if !m.Active() {
			t.Errorf("%s: hover closed the menu", c.name)
		}
	}
}

func TestClicks(t *testing.T) {
	x, y, _, _ := newMenu().Box()
	t.Run("left click on a row chooses it", func(t *testing.T) {
		m := newMenu()
		c, ok := result(m.Update(click(x+3, y+4, tea.MouseLeft))).(Chosen)
		if !ok || c.Item.Action != "copy_path" || m.Active() {
			t.Fatalf("chosen %#v active %v", c, m.Active())
		}
	})
	for name, pt := range map[string][2]int{"disabled row": {x + 3, y + 2}, "separator": {x + 3, y + 3}, "border": {x + 3, y}} {
		t.Run(name+" does nothing", func(t *testing.T) {
			m := newMenu()
			if msg := result(m.Update(click(pt[0], pt[1], tea.MouseLeft))); msg != nil || !m.Active() {
				t.Fatalf("msg %#v active %v", msg, m.Active())
			}
		})
	}
	t.Run("right click inside does nothing", func(t *testing.T) {
		m := newMenu()
		if msg := result(m.Update(click(x+3, y+1, tea.MouseRight))); msg != nil || !m.Active() {
			t.Fatalf("msg %#v active %v", msg, m.Active())
		}
	})
	t.Run("left click outside closes", func(t *testing.T) {
		m := newMenu()
		if msg := result(m.Update(click(60, 20, tea.MouseLeft))); msg != nil || m.Active() {
			t.Fatalf("msg %#v active %v", msg, m.Active())
		}
	})
	t.Run("right click outside closes and is sent on", func(t *testing.T) {
		m := newMenu()
		r, ok := result(m.Update(click(60, 20, tea.MouseRight))).(Reroute)
		if !ok || r.Press.X != 60 || m.Active() {
			t.Fatalf("reroute %#v active %v", r, m.Active())
		}
	})
}

func TestTheMenuStaysInsideTheFrame(t *testing.T) {
	for _, c := range []struct{ x, y, w, h, wantX, wantY int }{
		{78, 22, 80, 24, 80 - 16, 24 - 6},
		{0, 0, 80, 24, 0, 0},
		{-3, -3, 80, 24, 0, 0},
	} {
		m := New(Styles{})
		m.Show(c.x, c.y, c.w, c.h, items)
		x, y, w, h := m.Box()
		if x != c.wantX || y != c.wantY || x+w > c.w || y+h > c.h {
			t.Errorf("menu at %d,%d in %dx%d: box %d,%d %dx%d", c.x, c.y, c.w, c.h, x, y, w, h)
		}
	}
	m := New(Styles{})
	m.Show(0, 0, 4, 2, items)
	if _, _, w, _ := m.Box(); w != 0 {
		t.Errorf("a frame too small for the menu gave a box of width %d", w)
	}
}

func TestDrawPaintsBorderRowsAndTheHighlight(t *testing.T) {
	m := newMenu()
	g := grid.New(80, 24)
	m.Draw(g)
	plain := g.Plain()
	if !strings.HasPrefix(plain[5][10:], "╭") || !strings.Contains(plain[6], "│ Close") || !strings.Contains(plain[8], "──") {
		t.Fatalf("rows: %q %q %q", plain[5], plain[6], plain[8])
	}
	if !strings.Contains(plain[9], "Copy Path") || !strings.HasPrefix(plain[10][10:], "╰") {
		t.Fatalf("rows: %q %q", plain[9], plain[10])
	}
	if !g.At(12, 6).Style.Bold {
		t.Error("the highlighted row is not drawn selected")
	}
	if !g.At(12, 7).Style.Faint {
		t.Error("the disabled row is not drawn faint")
	}
}

func longItems(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{Label: "Row " + string(rune('A'+i)), Action: "row" + string(rune('A'+i))}
	}
	return out
}

// TestKeyboardStaysOnTheRowsThatAreDrawn drives a menu that is taller than the frame:
// the window follows the highlight, so Enter never chooses a row that is not shown.
func TestKeyboardStaysOnTheRowsThatAreDrawn(t *testing.T) {
	m := New(Styles{Selected: grid.Style{Bold: true}})
	m.Show(2, 2, 40, 8, longItems(12))
	for range 9 {
		m.Update(key(tea.KeyDown))
	}
	if m.Selected() != 9 {
		t.Fatalf("highlight %d after 9 Downs, want 9", m.Selected())
	}
	g := grid.New(40, 8)
	m.Draw(g)
	_, by, _, bh := m.Box()
	shown := false
	for y := by + 1; y < by+bh-1; y++ {
		if g.At(4, y).Style.Bold && strings.Contains(g.Plain()[y], "Row J") {
			shown = true
		}
	}
	if !shown {
		t.Errorf("the highlighted row 9 is not drawn selected in the window:\n%s", strings.Join(g.Plain(), "\n"))
	}
	if got, ok := result(m.Update(key(tea.KeyEnter))).(Chosen); !ok || got.Item.Action != "rowJ" {
		t.Errorf("Enter chose %+v, want the drawn row J", got)
	}
}

func TestLongMenuScrollsAndMarksTheHiddenRows(t *testing.T) {
	m := New(Styles{})
	m.Show(0, 0, 40, 8, longItems(12))
	g := grid.New(40, 8)
	m.Draw(g)
	if !strings.Contains(g.Plain()[7], "▼") || strings.Contains(g.Plain()[0], "▲") {
		t.Errorf("first window markers: %q / %q", g.Plain()[0], g.Plain()[7])
	}
	m.Update(key(tea.KeyEnd))
	g = grid.New(40, 8)
	m.Draw(g)
	if !strings.Contains(g.Plain()[0], "▲") || strings.Contains(g.Plain()[7], "▼") || !strings.Contains(g.Plain()[6], "Row L") {
		t.Errorf("last window:\n%s", strings.Join(g.Plain(), "\n"))
	}
	// A click on the drawn rows picks the row that is drawn there, not a hidden one.
	cmd := m.Update(click(3, 6, tea.MouseLeft))
	if got, ok := result(cmd).(Chosen); !ok || got.Item.Action != "rowL" {
		t.Errorf("click on the bottom row chose %+v, want row L", got)
	}
}

func TestWheelScrollsALongMenu(t *testing.T) {
	m := New(Styles{})
	m.Show(0, 0, 40, 8, longItems(12))
	g := grid.New(40, 8)
	m.Draw(g)
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 3, Y: 3, Button: tea.MouseWheelDown}))
	g = grid.New(40, 8)
	m.Draw(g)
	if !strings.Contains(g.Plain()[1], "Row B") {
		t.Errorf("after one wheel notch the first drawn row is not B:\n%s", strings.Join(g.Plain(), "\n"))
	}
}
