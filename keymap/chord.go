package keymap

import (
	"fmt"
	"slices"
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

// terminalEquivalents lists the chords whose binding a terminal delivers as chord,
// most likely first. Fresh registers these as aliases of a binding
// (terminal_key_equivalents): a terminal without the kitty keyboard protocol sends
// Ctrl+/ as 0x1f, Ctrl+Backspace as Ctrl+H, Ctrl+Space as NUL and Ctrl+- as 0x1f
// too. The decoder reads 0x1f as ctrl+_, where Fresh's reads it as ctrl+7, so ctrl+_
// also stands for ctrl+7 and ctrl+/. Only a chord with Ctrl has equivalents, and the
// other modifiers carry over.
func terminalEquivalents(chord string) []string {
	parts := chordParts(chord)
	if len(parts) < 2 || !slices.Contains(parts[:len(parts)-1], "ctrl") {
		return nil
	}
	key, mods := parts[len(parts)-1], parts[:len(parts)-1]
	var keys []string
	switch key {
	case "7":
		keys = []string{"/"}
	case "/":
		keys = []string{"7"}
	case "_":
		keys = []string{"7", "/", "-"}
	case "-":
		keys = []string{"_"}
	case "@":
		keys = []string{"space"}
	case "space":
		keys = []string{"@"}
	case "h":
		keys = []string{"backspace"}
	case "backspace":
		// Fresh aliases Ctrl+H to Ctrl+Backspace for a plain Ctrl+H binding only.
		if len(mods) == 1 {
			keys = []string{"h"}
		}
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.Join(append(slices.Clone(mods), k), "+")
	}
	return out
}
