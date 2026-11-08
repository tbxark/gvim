package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pressDrag presses the left button at (x1, y1), moves it to (x2, y2) and releases it, as a
// mouse drag does.
func pressDrag(m *Model, x1, y1, x2, y2 int) {
	m.Update(click(x1, y1, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: x2, Y: y2, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: x2, Y: y2, Button: tea.MouseLeft}))
}

func TestPressOnABorderDragsItsRatio(t *testing.T) {
	tests := []struct {
		name     string
		setup    []string
		press    [2]int // the press, on the line
		move     [2]int // where the pointer goes before the release
		ratio    int    // the ratio of the root split after the drag
		line     rect   // the line after the drag
		vertical bool
	}{
		{
			name:  "a vertical line follows the pointer to the left",
			setup: []string{"split_vertical"},
			press: [2]int{40, 5}, move: [2]int{20, 5},
			ratio: 25, line: rect{X: 20, Y: 2, W: 1, H: 21}, vertical: true,
		},
		{
			name:  "a vertical line follows the pointer to the right",
			setup: []string{"split_vertical"},
			press: [2]int{40, 5}, move: [2]int{60, 5},
			ratio: 76, line: rect{X: 60, Y: 2, W: 1, H: 21}, vertical: true,
		},
		{
			name:  "a line dragged past the right edge stops at 90 percent",
			setup: []string{"split_vertical"},
			press: [2]int{40, 5}, move: [2]int{79, 5},
			ratio: 90, line: rect{X: 71, Y: 2, W: 1, H: 21}, vertical: true,
		},
		{
			name:  "a line dragged past the left edge stops at 10 percent",
			setup: []string{"split_vertical"},
			press: [2]int{40, 5}, move: [2]int{0, 5},
			ratio: 10, line: rect{X: 8, Y: 2, W: 1, H: 21}, vertical: true,
		},
		{
			name:  "a stacked line follows the pointer up",
			setup: []string{"split_horizontal"},
			press: [2]int{10, 12}, move: [2]int{10, 7},
			ratio: 25, line: rect{X: 0, Y: 7, W: 80, H: 1}, vertical: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
			for _, a := range tt.setup {
				runAction(t, m, a)
			}
			focus := m.layout().Focus
			m.Update(click(tt.press[0], tt.press[1], tea.MouseLeft))
			if m.layout().Focus != focus {
				t.Errorf("the press on the line moved the focus to %d", m.layout().Focus)
			}
			m.Update(tea.MouseMotionMsg(tea.Mouse{X: tt.move[0], Y: tt.move[1], Button: tea.MouseLeft}))
			if got := m.splits.Root.Share(); got != tt.ratio {
				t.Errorf("ratio = %d, want %d", got, tt.ratio)
			}
			l := m.layout()
			if len(l.Borders) != 1 || l.Borders[0].Rect != tt.line || l.Borders[0].Vertical != tt.vertical {
				t.Fatalf("lines = %+v, want one at %v", l.Borders, tt.line)
			}
			if got := l.Hit(tt.line.X, tt.line.Y); got.Kind != hitSplitBorder {
				t.Errorf("hit on the moved line = %+v, want a split border", got)
			}
			m.Update(tea.MouseReleaseMsg(tea.Mouse{X: tt.move[0], Y: tt.move[1], Button: tea.MouseLeft}))
		})
	}
}

func TestDragEndsOnRelease(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	pressDrag(m, 40, 5, 20, 5)
	if got := m.splits.Root.Share(); got != 25 {
		t.Fatalf("ratio after the drag = %d, want 25", got)
	}
	// The release ended the drag, so a motion with no button does not move the line.
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 60, Y: 5}))
	if got := m.splits.Root.Share(); got != 25 {
		t.Errorf("ratio after a motion past the release = %d, want 25", got)
	}
}

func TestDragOfAnUnrelatedPressDoesNotResize(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	// A press in a pane is a caret press, so the motion selects text and leaves the ratio.
	m.Update(click(10, 5, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 60, Y: 5, Button: tea.MouseLeft}))
	if got := m.splits.Root.Share(); got != 50 {
		t.Errorf("ratio = %d after a text drag, want 50", got)
	}
}
