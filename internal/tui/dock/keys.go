package dock

import (
	"fmt"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// The xterm sequences of the keys that have one. Arrows, Home and End end in a
// letter. The editing and function keys end in ~ after a number. F1 to F4 use SS3
// with no modifier and the CSI form with one.
var (
	csiLetters = map[rune]byte{
		tea.KeyUp: 'A', tea.KeyDown: 'B', tea.KeyRight: 'C', tea.KeyLeft: 'D',
		tea.KeyHome: 'H', tea.KeyEnd: 'F',
	}
	csiNumbers = map[rune]int{
		tea.KeyInsert: 2, tea.KeyDelete: 3, tea.KeyPgUp: 5, tea.KeyPgDown: 6,
		tea.KeyF5: 15, tea.KeyF6: 17, tea.KeyF7: 18, tea.KeyF8: 19,
		tea.KeyF9: 20, tea.KeyF10: 21, tea.KeyF11: 23, tea.KeyF12: 24,
	}
	ss3Letters = map[rune]byte{tea.KeyF1: 'P', tea.KeyF2: 'Q', tea.KeyF3: 'R', tea.KeyF4: 'S'}
)

// dockBytes returns the bytes a key press sends to the shell, and false when the key
// sends nothing. appCursor is the shell's application cursor keys mode. Printable text is
// sent as UTF-8. Ctrl letters and the few punctuation keys with a control byte are sent
// as that byte. Alt puts an ESC in front of text and control keys. The cursor, editing
// and function keys keep the xterm form, with their modifiers in the parameter, so
// Alt+Left is ESC [ 1 ; 3 D. Without a modifier, the arrows, Home and End take the SS3
// form, ESC O A, while appCursor is on.
func dockBytes(k tea.Key, appCursor bool) ([]byte, bool) {
	if seq, ok := csiKey(k, appCursor); ok {
		return []byte(seq), true
	}
	ctrl := k.Mod&tea.ModCtrl != 0
	var seq string
	switch {
	case k.Code == tea.KeyEnter:
		seq = "\r"
	case k.Code == tea.KeyBackspace && ctrl:
		seq = "\b"
	case k.Code == tea.KeyBackspace:
		seq = "\x7f"
	case k.Code == tea.KeyTab:
		seq = "\t"
	case k.Code == tea.KeyEscape:
		seq = "\x1b"
	case ctrl:
		b, ok := ctrlByte(k.Code)
		if !ok {
			return nil, false
		}
		seq = string(b)
	case k.Text != "":
		seq = k.Text
	case k.Code > 0x1f && k.Code != 0x7f && unicode.IsPrint(k.Code):
		seq = string(k.Code)
	default:
		return nil, false
	}
	if k.Mod&tea.ModAlt != 0 {
		seq = "\x1b" + seq
	}
	return []byte(seq), true
}

// csiKey returns the xterm sequence of a cursor, editing or function key, and false
// for any other key. Shift+Tab is the back-tab sequence. An unmodified cursor key or
// Home or End is in the SS3 form while appCursor is on.
func csiKey(k tea.Key, appCursor bool) (string, bool) {
	if k.Code == tea.KeyTab && k.Mod&tea.ModShift != 0 {
		return "\x1b[Z", true
	}
	mods := xtermMods(k.Mod)
	if letter, ok := csiLetters[k.Code]; ok {
		if mods == 1 {
			if appCursor {
				return fmt.Sprintf("\x1bO%c", letter), true
			}
			return fmt.Sprintf("\x1b[%c", letter), true
		}
		return fmt.Sprintf("\x1b[1;%d%c", mods, letter), true
	}
	if n, ok := csiNumbers[k.Code]; ok {
		if mods == 1 {
			return fmt.Sprintf("\x1b[%d~", n), true
		}
		return fmt.Sprintf("\x1b[%d;%d~", n, mods), true
	}
	if letter, ok := ss3Letters[k.Code]; ok {
		if mods == 1 {
			return fmt.Sprintf("\x1bO%c", letter), true
		}
		return fmt.Sprintf("\x1b[1;%d%c", mods, letter), true
	}
	return "", false
}

// xtermMods is the xterm modifier parameter: 1, plus 1 for Shift, 2 for Alt and 4 for
// Ctrl.
func xtermMods(m tea.KeyMod) int {
	n := 1
	if m&tea.ModShift != 0 {
		n++
	}
	if m&tea.ModAlt != 0 {
		n += 2
	}
	if m&tea.ModCtrl != 0 {
		n += 4
	}
	return n
}

// ctrlByte is the control byte that Ctrl with r sends: Ctrl+A to Ctrl+Z, and the
// punctuation keys that have one. It reports false for other keys.
func ctrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r-'a') + 1, true
	case r >= 'A' && r <= 'Z':
		return byte(r-'A') + 1, true
	}
	switch r {
	case ' ', '@':
		return 0x00, true
	case '[':
		return 0x1b, true
	case '\\':
		return 0x1c, true
	case ']':
		return 0x1d, true
	case '^':
		return 0x1e, true
	case '_':
		return 0x1f, true
	case '?':
		return 0x7f, true
	}
	return 0, false
}
