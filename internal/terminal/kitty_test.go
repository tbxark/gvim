package terminal

import (
	"strings"
	"testing"
)

// TestKittyKeyboardQueryAndStack covers the query answer and the push/pop stack a
// child uses to enable and disable the protocol.
func TestKittyKeyboardQueryAndStack(t *testing.T) {
	s, p := startFeedSession(t)
	feed := func(in, wantReply string) {
		t.Helper()
		p.reset()
		s.feed([]byte(in))
		waitWritten(t, p, wantReply)
	}
	feed("\x1b[?u", "\x1b[?0u")

	// CSI > flags u pushes the flags; the query reports the new top.
	feed("\x1b[>5u\x1b[?u", "\x1b[?5u")

	// A second push replaces the current flags, and a pop returns to the first.
	feed("\x1b[>1u\x1b[?u", "\x1b[?1u")
	feed("\x1b[<u\x1b[?u", "\x1b[?5u")

	// Popping every entry goes back to legacy encoding.
	feed("\x1b[<2u\x1b[?u", "\x1b[?0u")
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("flags after popping everything = %d, want 0", got)
	}
}

// TestKittyKeyboardSetModes covers CSI = flags ; mode u: set, add and remove.
func TestKittyKeyboardSetModes(t *testing.T) {
	s, p := startFeedSession(t)
	s.feed([]byte("\x1b[=1;1u"))
	if got := s.KittyKeyboardFlags(); got != kittyDisambiguate {
		t.Fatalf("set mode flags = %d, want disambiguate", got)
	}
	s.feed([]byte("\x1b[=4;2u"))
	if got := s.KittyKeyboardFlags(); got != kittyDisambiguate|kittyReportAlternateKeys {
		t.Fatalf("add mode flags = %d, want disambiguate|alternate", got)
	}
	s.feed([]byte("\x1b[=1;3u"))
	if got := s.KittyKeyboardFlags(); got != kittyReportAlternateKeys {
		t.Fatalf("remove mode flags = %d, want alternate only", got)
	}
	// A child that set without pushing gets a base entry; popping clears it. The
	// one reply also proves the controls above queued nothing themselves.
	s.feed([]byte("\x1b[<u\x1b[?u"))
	waitWritten(t, p, "\x1b[?0u")
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("flags after popping the set base = %d, want 0", got)
	}
}

// TestKittyKeyboardPerScreenStacks checks that main and alternate screens keep
// separate stacks, as the protocol requires.
func TestKittyKeyboardPerScreenStacks(t *testing.T) {
	s, p := startFeedSession(t)
	s.feed([]byte("\x1b[>1u"))
	if got := s.KittyKeyboardFlags(); got != kittyDisambiguate {
		t.Fatalf("main flags = %d", got)
	}
	s.feed([]byte("\x1b[?1049h"))
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("alternate screen starts with %d, want 0", got)
	}
	s.feed([]byte("\x1b[>5u"))
	if got := s.KittyKeyboardFlags(); got != kittyDisambiguate|kittyReportAlternateKeys {
		t.Fatalf("alternate flags = %d", got)
	}
	s.feed([]byte("\x1b[?1049l"))
	if got := s.KittyKeyboardFlags(); got != kittyDisambiguate {
		t.Fatalf("main flags after returning = %d, want disambiguate", got)
	}
	s.feed([]byte("\x1b[?1049h\x1b[?u"))
	waitWritten(t, p, "\x1b[?5u")
	if got := s.KittyKeyboardFlags(); got != kittyDisambiguate|kittyReportAlternateKeys {
		t.Fatalf("alternate flags after returning = %d", got)
	}
}

// TestKittyKeyboardSplitSequences checks a control sequence split across reads and
// that other output is ignored.
func TestKittyKeyboardSplitSequences(t *testing.T) {
	s, p := startFeedSession(t)
	s.feed([]byte("\x1b[?"))
	s.feed([]byte("u"))
	waitWritten(t, p, "\x1b[?0u")
	// Another control sequence and plain text leave the flags untouched; the
	// query reply is the only bytes a chunk with push+query may produce.
	p.reset()
	s.feed([]byte("hello\x1b[200~paste\x1b[201~\x1b[>2u\x1b[?u"))
	waitWritten(t, p, "\x1b[?2u")
	if got := s.KittyKeyboardFlags(); got != kittyReportEventTypes {
		t.Fatalf("flags after other output = %d, want event types", got)
	}
}

// TestKittyKeyboardBounds checks the stack bound: a child cannot grow the
// editor's memory by pushing in a loop, and popping past the bottom is harmless.
func TestKittyKeyboardBounds(t *testing.T) {
	s, p := startFeedSession(t)
	s.feed([]byte(strings.Repeat("\x1b[>1u", kittyStackMax+10)))
	if got := len(s.kitty.screens[0]); got != kittyStackMax {
		t.Fatalf("stack depth = %d, want the bound %d", got, kittyStackMax)
	}
	s.feed([]byte("\x1b[<999u\x1b[?u"))
	waitWritten(t, p, "\x1b[?0u")
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("over-popping left flags %d, want 0", got)
	}
}

// TestKittyKeyboardPushDefaultsAndEviction covers the protocol's push edge cases:
// an omitted flags parameter pushes zero, a full stack evicts its oldest entry,
// and popping past the bottom resets the flags.
func TestKittyKeyboardPushDefaultsAndEviction(t *testing.T) {
	s, _ := startFeedSession(t)
	s.feed([]byte("\x1b[>5u"))
	if got := s.KittyKeyboardFlags(); got != 5 {
		t.Fatalf("current flags = %d, want 5", got)
	}
	s.feed([]byte("\x1b[>u")) // omitted flags default to zero
	if got := s.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("flags after an omitted push = %d, want 0", got)
	}
	s.feed([]byte("\x1b[<u"))
	if got := s.KittyKeyboardFlags(); got != 5 {
		t.Fatalf("flags after popping the default push = %d, want 5", got)
	}

	// Fill past the bound with an old distinct entry first: the oldest entry is
	// evicted, so popping the retained entries returns ones, not the old five.
	full, _ := startFeedSession(t)
	full.feed([]byte("\x1b[>5u" + strings.Repeat("\x1b[>1u", kittyStackMax-1)))
	if got := len(full.kitty.screens[0]); got != kittyStackMax {
		t.Fatalf("stack depth = %d, want %d", got, kittyStackMax)
	}
	full.feed([]byte("\x1b[>2u")) // one more push evicts the oldest (5)
	full.feed([]byte(strings.Repeat("\x1b[<u", kittyStackMax-1)))
	if got := full.KittyKeyboardFlags(); got != 1 {
		t.Fatalf("flags after evicting the oldest entry = %d, want the retained 1", got)
	}
	full.feed([]byte("\x1b[<9u"))
	if got := full.KittyKeyboardFlags(); got != 0 {
		t.Fatalf("over-popping left flags %d, want 0", got)
	}
}

// TestKittyKeyboardInlineAltScreenOrder verifies that DECSET/DECRST alt-screen
// switches interleaved in the same read as Kitty push/pop/query apply in exact
// byte-stream order.
func TestKittyKeyboardInlineAltScreenOrder(t *testing.T) {
	t.Parallel()

	t.Run("enter alt then push targets alt stack", func(t *testing.T) {
		s, _ := startFeedSession(t)
		s.feed([]byte("\x1b[?1049h\x1b[>1u"))
		if s.kitty.screen != 1 || s.KittyKeyboardFlags() != kittyDisambiguate {
			t.Fatalf("alt screen=%d flags=%d, want screen=1 flags=1", s.kitty.screen, s.KittyKeyboardFlags())
		}
		s.feed([]byte("\x1b[?1049l"))
		if s.kitty.screen != 0 || s.KittyKeyboardFlags() != 0 {
			t.Fatalf("main screen=%d flags=%d, want screen=0 flags=0", s.kitty.screen, s.KittyKeyboardFlags())
		}
	})

	t.Run("pop then leave alt pops alt stack without clobbering main", func(t *testing.T) {
		s, p := startFeedSession(t)
		s.feed([]byte("\x1b[>2u"))
		s.feed([]byte("\x1b[?1049h\x1b[>5u"))
		s.feed([]byte("\x1b[<u\x1b[?1049l\x1b[?u"))
		waitWritten(t, p, "\x1b[?2u")
		if s.kitty.screen != 0 || s.KittyKeyboardFlags() != kittyReportEventTypes {
			t.Fatalf("after pop+leave screen=%d flags=%d, want screen=0 flags=2", s.kitty.screen, s.KittyKeyboardFlags())
		}
		s.feed([]byte("\x1b[?1049h"))
		if got := s.KittyKeyboardFlags(); got != 0 {
			t.Fatalf("alt stack after returning = %d, want 0 (was popped)", got)
		}
	})

	t.Run("multi-param private mode and modes 47/1047", func(t *testing.T) {
		s, p := startFeedSession(t)
		s.feed([]byte("\x1b[?1;1049h\x1b[>1u"))
		if s.kitty.screen != 1 || s.KittyKeyboardFlags() != 1 {
			t.Fatalf("multi-param enter: screen=%d flags=%d", s.kitty.screen, s.KittyKeyboardFlags())
		}
		// Mode 47 is not an alternate-screen switch to x/vt (its alt modes are
		// 1047 and 1049), so it leaves the child on the screen it is on: the push
		// and the query target the alt stack. 1047h is a no-op already on alt.
		s.feed([]byte("\x1b[?47l\x1b[>4u\x1b[?1047h\x1b[?u"))
		waitWritten(t, p, "\x1b[?4u")
		if s.kitty.screen != 1 {
			t.Fatalf("after 47l+1047h: screen=%d, want 1 (still alt)", s.kitty.screen)
		}
		p.reset()
		s.feed([]byte("\x1b[?1047l\x1b[?u"))
		waitWritten(t, p, "\x1b[?0u")
	})

	t.Run("split alt-screen and push across chunks", func(t *testing.T) {
		s, _ := startFeedSession(t)
		s.feed([]byte("\x1b[?104"))
		s.feed([]byte("9h\x1b[>1u"))
		if s.kitty.screen != 1 || s.KittyKeyboardFlags() != 1 {
			t.Fatalf("split enter+push: screen=%d flags=%d", s.kitty.screen, s.KittyKeyboardFlags())
		}
	})

	t.Run("non-private h/l and empty private h/l do not switch screen", func(t *testing.T) {
		s, _ := startFeedSession(t)
		s.feed([]byte("\x1b[4h\x1b[h\x1b[?h\x1b[?1049h\x1b[4l\x1b[l\x1b[?l"))
		if s.kitty.screen != 1 {
			t.Fatalf("screen=%d, want 1 (non-private modes ignored)", s.kitty.screen)
		}
	})
}

// BenchmarkFeedSGR keeps a per-read cost baseline for output with no protocol
// sequences, now that the emulator's parser owns the scanning.
func BenchmarkFeedSGR(b *testing.B) {
	chunk := make([]byte, 0, 8192)
	sample := []byte("\x1b[38;5;196mhello \x1b[0m\x1b[1;32mworld\x1b[0m\x1b[12;34H\x1b[K")
	for len(chunk) < 8192 {
		chunk = append(chunk, sample...)
	}
	p := newFeedProc()
	s := newSession(p, 80, 24, 0)
	b.Cleanup(func() { _ = s.Close() })
	b.ReportAllocs()
	for b.Loop() {
		if !s.feed(chunk) {
			b.Fatal("feed on a closed session")
		}
	}
}
