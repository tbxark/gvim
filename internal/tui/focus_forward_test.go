package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestViewAlwaysReportsFocus checks the program asks its host terminal for focus
// events in every workspace (not only a remote authority), so the dock can merge
// the real window focus into DECSET 1004 reporting.
func TestViewAlwaysReportsFocus(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	if v := m.View(); !v.ReportFocus {
		t.Fatal("View.ReportFocus is off for a local workspace")
	}
}

// TestWindowFocusMessagesReachTheTerminal checks FocusMsg/BlurMsg are forwarded to
// the dock and stop at the combined dock/window state, without a terminal child
// thinking it is focused while the dock is on a file buffer.
func TestWindowFocusMessagesReachTheTerminal(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	term := starter.term
	last := func() bool {
		if len(term.focusCalls) == 0 {
			t.Fatal("the terminal got no focus reports")
		}
		return term.focusCalls[len(term.focusCalls)-1]
	}
	if !last() {
		t.Fatalf("opened dock focus reports = %v", term.focusCalls)
	}
	_, _ = m.Update(tea.BlurMsg{})
	if last() {
		t.Fatalf("window blur reports = %v", term.focusCalls)
	}
	before := len(term.focusCalls)
	_, _ = m.Update(tea.BlurMsg{})
	if len(term.focusCalls) != before {
		t.Fatalf("repeated window blur reported again: %v", term.focusCalls)
	}
	_, _ = m.Update(tea.FocusMsg{})
	if !last() {
		t.Fatalf("window focus back reports = %v", term.focusCalls)
	}
	// Moving the keyboard to the editor blurs the child; a later window focus
	// change must not focus it again.
	m.dock.SetFocus(false)
	if last() {
		t.Fatalf("editor focus reports = %v", term.focusCalls)
	}
	before = len(term.focusCalls)
	_, _ = m.Update(tea.FocusMsg{})
	if len(term.focusCalls) != before {
		t.Fatalf("window focus while the dock is unfocused reported focus: %v", term.focusCalls)
	}
}
