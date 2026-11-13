package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
)

// tabRun returns the run of the name of tab i.
func tabRun(t *testing.T, m *Model, i int) frame.Run {
	t.Helper()
	for _, r := range m.layout().Strip.Runs {
		if r.Kind == frame.HitTab && r.Index == i {
			return r
		}
	}
	t.Fatalf("no run for tab %d", i)
	return frame.Run{}
}

// pastMidpoint is a column inside tab to, on the side away from tab from. A drag
// swaps tabs only once the pointer is that far over the neighbour.
func pastMidpoint(t *testing.T, m *Model, from, to int) int {
	t.Helper()
	r := tabRun(t, m, to)
	if to > from {
		return r.X + r.W
	}
	return r.X
}

func TestDraggingATabReordersTheStrip(t *testing.T) {
	tests := []struct {
		name     string
		from, to int
		want     []string
	}{
		{"onto the next tab", 0, 1, []string{"b.txt", "a.txt", "c.txt"}},
		{"to the end", 0, 2, []string{"b.txt", "c.txt", "a.txt"}},
		{"back to the start", 2, 0, []string{"c.txt", "a.txt", "b.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ed := newCtxModel(t, "a.txt", "b.txt", "c.txt")
			y := m.layout().StripY
			src := tabRun(t, m, tt.from)
			mouseDownAt(m, src.X+2, y)
			x := pastMidpoint(t, m, tt.from, tt.to)
			mouseMove(m, x, y)
			mouseUpAt(m, x, y)
			if got := tabNames(ed); !slices.Equal(got, tt.want) {
				t.Fatalf("tabs = %v, want %v", got, tt.want)
			}
			if active := ed.tabs[slices.IndexFunc(ed.tabs, func(t TabInfo) bool { return t.Active })].Name; active != ed.tabs[tt.to].Name {
				t.Errorf("the active tab is %q, want the dragged tab %q", active, ed.tabs[tt.to].Name)
			}
		})
	}
}

func TestDraggingATabPastTheEndsStopsAtTheEnds(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "b.txt", "c.txt")
	y := m.layout().StripY
	src := tabRun(t, m, 1)
	mouseDownAt(m, src.X+2, y)
	mouseMove(m, 95, y) // far right of the last tab
	if got := tabNames(ed); !slices.Equal(got, []string{"a.txt", "c.txt", "b.txt"}) {
		t.Fatalf("tabs = %v after dragging past the last tab", got)
	}
	mouseMove(m, 0, y)
	if got := tabNames(ed); !slices.Equal(got, []string{"b.txt", "a.txt", "c.txt"}) {
		t.Fatalf("tabs = %v after dragging to the left edge", got)
	}
	mouseUpAt(m, 0, y)
}

func TestTabDragEndsOnReleaseAndAClickDoesNotMoveATab(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "b.txt", "c.txt")
	y := m.layout().StripY
	src, dst := tabRun(t, m, 0), tabRun(t, m, 2)
	// A plain click activates the tab and moves nothing.
	mouseDownAt(m, src.X+2, y)
	mouseUpAt(m, src.X+2, y)
	if got := tabNames(ed); !slices.Equal(got, []string{"a.txt", "b.txt", "c.txt"}) {
		t.Fatalf("a click moved tabs: %v", got)
	}
	// Motion after the release does not drag.
	mouseMove(m, dst.X+2, y)
	if got := tabNames(ed); !slices.Equal(got, []string{"a.txt", "b.txt", "c.txt"}) {
		t.Fatalf("a move after the release reordered the tabs: %v", got)
	}
}

func TestTabDragLeavingTheStripRowDoesNotReorder(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "b.txt", "c.txt")
	l := m.layout()
	src, dst := tabRun(t, m, 0), tabRun(t, m, 2)
	mouseDownAt(m, src.X+2, l.StripY)
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: dst.X + 2, Y: l.BodyTop + 3, Button: tea.MouseLeft}))
	if got := tabNames(ed); !slices.Equal(got, []string{"a.txt", "b.txt", "c.txt"}) {
		t.Fatalf("a drag over the editor reordered the tabs: %v", got)
	}
}

func TestDraggingACloseButtonDoesNotMoveTheTab(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "b.txt", "c.txt")
	y := m.layout().StripY
	var closeRun frame.Run
	for _, r := range m.layout().Strip.Runs {
		if r.Kind == frame.HitTabClose && r.Index == 0 {
			closeRun = r
		}
	}
	mouseDownAt(m, closeRun.X, y)
	mouseMove(m, tabRun(t, m, 1).X+2, y)
	if got := tabNames(ed); len(got) != 2 {
		// The press on × closed tab 0, and nothing else may move.
		t.Fatalf("tabs = %v, want a.txt closed", got)
	}
	if got := tabNames(ed); !slices.Equal(got, []string{"b.txt", "c.txt"}) {
		t.Fatalf("tabs = %v after a press on × and a drag", got)
	}
}

func TestTabDragWithoutEditorSupportSaysSo(t *testing.T) {
	ed := &tabsEditor{fakeEditor: newFakeEditor("x")}
	ed.tabs = []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
	m := newRevertModel(t, ed)
	y := m.layout().StripY
	src := tabRun(t, m, 0)
	mouseDownAt(m, src.X+2, y)
	mouseMove(m, pastMidpoint(t, m, 0, 1), y)
	if ed.tabs[0].Name != "a.txt" {
		t.Fatal("tabs moved with no support")
	}
	if m.notice == "" {
		t.Error("the status row does not say moving tabs is not supported")
	}
}

// TestDraggingOverAWideTabSwapsOnlyOnce sweeps the pointer across a wide neighbour.
// The tab swaps once the pointer passes the neighbour's midpoint and stays there
// while the pointer is still over the neighbour, instead of flipping on every cell.
func TestDraggingOverAWideTabSwapsOnlyOnce(t *testing.T) {
	m, ed := newCtxModel(t, "a_very_long_file_name.txt", "b.txt")
	y := m.layout().StripY
	a, b := tabRun(t, m, 0), tabRun(t, m, 1)
	mouseDownAt(m, b.X+2, y)
	swaps, last := 0, tabNames(ed)
	for x := a.X + a.W - 1; x >= a.X+2; x-- {
		mouseMove(m, x, y)
		if got := tabNames(ed); !slices.Equal(got, last) {
			swaps++
			last = got
		}
	}
	if swaps > 1 {
		t.Errorf("the tabs swapped %d times during one sweep, want at most 1", swaps)
	}
	if want := []string{"b.txt", "a_very_long_file_name.txt"}; !slices.Equal(last, want) {
		t.Errorf("tabs = %v, want %v", last, want)
	}
}
