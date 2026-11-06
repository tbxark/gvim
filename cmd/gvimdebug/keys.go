package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// keyName is a parsed key spec: a key name or one character, with its modifiers.
type keyName struct {
	ctrl, alt, shift bool
	name             string
}

// The keys that send a fixed sequence, by the names the keymap files use.
var (
	plainKeys = map[string]string{
		"enter": "\r", "tab": "\t", "esc": "\x1b", "backspace": "\x7f", "space": " ",
	}
	// arrowKeys end in a letter. Home and End do too, and the modifier goes in the
	// middle of CSI 1 ; m letter.
	arrowKeys = map[string]byte{"up": 'A', "down": 'B', "right": 'C', "left": 'D', "home": 'H', "end": 'F'}
	// tildeKeys end in ~, after a number.
	tildeKeys = map[string]int{
		"insert": 2, "delete": 3, "pgup": 5, "pgdown": 6,
		"f5": 15, "f6": 17, "f7": 18, "f8": 19, "f9": 20, "f10": 21, "f11": 23, "f12": 24,
	}
	// ss3Keys are F1 to F4, which send SS3 P to S without modifiers.
	ss3Keys = map[string]byte{"f1": 'P', "f2": 'Q', "f3": 'R', "f4": 'S'}
)

// encodeKey returns the bytes a terminal sends for spec. The spec is spelled as in
// the keymap files: one character, or the modifiers ctrl, alt and shift joined to a
// key name with '+', as in "ctrl+s" or "shift+down". appCursor selects the cursor
// key sequences of application cursor keys mode, which the child turns on.
func encodeKey(spec string, appCursor bool) ([]byte, error) {
	k, err := parseKey(spec)
	if err != nil {
		return nil, err
	}
	name := strings.ToLower(k.name)
	// The xterm modifier parameter: 1 plus 1 for shift, 2 for alt and 4 for ctrl.
	param := 1
	if k.shift {
		param++
	}
	if k.alt {
		param += 2
	}
	if k.ctrl {
		param += 4
	}
	if c, ok := arrowKeys[name]; ok {
		switch {
		case param > 1:
			return fmt.Appendf(nil, "\x1b[1;%d%c", param, c), nil
		case appCursor:
			return []byte{0x1b, 'O', c}, nil
		default:
			return []byte{0x1b, '[', c}, nil
		}
	}
	if n, ok := tildeKeys[name]; ok {
		if param > 1 {
			return fmt.Appendf(nil, "\x1b[%d;%d~", n, param), nil
		}
		return fmt.Appendf(nil, "\x1b[%d~", n), nil
	}
	if c, ok := ss3Keys[name]; ok {
		if param > 1 {
			return fmt.Appendf(nil, "\x1b[1;%d%c", param, c), nil
		}
		return []byte{0x1b, 'O', c}, nil
	}
	if seq, ok := plainKeys[name]; ok {
		switch {
		case name == "tab" && k.shift:
			return []byte("\x1b[Z"), nil
		case name == "space" && k.ctrl:
			return []byte{0}, nil
		case k.alt:
			return []byte("\x1b" + seq), nil
		}
		return []byte(seq), nil
	}
	return encodeChar(k)
}

// encodeChar returns the bytes of one character with its modifiers.
func encodeChar(k keyName) ([]byte, error) {
	r, size := utf8.DecodeRuneInString(k.name)
	if size == 0 || size != len(k.name) {
		return nil, fmt.Errorf("unknown key %q", k.name)
	}
	var out []byte
	switch {
	case k.ctrl:
		b, ok := controlByte(r)
		if !ok {
			return nil, fmt.Errorf("ctrl has no control code for %q", k.name)
		}
		out = []byte{b}
	case k.shift:
		out = []byte(strings.ToUpper(k.name))
	default:
		out = []byte(k.name)
	}
	if k.alt {
		out = append([]byte{0x1b}, out...)
	}
	return out, nil
}

// controlByte is the control code of ctrl with r: ctrl+a is 0x01, and ctrl+space is NUL.
func controlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r-'a') + 1, true
	case r >= 'A' && r <= 'Z':
		return byte(r-'A') + 1, true
	case r == ' ':
		return 0, true
	}
	return 0, false
}

// parseKey splits a key spec into its modifiers and its key name. A single character
// is a key on its own, so "+" is the plus key, and "ctrl++" is ctrl with the plus key.
func parseKey(spec string) (keyName, error) {
	if utf8.RuneCountInString(spec) == 1 {
		return keyName{name: spec}, nil
	}
	mods, name := "", spec
	switch {
	case strings.HasSuffix(spec, "++"):
		mods, name = strings.TrimSuffix(spec, "++"), "+"
	case strings.Contains(spec, "+"):
		i := strings.LastIndex(spec, "+")
		mods, name = spec[:i], spec[i+1:]
	}
	if name == "" {
		return keyName{}, fmt.Errorf("no key name in %q", spec)
	}
	k := keyName{name: name}
	if mods != "" {
		for _, m := range strings.Split(mods, "+") {
			switch strings.ToLower(m) {
			case "ctrl":
				k.ctrl = true
			case "alt":
				k.alt = true
			case "shift":
				k.shift = true
			default:
				return keyName{}, fmt.Errorf("unknown modifier %q in %q", m, spec)
			}
		}
	}
	return k, nil
}

// mouseEvent is one mouse action at a screen cell. X and Y are zero-based.
type mouseEvent struct {
	Action string `json:"action"` // click, press, release, move, wheel-up or wheel-down
	Button string `json:"button"` // left, middle or right; empty for none
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

// mouseBytes encodes ev in the SGR mouse encoding (mode 1006). gvim turns on mouse
// reporting, so the child reads these reports as mouse events.
func mouseBytes(ev mouseEvent) ([]byte, error) {
	if ev.X < 0 || ev.Y < 0 {
		return nil, fmt.Errorf("mouse position %d,%d is outside the screen", ev.X, ev.Y)
	}
	btn, err := buttonCode(ev.Button)
	if err != nil {
		return nil, err
	}
	x, y := ev.X+1, ev.Y+1
	switch ev.Action {
	case "click":
		return []byte(sgr(btn, x, y, true) + sgr(btn, x, y, false)), nil
	case "press":
		return []byte(sgr(btn, x, y, true)), nil
	case "release":
		return []byte(sgr(btn, x, y, false)), nil
	case "move":
		return []byte(sgr(btn+32, x, y, true)), nil
	case "wheel-up":
		return []byte(sgr(64, x, y, true)), nil
	case "wheel-down":
		return []byte(sgr(65, x, y, true)), nil
	}
	return nil, fmt.Errorf("unknown mouse action %q", ev.Action)
}

// buttonCode is the SGR code of a button. No button is code 3.
func buttonCode(button string) (int, error) {
	switch button {
	case "left":
		return 0, nil
	case "middle":
		return 1, nil
	case "right":
		return 2, nil
	case "":
		return 3, nil
	}
	return 0, fmt.Errorf("unknown mouse button %q", button)
}

// sgr is one SGR mouse report. A press ends in M, and a release ends in m.
func sgr(code, x, y int, press bool) string {
	end := "m"
	if press {
		end = "M"
	}
	return fmt.Sprintf("\x1b[<%d;%d;%d%s", code, x, y, end)
}
