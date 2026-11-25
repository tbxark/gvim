package terminal

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// The Kitty keyboard protocol's progressive enhancement flags. The emulator
// answers a child's CSI ? u query with the flags the child pushed, so a TUI can
// ask for unambiguous CSI-u input (Ctrl+I distinct from Tab, Shift+Enter, Alt+[)
// and the dock can encode its keys in that form. Fresh tracks the same flags for
// the child (see docs/internal/terminal.md in refs/fresh and docs/terminal.md).
const (
	kittyDisambiguate uint8 = 1 << iota
	kittyReportEventTypes
	kittyReportAlternateKeys
	kittyReportAllKeys
	kittyReportAssociatedText
	kittyKnownFlags = kittyDisambiguate | kittyReportEventTypes | kittyReportAlternateKeys |
		kittyReportAllKeys | kittyReportAssociatedText
)

// kittyStackMax bounds the flags stack a child can build, so a program that
// pushes in a loop cannot grow the editor's memory. Kitty's own limit is lower;
// the newest entries win.
const kittyStackMax = 64

// kittyKeyboard is the per-session Kitty keyboard protocol state: the flags stack
// of each screen. Kitty keeps separate stacks for the main and alternate screens,
// and the child's push/pop therefore follow the screen it is on when it sends
// them. The zero value is a child that never enabled the protocol.
//
// The emulator parses the CSI-u controls itself: newSession registers the four
// prefixed forms (query CSI ? u, push CSI > flags u, pop CSI < n u, set
// CSI = flags ; mode u) as handlers that run inside emu.Write, in exact
// byte-stream order with the AltScreen callback that switches the active stack.
//
// Only presses are sent to the child by the dock, as in Fresh (it never forwards
// releases), so the report-event-types flag affects the child's own reporting from
// the host terminal, not gvim's encoding: there is no release event for gvim to
// forward.
type kittyKeyboard struct {
	// screens[0] is the main screen's stack and screens[1] the alternate
	// screen's. The current flags are the top of the active screen's stack, or
	// zero when it has no entries.
	screens [2][]uint8
	screen  int
}

// setAltScreen switches the active flags stack. The other screen's stack is kept,
// as the protocol specifies.
func (k *kittyKeyboard) setAltScreen(on bool) {
	if on {
		k.screen = 1
	} else {
		k.screen = 0
	}
}

// flags returns the enhancement flags the child currently has in effect.
func (k *kittyKeyboard) flags() uint8 {
	stack := k.screens[k.screen]
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1]
}

// kittyQuery is the emulator's handler for CSI ? u: it queues the flags reply
// for feed to send to the child. The parameters, if any, are ignored, as they
// were by the scanner this replaces.
func (s *Session) kittyQuery(ansi.Params) bool {
	s.kittyReplies = append(s.kittyReplies, []byte(fmt.Sprintf("\x1b[?%du", s.kitty.flags())))
	return true
}

// kittyPush is the emulator's handler for CSI > flags u.
func (s *Session) kittyPush(params ansi.Params) bool {
	if len(params) <= 1 {
		if flags, ok := kittyFlagsParam(params, 0); ok {
			s.kitty.push(flags)
		}
	}
	return true
}

// kittyPop is the emulator's handler for CSI < n u.
func (s *Session) kittyPop(params ansi.Params) bool {
	if len(params) <= 1 {
		if n, ok := kittyCountParam(params, 0); ok {
			s.kitty.pop(n)
		}
	}
	return true
}

// kittySet is the emulator's handler for CSI = flags ; mode u.
func (s *Session) kittySet(params ansi.Params) bool {
	if len(params) > 2 {
		return true
	}
	flags, ok := kittyFlagsParam(params, 0)
	if !ok {
		return true
	}
	mode, ok := kittyCountParam(params, 1)
	if !ok || mode > 3 {
		return true
	}
	s.kitty.set(flags, mode)
	return true
}

// kittyFlagsParam reads the enhancement-flags parameter at index i: a missing
// or empty parameter is zero flags, a value above 255 is ignored, and the known
// bits are kept. Sub-parameters (a `:` separator) are not part of the protocol.
func kittyFlagsParam(params ansi.Params, i int) (uint8, bool) {
	v, more, _ := params.Param(i, 0)
	if more || v > 255 {
		return 0, false
	}
	return uint8(v) & kittyKnownFlags, true
}

// kittyCountParam reads a positive decimal count at index i (the pop count or
// the set mode); a missing or empty parameter means one.
func kittyCountParam(params ansi.Params, i int) (int, bool) {
	v, more, _ := params.Param(i, 1)
	if more || v < 1 {
		return 0, false
	}
	return v, true
}

// push makes flags current on the active screen. A full stack drops its oldest
// entry, so the child can always push the flags it is about to use.
func (k *kittyKeyboard) push(flags uint8) {
	stack := k.screens[k.screen]
	if len(stack) >= kittyStackMax {
		stack = stack[len(stack)-(kittyStackMax-1):]
	}
	stack = append(stack, flags&kittyKnownFlags)
	k.screens[k.screen] = stack
}

// pop removes n entries from the active screen's stack. n is bounded to the
// number of entries, so popping more than the child pushed is harmless.
func (k *kittyKeyboard) pop(n int) {
	stack := k.screens[k.screen]
	if n > len(stack) {
		n = len(stack)
	}
	k.screens[k.screen] = stack[:len(stack)-n]
}

// set applies CSI = flags ; mode u to the current flags. Mode 1 sets them, mode 2
// adds them and mode 3 removes them. A child that sets flags without pushing first
// gets a base entry, so a later pop returns it to no enhancement.
func (k *kittyKeyboard) set(flags uint8, mode int) {
	stack := k.screens[k.screen]
	if len(stack) == 0 {
		stack = append(stack, 0)
	}
	top := len(stack) - 1
	flags &= kittyKnownFlags
	switch mode {
	case 1:
		stack[top] = flags
	case 2:
		stack[top] |= flags
	case 3:
		stack[top] &^= flags
	}
	k.screens[k.screen] = stack
}
