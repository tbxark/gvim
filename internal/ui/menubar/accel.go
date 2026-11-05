package menubar

import (
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// accel is the accelerator of one label: the ASCII letter that opens a title or
// activates a row, the cell column of that letter in the label, and the letter
// as the label writes it.
type accel struct {
	key  rune   // lowercase letter, 0 when the label has no free letter
	col  int    // cell column of the letter within the label
	text string // the letter as written in the label
}

// pickAccels gives each label the accelerator of pickAccel, in order. A letter
// that an earlier label took is not free for a later one, so the labels of one
// list never share a letter. The title list and each dropdown are separate lists.
func pickAccels(labels []string) []accel {
	taken := make(map[rune]bool, len(labels))
	out := make([]accel, len(labels))
	for i, s := range labels {
		out[i] = pickAccel(s, taken)
		if out[i].key != 0 {
			taken[out[i].key] = true
		}
	}
	return out
}

// pickAccel returns the first ASCII letter of s, scanning left to right, that is
// not in taken. Digits, spaces and punctuation are skipped. A label with no free
// letter gets the zero accel.
func pickAccel(s string, taken map[rune]bool) accel {
	for i, r := range s {
		lower := unicode.ToLower(r)
		if lower < 'a' || lower > 'z' || taken[lower] {
			continue
		}
		return accel{key: lower, col: ansi.StringWidth(s[:i]), text: string(r)}
	}
	return accel{}
}

// letterOf returns the lowercase ASCII letter that a key press types, or 0 when
// the press is not a plain letter. Ctrl and Alt chords are not letters, and
// Shift only changes the case.
func letterOf(msg tea.KeyPressMsg) rune {
	k := msg.Key()
	if k.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
		return 0
	}
	r, size := utf8.DecodeRuneInString(k.Text)
	if size == 0 || size != len(k.Text) || r == utf8.RuneError {
		return 0
	}
	r = unicode.ToLower(r)
	if r < 'a' || r > 'z' {
		return 0
	}
	return r
}
