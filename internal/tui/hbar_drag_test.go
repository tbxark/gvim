package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// wideDoc is a document taller than the frame whose first line is wider than the text
// area, so that with Wrap off both scrollbars show.
func wideDoc(lines, wide int) string {
	return strings.Repeat("x", wide) + "\n" + strings.Repeat("line\n", lines-1)
}

func mouseMove(m *Model, x, y int) {
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
}

func mouseDownAt(m *Model, x, y int) {
	m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
}

func mouseUpAt(m *Model, x, y int) {
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
}

// TestScrollbarThumbDragReachesTheEndWithTheHorizontalBarShowing is the long-file case
// of the vertical scrollbar: the thumb follows the pointer down to the last track row,
// and that row is the last page, also when the horizontal bar takes a row of the pane.
func TestScrollbarThumbDragReachesTheEndWithTheHorizontalBarShowing(t *testing.T) {
	const lines = 2695
	m, ed := viewModel(t, DefaultViewOptions(), wideDoc(lines, 200), 80, 40)
	runAction(t, m, "toggle_line_wrap")
	l := m.layout()
	fp := l.focusedPane()
	body := m.paneGeometry(fp, m.ed.Active()).BodyRows(fp.w, fp.h)
	if body != fp.h-1 {
		t.Fatalf("setup: %d text rows in a %d row pane, want the horizontal bar to take one", body, fp.h)
	}
	x := fp.x + fp.w - 1
	mouseDownAt(m, x, fp.y) // the thumb at the top
	for y := fp.y; y < fp.y+fp.h+3; y++ {
		mouseMove(m, x, y)
	}
	mouseUpAt(m, x, fp.y+fp.h)
	if top, want := ed.Active().Top, m.ed.Active().LineCount()-m.bodyRows(); top != want {
		t.Fatalf("top = %d after dragging the thumb past the bottom, want the last page %d", top, want)
	}
	// A press on the last track row goes to the last page too.
	mouseDownAt(m, x, fp.y+3)
	mouseUpAt(m, x, fp.y+3)
	// The bar leaves once the wide line is scrolled off, so the track is one row longer.
	last := fp.y + m.bodyRows() - 1
	mouseDownAt(m, x, last)
	mouseUpAt(m, x, last)
	if top, want := ed.Active().Top, m.ed.Active().LineCount()-m.bodyRows(); top != want {
		t.Fatalf("top = %d after a press on the last track row, want %d", top, want)
	}
}

// TestScrollbarThumbDragToTheLastBodyRowReachesTheEnd drags the thumb to the last row
// of the text, not onto the horizontal bar under it. The pointer row must map to the
// same track as a press on that row, so the drag ends on the last page.
func TestScrollbarThumbDragToTheLastBodyRowReachesTheEnd(t *testing.T) {
	const lines = 2695
	m, ed := viewModel(t, DefaultViewOptions(), wideDoc(lines, 300), 80, 24)
	runAction(t, m, "toggle_line_wrap")
	fp := m.layout().focusedPane()
	if body := m.bodyRows(); body != fp.h-1 {
		t.Fatalf("setup: %d text rows in a %d row pane, want the horizontal bar to take one", body, fp.h)
	}
	x := fp.x + fp.w - 1
	body := m.bodyRows()
	mouseDownAt(m, x, fp.y)
	last := fp.y + body - 1
	mouseMove(m, x, last)
	mouseUpAt(m, x, last)
	// The last page is the one whose last line is on the last text row while the bar
	// shows. Once the wide line scrolls off the bar hides, as it does for the wheel.
	want := m.ed.Active().LineCount() - body
	if top := ed.Active().Top; top != want {
		t.Fatalf("top = %d after dragging the thumb to the last text row, want the last page %d", top, want)
	}
}

// TestHorizontalScrollbarClickAndDragPanTheView: the bar on the last row of the pane
// moves the view sideways. A press on the track centres the thumb there, and a drag
// carries it; the ends of the track are the ends of the longest line.
func TestHorizontalScrollbarClickAndDragPanTheView(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), wideDoc(30, 300), 80, 12)
	runAction(t, m, "toggle_line_wrap")
	fp := m.layout().focusedPane()
	pane := m.paneGeometry(fp, m.ed.Active())
	_, most := pane.HScroll(fp.w, fp.h)
	if most <= 0 {
		t.Fatalf("setup: the view cannot pan (most %d)", most)
	}
	barY := fp.y + fp.h - 1
	left := func() int {
		l, _ := m.paneGeometry(m.layout().focusedPane(), m.ed.Active()).HScroll(fp.w, fp.h)
		return l
	}
	caretBefore := ed.Active().Cursor

	mouseDownAt(m, fp.x+fp.w-2, barY) // the right end of the track
	if got := left(); got != most {
		t.Fatalf("a press on the right end of the bar gave left %d, want %d", got, most)
	}
	mouseMove(m, fp.x+fp.w/2, barY)
	mid := left()
	if mid <= 0 || mid >= most {
		t.Fatalf("dragging to the middle gave left %d, want it between 0 and %d", mid, most)
	}
	mouseMove(m, fp.x-5, barY) // left of the track: the first column
	if got := left(); got != 0 {
		t.Fatalf("dragging past the left end gave left %d, want 0", got)
	}
	mouseUpAt(m, fp.x, barY)
	if ed.Active().Cursor != caretBefore {
		t.Errorf("the caret moved from %v to %v while the bar was dragged", caretBefore, ed.Active().Cursor)
	}
	// After the release a move does not drag the bar.
	mouseMove(m, fp.x+fp.w-2, barY)
	if got := left(); got != 0 {
		t.Errorf("a move after the release panned the view to %d", got)
	}
}
