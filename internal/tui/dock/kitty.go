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
	// it. Infer it the way Fresh's normalize_key does, even next to another
	// modifier (Alt+Shift+a reaches a legacy host as "A"+Alt without Shift).
	if unicode.IsUpper(code) && k.Mod&tea.ModShift == 0 {
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
	// Modifier-only keys are reported only when the child asked for every key as
	// an escape code; without that flag they have no report.
	if number, final, ok := kittyModifierKey(code); ok {
		if !reportAll {
			return "", false
		}
		return kittyCSI(number, bits, "", text, final), true
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

// kittyModBits maps the host key's modifiers to the protocol's bits. UV swaps
// Meta and Super relative to the protocol to keep XTerm compatibility: its
// ModMeta (the XTerm meta bit, 8) is the protocol's Super, its ModHyper is Hyper,
// and its ModSuper (32) is the protocol's Meta.
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
	if mod&tea.ModMeta != 0 {
		bits |= kittySuper
	}
	if mod&tea.ModHyper != 0 {
		bits |= kittyHyper
	}
	if mod&tea.ModSuper != 0 {
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
	case tea.KeyCapsLock:
		return 57358, 'u', true
	case tea.KeyScrollLock:
		return 57359, 'u', true
	case tea.KeyNumLock:
		return 57360, 'u', true
	case tea.KeyPrintScreen:
		return 57361, 'u', true
	case tea.KeyPause:
		return 57362, 'u', true
	case tea.KeyMenu:
		return 57363, 'u', true
	case tea.KeyKp0:
		return 57399, 'u', true
	case tea.KeyKp1:
		return 57400, 'u', true
	case tea.KeyKp2:
		return 57401, 'u', true
	case tea.KeyKp3:
		return 57402, 'u', true
	case tea.KeyKp4:
		return 57403, 'u', true
	case tea.KeyKp5:
		return 57404, 'u', true
	case tea.KeyKp6:
		return 57405, 'u', true
	case tea.KeyKp7:
		return 57406, 'u', true
	case tea.KeyKp8:
		return 57407, 'u', true
	case tea.KeyKp9:
		return 57408, 'u', true
	case tea.KeyKpDecimal:
		return 57409, 'u', true
	case tea.KeyKpDivide:
		return 57410, 'u', true
	case tea.KeyKpMultiply:
		return 57411, 'u', true
	case tea.KeyKpMinus:
		return 57412, 'u', true
	case tea.KeyKpPlus:
		return 57413, 'u', true
	case tea.KeyKpEnter:
		return 57414, 'u', true
	case tea.KeyKpEqual:
		return 57415, 'u', true
	case tea.KeyKpSep:
		return 57416, 'u', true
	case tea.KeyKpLeft:
		return 57417, 'u', true
	case tea.KeyKpRight:
		return 57418, 'u', true
	case tea.KeyKpUp:
		return 57419, 'u', true
	case tea.KeyKpDown:
		return 57420, 'u', true
	case tea.KeyKpPgUp:
		return 57421, 'u', true
	case tea.KeyKpPgDown:
		return 57422, 'u', true
	case tea.KeyKpHome:
		return 57423, 'u', true
	case tea.KeyKpEnd:
		return 57424, 'u', true
	case tea.KeyKpInsert:
		return 57425, 'u', true
	case tea.KeyKpDelete:
		return 57426, 'u', true
	case tea.KeyKpBegin:
		return 1, 'E', true
	case tea.KeyMediaPlay:
		return 57428, 'u', true
	case tea.KeyMediaPause:
		return 57429, 'u', true
	case tea.KeyMediaPlayPause:
		return 57430, 'u', true
	case tea.KeyMediaReverse:
		return 57431, 'u', true
	case tea.KeyMediaStop:
		return 57432, 'u', true
	case tea.KeyMediaFastForward:
		return 57433, 'u', true
	case tea.KeyMediaRewind:
		return 57434, 'u', true
	case tea.KeyMediaNext:
		return 57435, 'u', true
	case tea.KeyMediaPrev:
		return 57436, 'u', true
	case tea.KeyMediaRecord:
		return 57437, 'u', true
	case tea.KeyLowerVol:
		return 57438, 'u', true
	case tea.KeyRaiseVol:
		return 57439, 'u', true
	case tea.KeyMute:
		return 57440, 'u', true
	}
	if code >= tea.KeyF13 && code <= tea.KeyF35 {
		return 57376 + int(code-tea.KeyF13), 'u', true
	}
	return 0, 0, false
}

// kittyModifierKey returns the protocol code of a modifier-only key. These keys
// are only reported under report-all-keys, as the protocol specifies.
func kittyModifierKey(code rune) (int, rune, bool) {
	switch code {
	case tea.KeyLeftShift:
		return 57441, 'u', true
	case tea.KeyLeftCtrl:
		return 57442, 'u', true
	case tea.KeyLeftAlt:
		return 57443, 'u', true
	case tea.KeyLeftSuper:
		return 57444, 'u', true
	case tea.KeyLeftHyper:
		return 57445, 'u', true
	case tea.KeyLeftMeta:
		return 57446, 'u', true
	case tea.KeyRightShift:
		return 57447, 'u', true
	case tea.KeyRightCtrl:
		return 57448, 'u', true
	case tea.KeyRightAlt:
		return 57449, 'u', true
	case tea.KeyRightSuper:
		return 57450, 'u', true
	case tea.KeyRightHyper:
		return 57451, 'u', true
	case tea.KeyRightMeta:
		return 57452, 'u', true
	case tea.KeyIsoLevel3Shift:
		return 57453, 'u', true
	case tea.KeyIsoLevel5Shift:
		return 57454, 'u', true
	}
	return 0, 0, false
}
