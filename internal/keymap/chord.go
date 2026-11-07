package keymap

import (
	"fmt"
	"strings"
)

// MaxChordSteps is the longest key sequence a chord string may spell: "ctrl+k ctrl+c" is
// two steps.
const MaxChordSteps = 4

// NormalizeChord rewrites a chord string into canonical form. A chord is one step, such as
// "Shift+Ctrl+F", or up to MaxChordSteps steps separated by blanks, such as "g g". Every
// step goes through Normalize; the result joins the canonical steps with one blank.
func NormalizeChord(chord string) (string, error) {
	steps := strings.Fields(chord)
	switch {
	case len(steps) == 0:
		return "", fmt.Errorf("keymap: empty chord")
	case len(steps) > MaxChordSteps:
		return "", fmt.Errorf("keymap: %q has more than %d steps", chord, MaxChordSteps)
	}
	for i, s := range steps {
		c, err := Normalize(s)
		if err != nil {
			return "", err
		}
		steps[i] = c
	}
	return strings.Join(steps, " "), nil
}

// ValidateChord reports why chord cannot be bound, or nil when it can. Plugins bind chords
// they spell themselves, so the layering of plugin bindings checks each one alone before it
// builds the keymap: one bad key then costs one binding, not the whole map.
func ValidateChord(chord string) error {
	_, err := NormalizeChord(chord)
	return err
}
