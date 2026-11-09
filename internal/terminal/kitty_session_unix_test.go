//go:build unix

package terminal

import (
	"testing"
)

// TestSessionKittyKeyboard feeds the child-side protocol controls through the
// session. The query answer itself is covered by the parser test; here the point is
// the session wiring: push/pop reach the flags the dock reads, and the per-screen
// stacks follow the emulator's alternate-screen callback.
func TestSessionKittyKeyboard(t *testing.T) {
	s := startSh(t, "exec sleep 5")
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("initial flags = %d, want 0", got)
	}
	s.feed([]byte("\x1b[>5u"))
	if got := s.KittyKeyboardFlags(); got != 5 {
		t.Fatalf("flags after push = %d, want 5", got)
	}
	s.feed([]byte("\x1b[<u"))
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("flags after pop = %d, want 0 (legacy fallback)", got)
	}

	// The flags are per screen: the alternate screen starts empty and the main
	// screen's stack comes back when the child leaves it.
	s.feed([]byte("\x1b[>1u"))
	if got := s.KittyKeyboardFlags(); got != 1 {
		t.Fatalf("main flags = %d, want 1", got)
	}
	s.feed([]byte("\x1b[?1049h"))
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("alternate flags = %d, want 0", got)
	}
	s.feed([]byte("\x1b[>2u"))
	if got := s.KittyKeyboardFlags(); got != 2 {
		t.Fatalf("alternate flags after push = %d, want 2", got)
	}
	s.feed([]byte("\x1b[?1049l"))
	if got := s.KittyKeyboardFlags(); got != 1 {
		t.Fatalf("main flags after leaving the alternate screen = %d, want 1", got)
	}
}
