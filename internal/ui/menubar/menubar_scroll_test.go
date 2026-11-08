package menubar

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func wheel(x, y int, b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: b})
}

// borderCells returns the glyph at the middle of the top and the bottom border of box.
func borderCells(m Model, box dropBox) (top, bottom string) {
	plain := paintRows(m)
	cell := func(y int) string {
		runes := []rune(plain[y])
		return string(runes[box.x+box.w/2])
	}
	return cell(box.top), cell(box.bottom)
}

// TestShortFramesScrollTheDropdownWithTheWheelAndMarkTheHiddenRows covers 80x24 and
// 60x20 terminals, where the View dropdown has more rows than fit.
func TestShortFramesScrollTheDropdownWithTheWheelAndMarkTheHiddenRows(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 20}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			m := newBar(allOn(), w, h).openTitle("View")
			// The pointer is on the dropdown, so no row is highlighted.
			box := m.geometry(w, h).box
			m, _ = send(m, motion(box.x+3, box.top))
			box = m.geometry(w, h).box
			if box.visible() >= len(m.Rows()) {
				t.Fatalf("setup: all %d rows fit", len(m.Rows()))
			}
			if box.bottom > h-2 {
				t.Fatalf("bottom border %d is on or under the status row %d", box.bottom, h-1)
			}
			top, bottom := borderCells(m, box)
			if top == "▲" || bottom != "▼" {
				t.Errorf("at the top the borders show %q and %q, want a plain top and a down marker", top, bottom)
			}
			if !m.Consumes(wheel(box.x+3, box.top+2, tea.MouseWheelDown)) {
				t.Error("a wheel notch over the dropdown is not consumed, so the editor under it would scroll")
			}
			if m.Consumes(wheel(box.x+box.w+3, box.top+2, tea.MouseWheelDown)) {
				t.Error("a wheel notch outside the dropdown is consumed")
			}
			for range len(m.Rows()) {
				m, _ = send(m, wheel(box.x+3, box.top+2, tea.MouseWheelDown))
			}
			box = m.geometry(w, h).box
			if want := len(m.Rows()) - box.visible(); box.first != want {
				t.Fatalf("after many notches the window starts at %d, want the end %d", box.first, want)
			}
			top, bottom = borderCells(m, box)
			if top != "▲" || bottom == "▼" {
				t.Errorf("at the end the borders show %q and %q, want an up marker and a plain bottom", top, bottom)
			}
			last := m.Rows()[len(m.Rows())-1].Label
			if !strings.Contains(strings.Join(paintRows(m), "\n"), last) {
				t.Errorf("the last row %q cannot be reached with the mouse", last)
			}
			// The last row is clickable, and the click runs it.
			if got := m.Hit(box.x+3, box.bottom-1); got.Kind != TargetItem || got.Index != len(m.Rows())-1 {
				t.Errorf("Hit on the last visible row = %+v", got)
			}
			for range len(m.Rows()) {
				m, _ = send(m, wheel(box.x+3, box.top+2, tea.MouseWheelUp))
			}
			if got := m.geometry(w, h).box.first; got != 0 {
				t.Errorf("after wheeling up the window starts at %d", got)
			}
		})
	}
}

func TestWheelLeavesNoHighlightOutsideTheWindow(t *testing.T) {
	m := newBar(allOn(), 80, 24).openTitle("View")
	box := m.geometry(80, 24).box
	if m.Selected() != 0 {
		t.Fatalf("setup: highlight %d", m.Selected())
	}
	for range 5 {
		m, _ = send(m, wheel(box.x+3, box.top+2, tea.MouseWheelDown))
	}
	box = m.geometry(80, 24).box
	if box.first == 0 {
		t.Fatal("the wheel did not scroll")
	}
	if sel := m.Selected(); sel >= 0 && (sel < box.first || sel >= box.first+box.visible()) {
		t.Errorf("highlight %d is outside the window %d..%d", sel, box.first, box.first+box.visible())
	}
}

func TestWheelScrollsAnOpenSubmenu(t *testing.T) {
	m := newBar(allOn(), 80, 24).openTitle("Edit")
	box := m.geometry(80, 24).box
	i := rowIndex(t, m.Rows(), "Copy with Formatting")
	m, _ = send(m, motion(box.x+3, rowY(box, i)))
	if !m.SubOpen() {
		t.Fatal("hover did not open the submenu")
	}
	g := m.geometry(80, 24)
	if g.sub.visible() >= len(m.SubRows()) {
		t.Skip("all theme rows fit")
	}
	m, _ = send(m, wheel(g.sub.x+3, g.sub.top+2, tea.MouseWheelDown))
	if got := m.geometry(80, 24).sub.first; got != 1 {
		t.Errorf("submenu window starts at %d after one notch, want 1", got)
	}
}
