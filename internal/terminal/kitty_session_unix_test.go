//go:build unix

package terminal

import (
	"strings"
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

// TestKittyKeyboardWire drives the protocol over a real PTY: a child enables the
// protocol, asks for the flags, prints the answer, then reads the CSI-u bytes an
// enabled terminal sends for Ctrl+I. Unlike the parser unit test, this covers the
// query reply and the encoded-key delivery through the PTY and x/vt.
func TestKittyKeyboardWire(t *testing.T) {
	// stty raw makes dd see answers and key bytes without a newline. The child
	// enables the protocol, queries it, pushes a second flag, pops back, and asks
	// again; each answer is printed as hex. It then reads the CSI-u bytes an
	// enabled terminal sends for Ctrl+I (ESC [ 105 ; 5 u, 8 bytes).
	script := "stty raw -echo; " +
		"printf '\\033[>1u\\033[?u'; reply=$(dd bs=1 count=5 2>/dev/null); " +
		"printf 'reply1='; printf '%s' \"$reply\" | od -An -tx1 | tr -d '\\n'; printf '\\r\\n'; " +
		"printf '\\033[>5u\\033[?u'; reply=$(dd bs=1 count=5 2>/dev/null); " +
		"printf 'reply2='; printf '%s' \"$reply\" | od -An -tx1 | tr -d '\\n'; printf '\\r\\n'; " +
		"printf '\\033[<u\\033[=1;3u\\033[?u'; reply=$(dd bs=1 count=5 2>/dev/null); " +
		"printf 'reply3='; printf '%s' \"$reply\" | od -An -tx1 | tr -d '\\n'; printf '\\r\\n'; " +
		"printf '\\033[>1u'; printf 'ready\\r\\n'; " +
		"dd bs=1 count=8 2>/dev/null | od -An -tx1; exec sleep 5"
	s := startSh(t, script)
	// Initial flags 0 + push 1 -> 1; push 5 replaces it; pop restores 1, then
	// removing bit 1 gives 0 again (a unique answer that proves the pop restored 1,
	// not the pushed 5). The child re-enables the protocol for the key read.
	for _, want := range []string{"1b 5b 3f 31 75", "1b 5b 3f 35 75", "1b 5b 3f 30 75"} {
		waitFor(t, s, "the kitty flags answer "+want, func(sc Screen) bool {
			return screenHasHex(sc, want)
		})
	}
	waitFor(t, s, "ready for the key", func(sc Screen) bool {
		return strings.Contains(screenText(sc), "ready")
	})
	if _, err := s.Write([]byte("\x1b[105;5u")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, s, "the CSI-u key bytes", func(sc Screen) bool {
		return screenHasHex(sc, "1b 5b 31 30 35 3b 35 75")
	})
}

// screenHasHex normalizes the screen's whitespace before looking for od's hex
// dump, which pads and groups bytes differently across platforms.
func screenHasHex(sc Screen, want string) bool {
	return strings.Contains(strings.Join(strings.Fields(screenText(sc)), " "), want)
}
