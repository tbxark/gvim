package dock

import (
	"slices"
	"testing"
)

// TestPanelForwardsTerminalFocus checks the dock only reports real focus
// transitions to a terminal that supports DECSET 1004 reporting, and that hiding
// the dock blurs the child.
func TestPanelForwardsTerminalFocus(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	p := newTestPanel(t, &fakeStarter{term: term})
	// The start reports the dock's current (focused) state to the new session.
	if got := term.focusCalls; len(got) == 0 || !got[len(got)-1] {
		t.Fatalf("focus reports after start = %v, want a final focus-in", got)
	}
	before := len(term.focusCalls)
	p.SetFocus(false)
	if got := term.focusCalls; len(got) != before+1 || got[len(got)-1] {
		t.Fatalf("focus reports after blur = %v", got)
	}
	// A repeated SetFocus is not a transition and reports nothing.
	before = len(term.focusCalls)
	p.SetFocus(false)
	if len(term.focusCalls) != before {
		t.Fatalf("a repeated blur reported again: %v", term.focusCalls)
	}
	p.SetFocus(true)
	if got := term.focusCalls; len(got) != before+1 || !got[len(got)-1] {
		t.Fatalf("focus reports after focus = %v", got)
	}
	before = len(term.focusCalls)
	p.focusDock()
	if len(term.focusCalls) != before {
		t.Fatalf("focusing an already focused panel reported again: %v", term.focusCalls)
	}
	// Hiding the dock takes the keyboard away from the shell too.
	p.Hide()
	if got := term.focusCalls; len(got) != before+1 || got[len(got)-1] {
		t.Fatalf("focus reports after hide = %v", got)
	}
	// Showing it again focuses the running shell.
	p.Show()
	if got := term.focusCalls; len(got) == 0 || !got[len(got)-1] {
		t.Fatalf("focus reports after show = %v, want focus-in", got)
	}
}

// TestFocusEventsFollowTheActiveTab checks a tab switch moves the terminal focus in
// the expected direction (the tabs share one fake session, so the sequence is what
// is asserted).
func TestFocusEventsFollowTheActiveTab(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	starter := &fakeStarter{term: term}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: starter})
	if cmd := tabs.NewTab(); cmd == nil {
		t.Fatal("NewTab returned no command")
	} else if _, ok := tabs.Update(cmd()); !ok {
		t.Fatal("the first tab did not start")
	}
	if cmd := tabs.NewTab(); cmd == nil {
		t.Fatal("the second NewTab returned no command")
	} else if _, ok := tabs.Update(cmd()); !ok {
		t.Fatal("the second tab did not start")
	}
	tabs.SetFocus(true)
	term.focusCalls = nil
	if !tabs.SetActive(0) {
		t.Fatal("SetActive(0) failed")
	}
	// The old tab blurs; the newly active tab gets the focus when the dock had it.
	if got := term.focusCalls; !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("focus reports after the tab switch = %v, want blur then focus", got)
	}
}

// TestRestartReportsFocusToTheNewSession checks a restart of a focused dock gives
// the replacement session the current focus state (the shell may enable 1004
// later and must then learn that it is focused).
func TestRestartReportsFocusToTheNewSession(t *testing.T) {
	t.Parallel()
	old := newFakeTerm()
	starter := &fakeStarter{term: old}
	p := newTestPanel(t, starter)
	if _, ok := p.Update(exitMsg{gen: p.gen}); !ok {
		t.Fatal("the exit message was not handled")
	}
	starter.term = newFakeTerm()
	cmd := p.Restart()
	if cmd == nil {
		t.Fatal("Restart returned no command")
	}
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if !p.Running() {
		t.Fatal("the shell did not restart")
	}
	if got := starter.term.focusCalls; len(got) == 0 || !got[len(got)-1] {
		t.Fatalf("focus reports to the restarted session = %v, want focus-in", got)
	}
}
