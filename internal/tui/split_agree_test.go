package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/frame"
)

// checkFrame checks that the layout and the hit map of m agree in the current frame.
// Every cell of the body belongs to exactly one pane or one line between panes. A cell
// of a pane hits that pane, and a cell of a line hits the line. The editor's viewport
// has the size of the focused pane.
func checkFrame(t *testing.T, m *Model, f *multiFake, label string) {
	t.Helper()
	l := m.layout()
	for y := l.BodyTop; y <= l.BodyBottom; y++ {
		for x := l.EditorX; x < l.EditorX+l.EditorW; x++ {
			var owners []int // the panes that contain the cell
			for i, p := range l.Panes {
				if p.Contains(x, y) {
					owners = append(owners, i)
				}
			}
			var lines []int // the lines that contain the cell
			for j, b := range l.Borders {
				if b.Contains(x, y) {
					lines = append(lines, j)
				}
			}
			if len(owners)+len(lines) != 1 {
				t.Fatalf("%s: cell (%d, %d) is in %d panes and %d lines, want exactly one", label, x, y, len(owners), len(lines))
			}
			got := l.Hit(x, y)
			if len(lines) == 1 {
				if got.Kind != frame.HitSplitBorder || got.Index != lines[0] {
					t.Fatalf("%s: hit(%d, %d) = %+v on line %d, want a split border", label, x, y, got, lines[0])
				}
				continue
			}
			if i := owners[0]; got.Pane != i || got.Kind == frame.HitSplitBorder || got.Kind == frame.HitNone {
				t.Fatalf("%s: hit(%d, %d) = %+v in pane %d, want that pane", label, x, y, got, i)
			}
		}
	}
	fp := l.FocusedPane()
	if f.cols != fp.TextW || f.rows != m.focusedBodyRows() {
		t.Errorf("%s: viewport %d rows by %d cols, want the focused pane's %d by %d",
			label, f.rows, f.cols, m.focusedBodyRows(), fp.TextW)
	}
}

func TestLayoutAndHitMapAgreeAfterEachChange(t *testing.T) {
	f := newMultiFake(3, 30)
	m := newSplitModel(t, f, 80, 24)
	// dragFirstLine moves the first line of the layout by a few cells, as a drag does.
	dragFirstLine := func(m *Model) {
		b := m.layout().Borders[0]
		if b.Vertical {
			pressDrag(m, b.X, b.Y, b.X-7, b.Y)
			return
		}
		pressDrag(m, b.X, b.Y, b.X, b.Y-3)
	}
	steps := []struct {
		name string
		do   func(m *Model)
	}{
		{"split_vertical", func(m *Model) { runAction(t, m, "split_vertical") }},
		{"increase_split_size", func(m *Model) { runAction(t, m, "increase_split_size") }},
		{"decrease_split_size", func(m *Model) { runAction(t, m, "decrease_split_size") }},
		{"decrease_split_size", func(m *Model) { runAction(t, m, "decrease_split_size") }},
		{"toggle_maximize_split", func(m *Model) { runAction(t, m, "toggle_maximize_split") }},
		{"toggle_maximize_split", func(m *Model) { runAction(t, m, "toggle_maximize_split") }},
		{"split_horizontal", func(m *Model) { runAction(t, m, "split_horizontal") }},
		{"increase_split_size", func(m *Model) { runAction(t, m, "increase_split_size") }},
		{"drag the first line", dragFirstLine},
		{"next_pane", func(m *Model) { runAction(t, m, "next_pane") }},
		{"toggle_maximize_split", func(m *Model) { runAction(t, m, "toggle_maximize_split") }},
		{"prev_window", func(m *Model) { runAction(t, m, "prev_window") }},
		{"toggle_maximize_split", func(m *Model) { runAction(t, m, "toggle_maximize_split") }},
		{"close_split", func(m *Model) { runAction(t, m, "close_split") }},
		{"increase_split_size with one pane", func(m *Model) { runAction(t, m, "increase_split_size") }},
	}
	checkFrame(t, m, f, "start")
	for _, s := range steps {
		s.do(m)
		checkFrame(t, m, f, s.name)
	}
}
