package tui

import (
	"testing"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/frame"
)

func TestHitTabsCloseAndNew(t *testing.T) {
	tabs := []TabInfo{{Name: "a.txt"}, {Name: "b.txt", Active: true}}
	l := layoutFor(100, 30, frame.Input{Tabs: tabs})
	var kinds []frame.HitKind
	for _, r := range l.Strip.Runs {
		kinds = append(kinds, r.Kind)
		if got := l.Hit(r.X, l.StripY); got.Kind != r.Kind || got.Index != r.Index {
			t.Errorf("run %+v hit as %+v", r, got)
		}
	}
	want := []frame.HitKind{frame.HitTab, frame.HitTabClose, frame.HitTab, frame.HitTab, frame.HitTabClose, frame.HitTab, frame.HitTabNew}
	if len(kinds) != len(want) {
		t.Fatalf("tab runs = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("tab runs = %v, want %v", kinds, want)
		}
	}
}

func TestHitExplorerTitleButtons(t *testing.T) {
	in := frame.Input{ExplorerOpen: true, ExplorerWidth: explorer.ColumnsWidth(30)}
	l := layoutFor(100, 30, in)
	if got := l.Hit(l.ExReloadX, l.ExTitleY); got.Kind != frame.HitExplorerTitleReload {
		t.Errorf("reload button = %+v", got)
	}
	if got := l.Hit(l.ExCloseX, l.ExTitleY); got.Kind != frame.HitExplorerTitleClose {
		t.Errorf("close button = %+v", got)
	}
	if got := l.Hit(2, l.ExTitleY); got.Kind != frame.HitExplorerBody {
		t.Errorf("title body = %+v", got)
	}
}

func TestHitExplorerRowsAndGrip(t *testing.T) {
	in := frame.Input{ExplorerOpen: true, ExplorerRows: 3}
	l := layoutFor(100, 30, in)
	if got := l.Hit(3, l.BodyTop+1); got.Kind != frame.HitExplorerRow || got.Index != 1 {
		t.Errorf("second explorer row = %+v", got)
	}
	if got := l.Hit(3, l.BodyTop+5); got.Kind != frame.HitExplorerBody {
		t.Errorf("below the rows = %+v", got)
	}
	if got := l.Hit(l.Ex-1, l.BodyTop); got.Kind != frame.HitExplorerGrip {
		t.Errorf("right border = %+v", got)
	}
	if got := l.Hit(0, l.BodyTop); got.Kind != frame.HitExplorerBody {
		t.Errorf("left border = %+v", got)
	}
}

func TestHitEditorGutterTextAndTrack(t *testing.T) {
	in := frame.Input{ExplorerOpen: true, Lines: 100}
	l := layoutFor(80, 15, in)
	if !l.Scrollbar {
		t.Fatal("100 lines in 10 rows should show the scrollbar")
	}
	if got := l.Hit(l.EditorX, l.BodyTop+2); got.Kind != frame.HitGutter || got.Index != 2 {
		t.Errorf("gutter = %+v", got)
	}
	if got := l.Hit(l.TextX, l.BodyTop+2); got.Kind != frame.HitEditorText || got.Index != 2 {
		t.Errorf("text = %+v", got)
	}
	if got := l.Hit(l.W-2, l.BodyTop); got.Kind != frame.HitScrollbarTrack {
		t.Errorf("scrollbar column = %+v", got)
	}
	if got := l.Hit(l.W-1, l.BodyTop); got.Kind != frame.HitNone {
		t.Errorf("frame border = %+v, want no target", got)
	}
}

func TestHitNothingOutsideTheFrame(t *testing.T) {
	l := layoutFor(80, 24, frame.Input{})
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {80, 0}, {0, 24}} {
		if got := l.Hit(p[0], p[1]); got.Kind != frame.HitNone {
			t.Errorf("hit(%d, %d) = %+v", p[0], p[1], got)
		}
	}
}

func TestHitStatusRowIsNotTheBody(t *testing.T) {
	l := layoutFor(80, 24, frame.Input{})
	// The message of the left status group has no target, so a press there is not an
	// editor press. The caret position before it is the Go to Line target.
	if got := l.Hit(40, l.StatusY); got.Kind != frame.HitNone {
		t.Errorf("status row press = %+v", got)
	}
	if got := l.Hit(10, l.StatusY); got.Kind != frame.HitStatusSegment || got.Index != frame.StatusSegmentPosition {
		t.Errorf("caret position press = %+v", got)
	}
	if got := l.Hit(10, l.BodyBottom); got.Kind != frame.HitEditorText {
		t.Errorf("last body row = %+v", got)
	}
	if got := l.Hit(10, l.StatusY-1); got.Kind != frame.HitNone {
		t.Errorf("bottom border = %+v, want no target", got)
	}
}

func TestClickCounterCyclesInTheWindow(t *testing.T) {
	var c clickCounter
	base := testClock()
	steps := []struct {
		at   int // milliseconds after base
		x, y int
		want int
	}{
		{0, 5, 5, 1},
		{100, 5, 5, 2},
		{200, 5, 5, 3},
		{300, 5, 5, 1},  // the count cycles back to one
		{900, 5, 5, 1},  // outside the 500 ms window
		{1000, 6, 5, 1}, // another cell restarts the count
		{1100, 6, 5, 2}, // same cell, inside the window
		{1700, 6, 5, 1}, // the window is measured from the last press
		{1800, 6, 5, 2},
		{1900, 6, 5, 3},
	}
	for _, s := range steps {
		if got := c.press(base.Add(ms(s.at)), s.x, s.y); got != s.want {
			t.Errorf("press at %dms (%d,%d) = %d, want %d", s.at, s.x, s.y, got, s.want)
		}
	}
}
