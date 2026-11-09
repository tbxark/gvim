package dock

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPanelSendKeyUsesChildKittyFlags: the panel reads the active child's flags
// from the terminal and uses CSI-u while they are set, then legacy bytes again
// after the child disables the protocol.
func TestPanelSendKeyUsesChildKittyFlags(t *testing.T) {
	starter := &fakeStarter{term: newFakeTerm()}
	p := newTestPanel(t, starter)
	starter.term.kittyFlags = 1 // disambiguate escape codes

	p.SendKey(tea.Key{Code: 'i', Mod: tea.ModCtrl})
	runQueue(p, starter.term)
	if want := []string{"\x1b[105;5u"}; !slices.Equal(starter.term.writes, want) {
		t.Fatalf("kitty writes = %q, want %q", starter.term.writes, want)
	}

	starter.term.kittyFlags = 0
	starter.term.writes = nil
	p.SendKey(tea.Key{Code: 'i', Mod: tea.ModCtrl})
	runQueue(p, starter.term)
	if want := []string{"\t"}; !slices.Equal(starter.term.writes, want) {
		t.Fatalf("legacy writes = %q, want %q", starter.term.writes, want)
	}
}

// runQueue runs the panel's queued input writes directly against term. It avoids
// the dock's shell-lifecycle waits, which these encoding tests do not exercise.
func runQueue(p *Panel, term *fakeTerm) {
	for _, fn := range p.queue {
		_ = fn(term)
	}
	p.queue = nil
}
