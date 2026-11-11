// Package textsafe turns untrusted text into paintable text for the TUI's preview
// surfaces. A remote file or an open buffer can contain terminal escapes, C1 controls,
// bidi overrides, zero-width format characters and invalid UTF-8: painted as-is they
// can spoof the surrounding chrome. Clean removes those bytes while keeping readable
// text, tabs and newlines; CleanWithMap also returns the raw-rune to clean-rune map so a
// caller can move match spans/highlights onto the cleaned text.
package textsafe

import (
	"unicode"
	"unicode/utf8"
)

// Clean makes an untrusted line paintable: invalid UTF-8 bytes and the Cc (control) and
// Cf (format) categories are removed; a tab and a newline keep their normal semantics;
// every other readable rune is unchanged.
func Clean(s string) string {
	clean, _ := CleanWithMap(s)
	return clean
}

// CleanWithMap is Clean plus mapping: for every raw rune position (and one past the
// end) it returns the position in the clean text. Invalid UTF-8 bytes are separate raw
// positions with the same clean position as the next rune.
func CleanWithMap(s string) (string, []int) {
	runes := make([]rune, 0, utf8.RuneCountInString(s))
	mapping := make([]int, 0, utf8.RuneCountInString(s)+1)
	clean := 0
	for len(s) > 0 {
		mapping = append(mapping, clean)
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			s = s[1:]
			continue
		}
		s = s[size:]
		if r == '\t' || r == '\n' || (!unicode.Is(unicode.Cc, r) && !unicode.Is(unicode.Cf, r)) {
			runes = append(runes, r)
			clean++
		}
	}
	mapping = append(mapping, clean)
	return string(runes), mapping
}

// MapIndex maps a raw rune position through a CleanWithMap mapping. It clamps out-of-range
// positions so a caller can use it without panicking on a malformed match.
func MapIndex(mapping []int, i int) int {
	if len(mapping) == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= len(mapping) {
		return mapping[len(mapping)-1]
	}
	return mapping[i]
}
