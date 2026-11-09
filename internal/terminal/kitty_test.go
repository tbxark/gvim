package terminal

import (
	"slices"
	"testing"
)

// TestKittyKeyboardQueryAndStack covers the query answer and the push/pop stack a
// child uses to enable and disable the protocol.
func TestKittyKeyboardQueryAndStack(t *testing.T) {
	var k kittyKeyboard
	check := func(in string, want ...string) {
		t.Helper()
		var replies []string
		for _, reply := range k.scan([]byte(in)) {
			replies = append(replies, string(reply))
		}
		if !slices.Equal(replies, want) {
			t.Fatalf("scan(%q) replies = %v, want %v", in, replies, want)
		}
	}
	check("\x1b[?u", "\x1b[?0u")

	// CSI > flags u pushes the flags; the query reports the new top.
	check("\x1b[>5u")
	k.setAltScreen(false)
	check("\x1b[?u", "\x1b[?5u")

	// A second push replaces the current flags, and a pop returns to the first.
	check("\x1b[>1u")
	check("\x1b[?u", "\x1b[?1u")
	check("\x1b[<u")
	check("\x1b[?u", "\x1b[?5u")

	// Popping every entry goes back to legacy encoding.
	check("\x1b[<2u")
	if got := k.flags(); got != 0 {
		t.Fatalf("flags after popping everything = %d, want 0", got)
	}
	check("\x1b[?u", "\x1b[?0u")
}

// TestKittyKeyboardSetModes covers CSI = flags ; mode u: set, add and remove.
func TestKittyKeyboardSetModes(t *testing.T) {
	var k kittyKeyboard
	k.scan([]byte("\x1b[=1;1u"))
	if got := k.flags(); got != kittyDisambiguate {
		t.Fatalf("set mode flags = %d, want disambiguate", got)
	}
	k.scan([]byte("\x1b[=4;2u"))
	if got := k.flags(); got != kittyDisambiguate|kittyReportAlternateKeys {
		t.Fatalf("add mode flags = %d, want disambiguate|alternate", got)
	}
	k.scan([]byte("\x1b[=1;3u"))
	if got := k.flags(); got != kittyReportAlternateKeys {
		t.Fatalf("remove mode flags = %d, want alternate only", got)
	}
	// A child that set without pushing gets a base entry; popping clears it.
	k.scan([]byte("\x1b[<u"))
	if got := k.flags(); got != 0 {
		t.Fatalf("flags after popping the set base = %d, want 0", got)
	}
}

// TestKittyKeyboardPerScreenStacks checks that main and alternate screens keep
// separate stacks, as the protocol requires.
func TestKittyKeyboardPerScreenStacks(t *testing.T) {
	var k kittyKeyboard
	k.scan([]byte("\x1b[>1u"))
	if got := k.flags(); got != kittyDisambiguate {
		t.Fatalf("main flags = %d", got)
	}
	k.setAltScreen(true)
	if got := k.flags(); got != 0 {
		t.Fatalf("alternate screen starts with %d, want 0", got)
	}
	k.scan([]byte("\x1b[>5u"))
	if got := k.flags(); got != kittyDisambiguate|kittyReportAlternateKeys {
		t.Fatalf("alternate flags = %d", got)
	}
	k.setAltScreen(false)
	if got := k.flags(); got != kittyDisambiguate {
		t.Fatalf("main flags after returning = %d, want disambiguate", got)
	}
	k.setAltScreen(true)
	if got := k.flags(); got != kittyDisambiguate|kittyReportAlternateKeys {
		t.Fatalf("alternate flags after returning = %d", got)
	}
}

// TestKittyKeyboardSplitSequences checks a control sequence split across reads and
// that other output is ignored.
func TestKittyKeyboardSplitSequences(t *testing.T) {
	var k kittyKeyboard
	if replies := k.scan([]byte("\x1b[?")); len(replies) != 0 {
		t.Fatalf("a partial query answered %v", replies)
	}
	if replies := k.scan([]byte("u")); !slices.EqualFunc(replies, [][]byte{[]byte("\x1b[?0u")}, slices.Equal[[]byte]) {
		t.Fatalf("the split query answered %v", replies)
	}
	// Another control sequence and plain text leave the flags untouched.
	if replies := k.scan([]byte("hello\x1b[200~paste\x1b[201~\x1b[>2u")); len(replies) != 0 {
		t.Fatalf("non-kitty output answered %v", replies)
	}
	if got := k.flags(); got != kittyReportEventTypes {
		t.Fatalf("flags after other output = %d, want event types", got)
	}
}

// TestKittyKeyboardBounds checks the stack and pending-sequence bounds: a child
// cannot grow the editor's memory or mask an unbounded sequence across reads.
func TestKittyKeyboardBounds(t *testing.T) {
	var k kittyKeyboard
	for range kittyStackMax + 10 {
		k.scan([]byte("\x1b[>1u"))
	}
	if got := len(k.screens[0]); got != kittyStackMax {
		t.Fatalf("stack depth = %d, want the bound %d", got, kittyStackMax)
	}
	k.scan([]byte("\x1b[<999u"))
	if got := k.flags(); got != 0 {
		t.Fatalf("over-popping left flags %d, want 0", got)
	}
	if replies := k.scan([]byte("\x1b[" + string(make([]byte, kittyPendingMax+1)))); len(replies) != 0 {
		t.Fatalf("an oversized pending sequence answered %v", replies)
	}
}
