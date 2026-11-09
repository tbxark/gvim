package dock

import (
	"strconv"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// dockBytesFlags returns the bytes a key press sends to a child that enabled the
// Kitty keyboard protocol with flags, falling back to the legacy encoding for a
// child that did not (or for a key the protocol leaves in legacy form). It
// mirrors Fresh's kitty_encoded_key: the emulator never forwards key releases, so
// only presses are encoded (repeat events are presses too).
func dockBytesFlags(k tea.Key, appCursor bool, flags int) ([]byte, bool) {
	if seq, ok := kittyDockKey(k, flags); ok {
		return []byte(seq), true
	}
	return dockBytes(k, appCursor)
}

// kittyDockKey returns the CSI-u sequence for a key under the child's flags, and
// false when the protocol leaves the key in its legacy form.
func kittyDockKey(k tea.Key, flags int) (string, bool) {
	disambiguate := flags&ansi.KittyDisambiguateEscapeCodes != 0
	reportAll := flags&ansi.KittyReportAllKeysAsEscapeCodes != 0
	if !disambiguate && !reportAll && flags&ansi.KittyReportEventTypes == 0 {
		return "", false
	}
	code := k.Code
	if code == 0 && k.Text != "" {
		code = []rune(k.Text)[0]
	}
	bits := kittyModBits(k.Mod)
	// A legacy-compatible uppercase letter carries Shift; a kitty report can omit
	// it. Infer it the way Fresh's normalize_key does, unless Ctrl hides the case.
	if unicode.IsUpper(code) && bits == 0 && k.Mod&tea.ModCtrl == 0 {
		bits |= kittyShift
	}
	shifted := rune(0)
	if k.ShiftedCode != 0 {
		shifted = k.ShiftedCode
	} else if bits&kittyShift != 0 && unicode.IsLetter(code) {
		shifted = unicode.ToUpper(code)
	}
	alternate := ""
	if flags&ansi.KittyReportAlternateKeys != 0 && bits&kittyShift != 0 && shifted != 0 && shifted != code {
		alternate = strconv.Itoa(int(shifted))
	}
	text := ""
	if flags&ansi.KittyReportAllKeysAsEscapeCodes != 0 && flags&ansi.KittyReportAssociatedKeys != 0 && k.Text != "" {
		runes := []rune(k.Text)
		if len(runes) == 1 {
			text = strconv.Itoa(int(runes[0]))
		}
	}
	switch code {
	case tea.KeyEscape:
		if !disambiguate && !reportAll {
			return "", false
		}
		return kittyCSI(27, bits, "", text, 'u'), true
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		// Unmodified editing keys keep their legacy byte while a child uses the
		// protocol, so a shell stays usable if the program dies without popping
		// the flags. A modifier makes them a CSI-u key.
		number := 13
		switch code {
		case tea.KeyTab:
			number = 9
		case tea.KeyBackspace:
			number = 127
		}
		if bits == 0 && !reportAll {
			return "", false
		}
		return kittyCSI(number, bits, "", text, 'u'), true
	}
	if number, final, ok := kittyFunctionalKey(code); ok {
		return kittyCSI(number, bits, alternate, text, final), true
	}
	// Text keys. A key that types text and has no modifier beyond Shift goes out
	// as the text itself unless the child asked for all keys as escape codes.
	textKey := k.Text
	if textKey == "" && unicode.IsPrint(code) && bits&^(kittyShift) == 0 {
		textKey = string(code)
	}
	if bits&^kittyShift == 0 && textKey != "" && !reportAll {
		return "", false
	}
	if !disambiguate && !reportAll {
		return "", false
	}
	keyCode := code
	if unicode.IsLetter(keyCode) {
		keyCode = unicode.ToLower(keyCode)
	}
	return kittyCSI(int(keyCode), bits, alternate, text, 'u'), true
}

// kittyCSI builds CSI <number>[:<shifted>][;<mods>[;<text>]] <final>, omitting a
// bare number 1 as the protocol does (CSI A for an unmodified Up).
func kittyCSI(number int, bits int, alternate string, text string, final rune) string {
	var params []byte
	if number != 1 || alternate != "" || bits != 0 || text != "" {
		params = strconv.AppendInt(params, int64(number), 10)
	}
	if alternate != "" {
		params = append(params, ':')
		params = append(params, alternate...)
	}
	mode := kittyModParam(bits)
	if mode != 0 || text != "" {
		params = append(params, ';')
		if mode != 0 {
			params = strconv.AppendInt(params, int64(mode), 10)
		}
		if text != "" {
			params = append(params, ';')
			params = append(params, text...)
		}
	}
	return "\x1b[" + string(params) + string(final)
}

// The kitty protocol's modifier bits, in its parameter order.
const (
	kittyShift = 1 << iota
	kittyAlt
	kittyCtrl
	kittySuper
	kittyHyper
	kittyMeta
)

// kittyModBits maps the host key's modifiers to the protocol's bits. UV keeps Meta
// and Super apart for XTerm compatibility; the protocol's order is Super, Hyper,
// Meta, so ModSuper is bit 8 and ModMeta bit 32 here.
func kittyModBits(mod tea.KeyMod) int {
	bits := 0
	if mod&tea.ModShift != 0 {
		bits |= kittyShift
	}
	if mod&tea.ModAlt != 0 {
		bits |= kittyAlt
	}
	if mod&tea.ModCtrl != 0 {
		bits |= kittyCtrl
	}
	if mod&tea.ModSuper != 0 {
		bits |= kittySuper
	}
	if mod&tea.ModHyper != 0 {
		bits |= kittyHyper
	}
	if mod&tea.ModMeta != 0 {
		bits |= kittyMeta
	}
	return bits
}

// kittyModParam is 1 + the modifier bits, or 0 when there is no modifier.
func kittyModParam(bits int) int {
	if bits == 0 {
		return 0
	}
	return bits + 1
}

// kittyFunctionalKey returns the protocol number and final byte of a cursor,
// editing or function key. The dock's tea.Key codes follow the same key set as
// Fresh's crossterm codes where the protocols agree.
func kittyFunctionalKey(code rune) (int, rune, bool) {
	switch code {
	case tea.KeyUp:
		return 1, 'A', true
	case tea.KeyDown:
		return 1, 'B', true
	case tea.KeyRight:
		return 1, 'C', true
	case tea.KeyLeft:
		return 1, 'D', true
	case tea.KeyHome:
		return 1, 'H', true
	case tea.KeyEnd:
		return 1, 'F', true
	case tea.KeyInsert:
		return 2, '~', true
	case tea.KeyDelete:
		return 3, '~', true
	case tea.KeyPgUp:
		return 5, '~', true
	case tea.KeyPgDown:
		return 6, '~', true
	case tea.KeyF1:
		return 1, 'P', true
	case tea.KeyF2:
		return 1, 'Q', true
	case tea.KeyF3:
		return 13, '~', true
	case tea.KeyF4:
		return 1, 'S', true
	case tea.KeyF5:
		return 15, '~', true
	case tea.KeyF6:
		return 17, '~', true
	case tea.KeyF7:
		return 18, '~', true
	case tea.KeyF8:
		return 19, '~', true
	case tea.KeyF9:
		return 20, '~', true
	case tea.KeyF10:
		return 21, '~', true
	case tea.KeyF11:
		return 23, '~', true
	case tea.KeyF12:
		return 24, '~', true
	}
	if code >= tea.KeyF13 && code <= tea.KeyF35 {
		return 57376 + int(code-tea.KeyF13), 'u', true
	}
	return 0, 0, false
}
