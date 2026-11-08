package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
)

// tickOnce sends the autoscroll tick in flight and returns the head of the selection.
func tickOnce(t *testing.T, m *Model) {
	t.Helper()
	if m.drag.tick == 0 {
		t.Fatal("no autoscroll tick is in flight")
	}
	m.Update(dragScrollMsg{seq: m.drag.tick})
}

// TestAutoscrollSpeedGrowsWithTheDistanceAndIsCapped: held outside the pane, the
// selection grows by a number of lines per tick that rises with the distance from the
// edge and stops rising at dragMaxStep, instead of racing at one speed.
func TestAutoscrollSpeedGrowsWithTheDistanceAndIsCapped(t *testing.T) {
	for _, dir := range []struct {
		name string
		sign int
	}{{"below", 1}, {"above", -1}} {
		t.Run(dir.name, func(t *testing.T) {
			prev := 0
			for _, d := range []int{0, 1, 2, 4, 20} {
				m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("line\n", 5000), 60, 20)
				l := m.layout()
				start := 2500
				ed.SetCursor(buffer.Pos{Line: start}, false)
				m.syncViewport()
				m.revealCaret()
				y := l.bodyTop + 3
				m.Update(tea.MouseClickMsg(tea.Mouse{X: l.textX + 1, Y: y, Button: tea.MouseLeft}))
				var out int
				if dir.sign > 0 {
					out = l.bodyBottom + d
				} else {
					out = l.bodyTop - d
				}
				m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.textX + 1, Y: out, Button: tea.MouseLeft}))
				_, before, _ := selectionOf(ed)
				tickOnce(t, m)
				_, after, _ := selectionOf(ed)
				step := (after.Line - before.Line) * dir.sign
				want := min(1+d, dragMaxStep)
				if step != want {
					t.Errorf("%d rows %s the pane: a tick moved the head %d lines, want %d", d, dir.name, step, want)
				}
				if step < prev {
					t.Errorf("%d rows %s the pane is slower (%d) than a nearer pointer (%d)", d, dir.name, step, prev)
				}
				prev = step
			}
			if prev != dragMaxStep {
				t.Errorf("the speed far from the pane is %d lines per tick, want the cap %d", prev, dragMaxStep)
			}
		})
	}
}

// TestAutoscrollRateIsSane pins the tick period and the cap, so a held pointer cannot
// run at more than about a hundred lines a second.
func TestAutoscrollRateIsSane(t *testing.T) {
	perSecond := float64(dragMaxStep) * float64(1e9) / float64(dragScrollEvery.Nanoseconds())
	if perSecond > 100 {
		t.Errorf("the cap is %.0f lines per second", perSecond)
	}
	if one := float64(1e9) / float64(dragScrollEvery.Nanoseconds()); one > 20 {
		t.Errorf("one row past the edge runs at %.0f lines per second, want a gentle start", one)
	}
}

// TestDragToThePaneEdgePansHorizontallyWhileHeld: with Wrap off, holding the pointer at
// the right edge keeps moving the head along the line and the view follows, with no
// further motion events; the scrollbar column, one past the text, is faster. Held at
// the left edge it comes back.
func TestDragToThePaneEdgePansHorizontallyWhileHeld(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("x", 400)+"\nnext\n", 60, 12)
	runAction(t, m, "toggle_line_wrap")
	l := m.layout()
	fp := l.focusedPane()
	left := func() int {
		v, _ := m.paneGeometry(m.layout().focusedPane(), m.ed.Active()).HScroll(fp.W, fp.H)
		return v
	}
	mouseDownAt(m, l.textX+2, l.bodyTop)
	edge := fp.X + fp.W - 2 // the last text column
	_, cmd := m.Update(tea.MouseMotionMsg(tea.Mouse{X: edge, Y: l.bodyTop, Button: tea.MouseLeft}))
	if cmd == nil {
		t.Fatal("a drag to the right edge armed no autoscroll tick")
	}
	_, head0, _ := selectionOf(ed)
	left0 := left()
	tickOnce(t, m)
	_, head1, _ := selectionOf(ed)
	if head1.Col-head0.Col != 1 || head1.Line != 0 {
		t.Fatalf("a tick at the last text column moved the head %v to %v, want one column", head0, head1)
	}
	for range 60 {
		tickOnce(t, m)
	}
	_, head2, _ := selectionOf(ed)
	if head2.Col <= head1.Col+5 || left() <= left0 {
		t.Fatalf("after sixty ticks the head is at %v and the view at %d (was %d)", head2, left(), left0)
	}
	// On the scrollbar column the pan is faster.
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: fp.X + fp.W - 1, Y: l.bodyTop, Button: tea.MouseLeft}))
	_, a, _ := selectionOf(ed)
	tickOnce(t, m)
	_, b, _ := selectionOf(ed)
	if b.Col-a.Col != 2 {
		t.Errorf("a tick on the scrollbar column moved the head %d columns, want 2", b.Col-a.Col)
	}
	// Held at the left edge it pans back.
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.textX, Y: l.bodyTop, Button: tea.MouseLeft}))
	_, c, _ := selectionOf(ed)
	viewBefore := left()
	tickOnce(t, m)
	_, d, _ := selectionOf(ed)
	if d.Col >= c.Col || left() >= viewBefore {
		t.Errorf("a tick at the left edge: head %v to %v, view %d to %d, want both to move back", c, d, viewBefore, left())
	}
	// The chain ends once the head cannot move: at the end of the line.
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: fp.X + fp.W - 1, Y: l.bodyTop, Button: tea.MouseLeft}))
	ended := false
	for range 400 {
		if _, cmd := m.Update(dragScrollMsg{seq: m.drag.tick}); cmd == nil {
			ended = true
			break
		}
	}
	if _, end, _ := selectionOf(ed); !ended || end.Col != 400 {
		t.Fatalf("ended %v, head %v, want the end of the line (col 400)", ended, end)
	}
}

// TestDragEndingOnTheScrollbarColumnKeepsTheCaretInView: a release on the scrollbar
// column used to leave the caret outside the window.
func TestDragEndingOnTheScrollbarColumnKeepsTheCaretInView(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("x", 400)+"\n", 60, 12)
	runAction(t, m, "toggle_line_wrap")
	l := m.layout()
	fp := l.focusedPane()
	mouseDownAt(m, l.textX+2, l.bodyTop)
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: fp.X + fp.W - 1, Y: l.bodyTop, Button: tea.MouseLeft}))
	for range 30 {
		m.Update(dragScrollMsg{seq: m.drag.tick})
	}
	mouseUpAt(m, fp.X+fp.W-1, l.bodyTop)
	pane := m.paneGeometry(m.layout().focusedPane(), m.ed.Active())
	if _, _, ok := pane.Caret(fp.W, fp.H); !ok {
		t.Errorf("the caret %v is outside the window (left %d)", ed.Active().Cursor, func() int { v, _ := pane.HScroll(fp.W, fp.H); return v }())
	}
}
