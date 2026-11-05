package ui

import "testing"

func TestHitMenuLabels(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{})
	first := l.menuRuns[0]
	if got := l.hit(first.x, 0); got.Kind != hitMenuLabel || got.Index != 0 {
		t.Errorf("press on File = %+v", got)
	}
	// The gap between two labels belongs to no target.
	gap := l.menuRuns[0].x + l.menuRuns[0].w
	if got := l.hit(gap, 0); got.Kind != hitNone {
		t.Errorf("press in the gap = %+v", got)
	}
}

func TestHitTabsCloseAndNew(t *testing.T) {
	tabs := []TabInfo{{Name: "a.txt"}, {Name: "b.txt", Active: true}}
	l := layoutFor(100, 30, layoutInput{tabs: tabs})
	var kinds []hitKind
	for _, r := range l.strip.runs {
		kinds = append(kinds, r.kind)
		if got := l.hit(r.x, 1); got.Kind != r.kind || got.Index != r.index {
			t.Errorf("run %+v hit as %+v", r, got)
		}
	}
	want := []hitKind{hitTab, hitTabClose, hitTab, hitTab, hitTabClose, hitTab, hitTabNew}
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
	in := layoutInput{explorerOpen: true, explorerOverride: 30}
	l := layoutFor(100, 30, in)
	if got := l.hit(l.exReloadX, 1); got.Kind != hitExplorerTitleReload {
		t.Errorf("reload button = %+v", got)
	}
	if got := l.hit(l.exCloseX, 1); got.Kind != hitExplorerTitleClose {
		t.Errorf("close button = %+v", got)
	}
	if got := l.hit(2, 1); got.Kind != hitExplorerBody {
		t.Errorf("title body = %+v", got)
	}
}

func TestHitExplorerRowsAndGrip(t *testing.T) {
	in := layoutInput{explorerOpen: true, explorerRows: 3}
	l := layoutFor(100, 30, in)
	if got := l.hit(3, l.bodyTop+1); got.Kind != hitExplorerRow || got.Index != 1 {
		t.Errorf("second explorer row = %+v", got)
	}
	if got := l.hit(3, l.bodyTop+5); got.Kind != hitExplorerBody {
		t.Errorf("below the rows = %+v", got)
	}
	if got := l.hit(l.ex-1, l.bodyTop); got.Kind != hitExplorerGrip {
		t.Errorf("right border = %+v", got)
	}
	if got := l.hit(0, l.bodyTop); got.Kind != hitExplorerBody {
		t.Errorf("left border = %+v", got)
	}
}

func TestHitEditorGutterTextAndTrack(t *testing.T) {
	in := layoutInput{explorerOpen: true, lines: 100}
	l := layoutFor(80, 15, in)
	if !l.scrollbar {
		t.Fatal("100 lines in 12 rows should show the scrollbar")
	}
	if got := l.hit(l.editorX, l.bodyTop+2); got.Kind != hitGutter || got.Index != 2 {
		t.Errorf("gutter = %+v", got)
	}
	if got := l.hit(l.textX, l.bodyTop+2); got.Kind != hitEditorText || got.Index != 2 {
		t.Errorf("text = %+v", got)
	}
	if got := l.hit(l.w-1, l.bodyTop); got.Kind != hitScrollbarTrack {
		t.Errorf("last column = %+v", got)
	}
}

func TestHitNothingOutsideTheFrame(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{})
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {80, 0}, {0, 24}} {
		if got := l.hit(p[0], p[1]); got.Kind != hitNone {
			t.Errorf("hit(%d, %d) = %+v", p[0], p[1], got)
		}
	}
}

func TestHitStatusRowIsNotTheBody(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{})
	// The left status group has no targets, so a press there is not an editor press.
	if got := l.hit(10, l.statusY); got.Kind != hitNone {
		t.Errorf("status row press = %+v", got)
	}
	if got := l.hit(10, l.statusY-1); got.Kind != hitEditorText {
		t.Errorf("row above status = %+v", got)
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
