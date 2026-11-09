package terminal

import (
	"fmt"
	"strconv"
	"strings"
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

// kittyPendingMax bounds an incomplete control sequence carried between two
// reads. A real query, push or pop is a few bytes; anything longer is not a
// sequence this parser needs to answer.
const kittyPendingMax = 128

// kittyKeyboard is the per-session Kitty keyboard protocol state: the flags stack
// of each screen. Kitty keeps separate stacks for the main and alternate screens,
// and the child's push/pop therefore follow the screen it is on when it sends
// them. The zero value is a child that never enabled the protocol.
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
	// pending is an incomplete CSI sequence (ESC [ ...) from the end of the last
	// read.
	pending []byte
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

// scan consumes one read of child output and returns the replies to queue back
// to the child. It only looks at CSI sequences whose final byte is u: the query
// (CSI ? u), push (CSI > flags u), pop (CSI < n u) and set (CSI = flags ; mode u).
// All other bytes, including other control sequences, pass through untouched; the
// bytes are still written to the emulator by the caller.
func (k *kittyKeyboard) scan(p []byte) [][]byte {
	data := p
	if len(k.pending) > 0 {
		joined := make([]byte, 0, len(k.pending)+len(p))
		joined = append(joined, k.pending...)
		joined = append(joined, p...)
		k.pending = k.pending[:0]
		data = joined
	}
	var replies [][]byte
	for i := 0; i < len(data); {
		if data[i] != 0x1b {
			i++
			continue
		}
		if i+1 >= len(data) {
			k.keepPending(data[i:])
			return replies
		}
		if data[i+1] != '[' {
			i++
			continue
		}
		// A CSI sequence ends at the first byte in 0x40..0x7e.
		j := i + 2
		for j < len(data) && (data[j] < 0x40 || data[j] > 0x7e) {
			j++
		}
		if j >= len(data) {
			k.keepPending(data[i:])
			return replies
		}
		if data[j] == 'u' {
			if reply := k.command(string(data[i+2 : j])); reply != nil {
				replies = append(replies, reply)
			}
		}
		i = j + 1
	}
	return replies
}

// keepPending remembers an incomplete sequence for the next read, unless it is
// too long to be one of the sequences this parser answers.
func (k *kittyKeyboard) keepPending(rest []byte) {
	if len(rest) > kittyPendingMax {
		return
	}
	k.pending = append(k.pending[:0], rest...)
}

// command applies one CSI u body (the bytes between CSI and u) and returns the
// reply bytes, or nil when the body asks nothing.
func (k *kittyKeyboard) command(body string) []byte {
	if body == "" {
		return nil
	}
	marker, rest := body[0], body[1:]
	switch marker {
	case '?':
		return []byte(fmt.Sprintf("\x1b[?%du", k.flags()))
	case '>':
		flags, ok := kittyFlagsParam(rest)
		if !ok {
			return nil
		}
		k.push(flags)
	case '<':
		n, ok := kittyCountParam(rest)
		if !ok {
			return nil
		}
		k.pop(n)
	case '=':
		flagsText, modeText, _ := strings.Cut(rest, ";")
		flags, ok := kittyFlagsParam(flagsText)
		if !ok {
			return nil
		}
		mode := 1
		if modeText != "" {
			m, ok := kittyCountParam(modeText)
			if !ok || (m != 1 && m != 2 && m != 3) {
				return nil
			}
			mode = m
		}
		k.set(flags, mode)
	}
	return nil
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

// kittyFlagsParam parses a decimal enhancement-flags parameter. An empty value is
// zero flags; the known bits are kept and anything above them is ignored.
func kittyFlagsParam(text string) (uint8, bool) {
	if text == "" {
		return 0, true
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 || n > 255 {
		return 0, false
	}
	return uint8(n) & kittyKnownFlags, true
}

// kittyCountParam parses a positive decimal count; an empty value means one, as
// the protocol's pop parameter does.
func kittyCountParam(text string) (int, bool) {
	if text == "" {
		return 1, true
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
