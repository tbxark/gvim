package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/diff"
	"github.com/tbxark/gvim/internal/theme"
)

// compareLeft and compareRight differ in three hunks, with zero-based lines. b is
// removed: left line 1 is right line 1. X is added at right line 3, which comes
// after left line 3, so it starts at left line 4. f is added at the end of the
// right, after left line 4, which is the last line of the left, so its left start
// is clamped to line 4.
var (
	compareLeft  = []string{"a", "b", "c", "d", "e"}
	compareRight = []string{"a", "c", "d", "X", "e", "f"}
)

// newCompareModel returns a model with the two tabs in a compare view, the left pane
// focused.
func newCompareModel(t *testing.T, left, right []string) *Model {
	t.Helper()
	m := newSplitModel(t, newDocsFake(left, right), 80, 24)
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	return m
}

func TestCompareStepWrapsAroundOnTheFocusedSide(t *testing.T) {
	// The left starts are 1, 4 and 5, and 5 is clamped to 4. The right starts are 1,
	// 3 and 5. The other side scrolls to its own start of the same difference.
	tests := []struct {
		name       string
		focusRight bool
		from       int  // the cursor line on the focused side before the step
		forward    bool // next_difference when set, prev_difference otherwise
		want       int  // the cursor line on the focused side after the step
		otherTop   int  // the top of the other pane after the step
	}{
		{"left, next from the top", false, 0, true, 1, 1},
		{"left, next from the first difference", false, 1, true, 4, 3},
		{"left, next from the last difference wraps", false, 4, true, 1, 1},
		{"left, prev from the top wraps", false, 0, false, 4, 5},
		{"left, prev from the last difference", false, 4, false, 1, 1},
		{"left, prev from the first difference wraps", false, 1, false, 4, 5},
		{"right, next from the top", true, 0, true, 1, 1},
		{"right, next from the first difference", true, 1, true, 3, 4},
		{"right, next from the second difference", true, 3, true, 5, 4},
		{"right, next from the last difference wraps", true, 5, true, 1, 1},
		{"right, prev from the top wraps", true, 0, false, 5, 4},
		{"right, prev from the second difference", true, 3, false, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newCompareModel(t, compareLeft, compareRight)
			if tt.focusRight {
				runAction(t, m, "next_split")
			}
			m.ed.SetCursor(buffer.Pos{Line: tt.from}, false)
			action := "compare_prev_difference"
			if tt.forward {
				action = "compare_next_difference"
			}
			runAction(t, m, action)

			ps := panesOf(t, m, 2)
			other := 1
			if tt.focusRight {
				other = 0
			}
			if got := m.ed.Active().Cursor().Line; got != tt.want {
				t.Errorf("focused cursor line = %d, want %d", got, tt.want)
			}
			if got := ps[other].top; got != tt.otherTop {
				t.Errorf("other pane top = %d, want %d", got, tt.otherTop)
			}
		})
	}
}

func TestCompareHunksListsTheDifferences(t *testing.T) {
	// The hunk list is what a renderer paints, so it is read here and not from the
	// frame. Each hunk gives its start on the left and on the right.
	m := newSplitModel(t, newDocsFake(compareLeft, compareRight), 80, 24)
	if _, ok := m.compareHunks(); ok {
		t.Fatal("compareHunks with no compare view reports ok")
	}
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	hunks, ok := m.compareHunks()
	if !ok {
		t.Fatal("compareHunks = not ok")
	}
	var starts [][2]int
	for _, h := range hunks {
		starts = append(starts, [2]int{h.OldStart, h.NewStart})
	}
	if want := [][2]int{{1, 1}, {4, 3}, {5, 5}}; !slices.Equal(starts, want) {
		t.Errorf("hunk starts (left, right) = %v, want %v", starts, want)
	}
	// Focus the right pane: the list is the same, because it does not depend on focus.
	runAction(t, m, "next_split")
	if again, _ := m.compareHunks(); !slices.Equal(again, hunks) {
		t.Errorf("hunks changed with the focus: %v, want %v", again, hunks)
	}
}

func TestCompareReportsNoDifferencesAndKeepsTheCursor(t *testing.T) {
	m := newCompareModel(t, []string{"a", "b"}, []string{"a", "b"})
	runAction(t, m, "compare_next_difference")
	if m.notice != "No differences" {
		t.Errorf("notice = %q, want %q", m.notice, "No differences")
	}
	if got := m.ed.Active().Cursor().Line; got != 0 {
		t.Errorf("cursor line = %d, want 0", got)
	}
}

func TestCompareNeedsACompareView(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 3), 80, 24)
	for _, action := range []string{"compare_next_difference", "compare_prev_difference", "compare_switch_side"} {
		runAction(t, m, action)
		if m.notice != "No compare view is open" {
			t.Errorf("%s: notice = %q, want %q", action, m.notice, "No compare view is open")
		}
	}
}

func TestCompareWithoutTheOtherDocumentHasNoDifferences(t *testing.T) {
	// fakeEditor has no DocAt, so the right tab has no document to compare.
	f := newFakeEditor("a", "b")
	f.tabs = []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	runAction(t, m, "compare_next_difference")
	if m.notice == "" {
		t.Error("no notice for a compare whose other document is missing")
	}
	if got := f.doc.cursor.Line; got != 0 {
		t.Errorf("cursor line = %d, want 0", got)
	}
}

func TestCompareSwitchSideSwapsThePanesAndKeepsTheFocus(t *testing.T) {
	m := newCompareModel(t, compareLeft, compareRight)
	focused := m.splits.focus
	runAction(t, m, "compare_switch_side")
	ps := panesOf(t, m, 2)
	if ps[0].tab != 1 || ps[1].tab != 0 {
		t.Errorf("tabs after the switch = %d and %d, want 1 and 0", ps[0].tab, ps[1].tab)
	}
	if m.splits.focus != focused || m.layout().focus != 1 || m.activeTab() != 0 {
		t.Errorf("focus %d on the same node %v, active tab %d; want the focus on pane 1 and tab 0",
			m.layout().focus, m.splits.focus == focused, m.activeTab())
	}
}

func TestOpenCompareShowsTwoTabsSideBySide(t *testing.T) {
	m := newSplitModel(t, newDocsFake([]string{"a"}, []string{"b"}), 80, 24)
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	l := m.layout()
	if len(l.panes) != 2 || l.panes[0].tab != 0 || l.panes[1].tab != 1 || l.focus != 0 {
		t.Fatalf("panes %+v focus %d, want tabs 0 and 1 with the focus on pane 0", l.panes, l.focus)
	}
	if l.panes[0].y != l.panes[1].y || l.panes[1].x <= l.panes[0].x {
		t.Errorf("panes %+v are not side by side", l.panes)
	}
	if m.openCompare(0, 2) {
		t.Error("openCompare with a missing tab = true")
	}
	if n := len(m.layout().panes); n != 2 {
		t.Errorf("%d panes after a refused compare, want 2", n)
	}
}

func TestSplitLeavesTheCompareView(t *testing.T) {
	m := newCompareModel(t, compareLeft, compareRight)
	runAction(t, m, "split_horizontal")
	runAction(t, m, "compare_next_difference")
	if m.notice != "No compare view is open" {
		t.Errorf("notice = %q after a split, want the compare view closed", m.notice)
	}
}

func TestPickHunkWrapsAtBothEnds(t *testing.T) {
	hunks := []diff.Hunk{
		{OldStart: 1, NewStart: 1},
		{OldStart: 4, NewStart: 3},
		{OldStart: 5, NewStart: 5},
	}
	tests := []struct {
		name    string
		side    int
		line    int
		last    int // the last line index of the focused document
		forward bool
		want    int
	}{
		{"left, forward from the top", 0, 0, 4, true, 0},
		{"left, forward to the second", 0, 1, 4, true, 1},
		// The third start, 5, is clamped to 4, which is not after the cursor on 4.
		{"left, forward past the clamped end wraps", 0, 4, 4, true, 0},
		{"left, backward from the top wraps", 0, 0, 4, false, 2},
		{"left, backward skips the hunk at the cursor", 0, 4, 4, false, 0},
		{"right, forward from the second start", 1, 3, 5, true, 2},
		{"right, backward from the top wraps", 1, 0, 5, false, 2},
		{"right, backward from the second start", 1, 3, 5, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickHunk(hunks, tt.side, tt.line, tt.last, tt.forward); got != tt.want {
				t.Errorf("pickHunk = %d, want %d", got, tt.want)
			}
		})
	}
	if got := pickHunk(nil, 0, 0, 0, true); got != -1 {
		t.Errorf("pickHunk with no hunks = %d, want -1", got)
	}
}
