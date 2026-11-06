package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// windowKeys binds the chords that run the pane, window and previous-tab actions.
var windowKeys = fakeKeymap{
	"normal|j|ctrl": {Action: "next_window"},
	"normal|h|ctrl": {Action: "prev_window"},
	"normal|g|ctrl": {Action: "switch_to_previous_tab"},
	"normal|p|ctrl": {Action: "next_pane"},
}

// focusStep is one action of a walk and the focus it should leave.
type focusStep struct {
	action string
	focus  int // the layout index of the focused pane
}

func TestNextAndPrevPaneMoveAlongTheLeaves(t *testing.T) {
	m := newSplitModel(t, newMultiFake(3, 30), 80, 24)
	runAction(t, m, "split_vertical")
	runAction(t, m, "split_horizontal") // three leaves; the focus is the bottom of the right column
	walk := []focusStep{
		{"next_pane", 0}, {"next_pane", 1}, {"next_pane", 2}, // wraps at the end
		{"prev_pane", 1}, {"prev_pane", 0}, {"prev_pane", 2}, // wraps at the start
	}
	for _, s := range walk {
		runAction(t, m, s.action)
		if got := m.layout().focus; got != s.focus {
			t.Errorf("after %s: focus %d, want %d", s.action, got, s.focus)
		}
	}
}

func TestNextAndPrevWindowMoveAlongTheLeavesWhenSplit(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	walk := []focusStep{
		{"next_window", 0}, {"next_window", 1}, {"next_window", 0},
		{"prev_window", 1}, {"prev_window", 0},
	}
	for _, s := range walk {
		runAction(t, m, s.action)
		if got := m.layout().focus; got != s.focus {
			t.Errorf("after %s: focus %d, want %d", s.action, got, s.focus)
		}
	}
}

func TestNextAndPrevWindowCycleTabsWithoutASplit(t *testing.T) {
	f := newMultiFake(3, 3)
	m := newSplitModel(t, f, 80, 24)
	walk := []struct {
		action string
		tab    int
	}{
		{"next_window", 1}, {"next_window", 2}, {"next_window", 0}, // wraps at the end
		{"prev_window", 2}, {"prev_window", 1},
	}
	for _, s := range walk {
		runAction(t, m, s.action)
		if f.active != s.tab {
			t.Errorf("after %s: active tab %d, want %d", s.action, f.active, s.tab)
		}
		if n := len(m.layout().panes); n != 1 {
			t.Errorf("after %s: %d panes, want 1", s.action, n)
		}
	}
}

func TestNextWindowWithNoTabsDoesNothing(t *testing.T) {
	m := newSplitModel(t, newMultiFake(0, 0), 80, 24)
	runAction(t, m, "next_window")
	runAction(t, m, "prev_window")
	if n := len(m.layout().panes); n != 1 {
		t.Errorf("%d panes with no tab, want 1", n)
	}
}

func TestSwitchToPreviousTabTogglesBetweenTheLastTwoTabs(t *testing.T) {
	f := newMultiFake(3, 3)
	m := newSplitModel(t, f, 80, 24)
	walk := []struct {
		action string
		tab    int
	}{
		{"next_window", 1},
		{"next_window", 2},
		{"switch_to_previous_tab", 1}, // the last switch was 1 to 2
		{"switch_to_previous_tab", 2}, // and back
	}
	for _, s := range walk {
		runAction(t, m, s.action)
		if f.active != s.tab {
			t.Errorf("after %s: active tab %d, want %d", s.action, f.active, s.tab)
		}
	}
}

func TestSwitchToPreviousTabWithNoSwitchSaysSo(t *testing.T) {
	f := newMultiFake(2, 3)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "switch_to_previous_tab")
	if f.active != 0 {
		t.Errorf("active tab %d, want 0", f.active)
	}
	if m.notice != "No previous tab" {
		t.Errorf("notice = %q, want %q", m.notice, "No previous tab")
	}
}

func TestSwitchToPreviousTabSeesASwitchMadeByAKey(t *testing.T) {
	// Update records each tab change, so a switch made by a key counts as the last
	// switch, not only the actions that run inside this root.
	f := newMultiFake(3, 3)
	m := newSplitModelKeys(t, f, 80, 24, windowKeys)
	press(m, ctrl('j')) // next_window: 0 to 1
	press(m, ctrl('j')) // 1 to 2
	press(m, ctrl('g')) // switch_to_previous_tab
	if f.active != 1 {
		t.Errorf("after ctrl+g: active tab %d, want 1", f.active)
	}
}

func TestSwitchToPreviousTabSeesATabClick(t *testing.T) {
	// A click on a tab switches tabs outside any action, so Update has to record it.
	f := newMultiFake(3, 3)
	m := newSplitModelKeys(t, f, 80, 24, windowKeys)
	l := m.layout()
	var tab2 *run
	for i := range l.strip.runs {
		if r := l.strip.runs[i]; r.kind == hitTab && r.index == 2 {
			tab2 = &r
		}
	}
	if tab2 == nil {
		t.Fatal("no run for tab 2 in the tab strip")
	}
	m.Update(click(tab2.x, l.stripY, tea.MouseLeft))
	if f.active != 2 {
		t.Fatalf("after the tab click: active tab %d, want 2", f.active)
	}
	press(m, ctrl('g'))
	if f.active != 0 {
		t.Errorf("after ctrl+g: active tab %d, want 0, the tab the click left", f.active)
	}
}

func TestSwitchToPreviousTabForgetsAClosedTab(t *testing.T) {
	f := newMultiFake(3, 3)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "next_window") // 0 to 1, so tab 0 is the previous one
	m.requestClose(0)              // closes the previous tab
	runAction(t, m, "switch_to_previous_tab")
	if m.notice != "No previous tab" {
		t.Errorf("notice = %q after the previous tab closed, want %q", m.notice, "No previous tab")
	}
	if f.active != 0 {
		t.Errorf("active tab %d, want 0 (the tab that was 1)", f.active)
	}
}

func TestSwitchToPreviousTabFollowsAnEarlierTabThatCloses(t *testing.T) {
	// Tabs 0, 1 and 2. The last switch was 1 to 2. Closing tab 0 moves the previous tab
	// down to index 0, and the switch goes to the same document.
	f := newMultiFake(3, 3)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "next_window")
	runAction(t, m, "next_window")
	m.requestClose(0)
	runAction(t, m, "switch_to_previous_tab")
	if f.active != 0 || f.doc().Line(0) != "t1 0" {
		t.Errorf("active tab %d starting %q, want tab 0 starting %q", f.active, f.doc().Line(0), "t1 0")
	}
}

func TestPaneAndWindowKeysReachTheSplitActions(t *testing.T) {
	f := newMultiFake(3, 3)
	m := newSplitModelKeys(t, f, 80, 24, windowKeys)
	runAction(t, m, "split_vertical")
	press(m, ctrl('p')) // next_pane: the left pane takes the focus
	if got := m.layout().focus; got != 0 {
		t.Errorf("after ctrl+p: focus %d, want 0", got)
	}
	press(m, ctrl('h')) // prev_window: wraps to the right pane
	if got := m.layout().focus; got != 1 {
		t.Errorf("after ctrl+h: focus %d, want 1", got)
	}
}

// TestPaneWindowResizeAndTabActionsAreHandled runs each command the way the palette and
// the menu do, through Run. A command the root does not handle reports the editor's
// unknown action, which the palette shows as "Not supported".
func TestPaneWindowResizeAndTabActionsAreHandled(t *testing.T) {
	actions := []string{
		"next_pane", "prev_pane", "next_window", "prev_window",
		"increase_split_size", "decrease_split_size",
		"switch_to_previous_tab", "toggle_maximize_split",
	}
	for _, a := range actions {
		for _, split := range []bool{false, true} {
			m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
			if split {
				runAction(t, m, "split_vertical")
			}
			if err := m.Run(a); err != nil {
				t.Errorf("Run(%q) with split %v = %v, want the action handled", a, split, err)
			}
		}
	}
}
