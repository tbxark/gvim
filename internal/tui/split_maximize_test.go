package tui

import (
	"slices"
	"testing"
)

// maximizeKeys binds the chord that runs toggle_maximize_split.
var maximizeKeys = fakeKeymap{
	"normal|t|ctrl": {Action: "toggle_maximize_split"},
}

func TestToggleMaximizeShowsOnlyTheFocusedPane(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus

	runAction(t, m, "toggle_maximize_split")
	l := m.layout()
	if len(l.Panes) != 2 || l.Focus != 1 {
		t.Fatalf("%d panes with focus %d, want 2 with focus 1", len(l.Panes), l.Focus)
	}
	if l.Panes[0].Rect != (rect{}) {
		t.Errorf("the unfocused pane has area %v, want none", l.Panes[0].Rect)
	}
	if want := (rect{X: 0, Y: 2, W: 80, H: 21}); l.Panes[1].Rect != want {
		t.Errorf("the focused pane is %v, want the whole band %v", l.Panes[1].Rect, want)
	}
	if len(l.Borders) != 0 {
		t.Errorf("%d lines between panes while maximized, want none", len(l.Borders))
	}

	runAction(t, m, "toggle_maximize_split")
	want := []rect{{X: 0, Y: 2, W: 40, H: 21}, {X: 41, Y: 2, W: 39, H: 21}}
	if got := paneRects(m.layout()); !slices.Equal(got, want) {
		t.Errorf("after the second toggle: panes %v, want %v", got, want)
	}
	if n := len(m.layout().Borders); n != 1 {
		t.Errorf("%d lines after the second toggle, want 1", n)
	}
}

func TestMaximizedPaneFollowsTheFocus(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	runAction(t, m, "toggle_maximize_split")
	runAction(t, m, "next_split") // the left pane takes the focus, and is the one shown

	l := m.layout()
	if l.Focus != 0 || l.Panes[0].Rect != (rect{X: 0, Y: 2, W: 80, H: 21}) || l.Panes[1].Rect != (rect{}) {
		t.Errorf("after next_split: focus %d, panes %v, want the left pane alone", l.Focus, paneRects(l))
	}
}

func TestSplitWhileMaximizedRestoresEveryPane(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	runAction(t, m, "toggle_maximize_split")
	runAction(t, m, "split_horizontal")

	l := m.layout()
	if len(l.Panes) != 3 || len(l.Borders) != 2 {
		t.Fatalf("%d panes and %d lines, want 3 and 2", len(l.Panes), len(l.Borders))
	}
	for i, p := range l.Panes {
		if p.W <= 0 || p.H <= 0 {
			t.Errorf("pane %d has no area after the split: %v", i, p.Rect)
		}
	}
}

func TestMaximizeWithOnePaneSaysSo(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "toggle_maximize_split")
	if m.notice != "Only one split is open" {
		t.Errorf("notice = %q, want the one split notice", m.notice)
	}
	if got := paneRects(m.layout()); !slices.Equal(got, []rect{{X: 0, Y: 2, W: 80, H: 21}}) {
		t.Errorf("panes = %v, want the whole band", got)
	}
}

func TestMaximizedHitMapFollowsThePane(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	runAction(t, m, "toggle_maximize_split") // the right pane fills the band

	// Where the line was, and the cells of the hidden left pane, now belong to the
	// focused pane.
	for _, x := range []int{10, 40, 60} {
		want := hitTarget{Kind: hitEditorText, Index: 3, Pane: 1}
		if got := m.layout().Hit(x, 5); got != want {
			t.Errorf("hit(%d, 5) = %+v while maximized, want %+v", x, got, want)
		}
	}
}

func TestMaximizeKeyReachesTheSplit(t *testing.T) {
	m := newSplitModelKeys(t, newMultiFake(2, 30), 80, 24, maximizeKeys)
	runAction(t, m, "split_vertical")
	press(m, ctrl('t'))
	if n := len(m.layout().Borders); n != 0 {
		t.Errorf("%d lines after ctrl+t, want none", n)
	}
	press(m, ctrl('t'))
	if n := len(m.layout().Borders); n != 1 {
		t.Errorf("%d lines after the second ctrl+t, want 1", n)
	}
}

func TestMaximizeInACompareViewSaysSo(t *testing.T) {
	// A compare view keeps both sides, so a press shows a notice and changes nothing.
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	runAction(t, m, "toggle_maximize_split")
	if m.notice != "Maximize does not apply to a compare view" {
		t.Errorf("notice = %q, want the compare view notice", m.notice)
	}
	if m.splits.maximized {
		t.Error("the compare view was maximized")
	}
	if got := paneRects(m.layout()); len(got) != 2 || len(m.layout().Borders) != 1 {
		t.Errorf("panes = %v with %d lines, want both sides and one line", got, len(m.layout().Borders))
	}
}
