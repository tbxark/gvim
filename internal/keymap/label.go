package keymap

import (
	"runtime"
	"strings"
)

// SuperName is how a label names the Super modifier: macOS calls the key Cmd.
var SuperName = func() string {
	if runtime.GOOS == "darwin" {
		return "Cmd"
	}
	return "Super"
}()

// keyLabels are the key names a label spells differently from the canonical chord.
var keyLabels = map[string]string{
	"pageup":    "PgUp",
	"pagedown":  "PgDn",
	"escape":    "Esc",
	"backspace": "Backspace",
	"delete":    "Delete",
	"insert":    "Insert",
	"enter":     "Enter",
	"space":     "Space",
	"tab":       "Tab",
	"home":      "Home",
	"end":       "End",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
}

// modifierLabels are the modifier names of a label.
var modifierLabels = map[string]string{"ctrl": "Ctrl", "alt": "Alt", "shift": "Shift"}

// FormatChord writes a chord as the UI shows it: plain text, one style everywhere, such
// as "Ctrl+S", "Ctrl+Shift+Left" or "F12". The steps of a sequence are joined by one
// blank: "Ctrl+X Ctrl+S". Single letters are upper-cased. The chord may be in any
// spelling Normalize reads; one it cannot read is returned as it is, so a label never
// loses text.
func FormatChord(chord string) string {
	steps := strings.Fields(chord)
	if len(steps) == 0 {
		return chord
	}
	out := make([]string, len(steps))
	for i, s := range steps {
		c, err := Normalize(s)
		if err != nil {
			return chord
		}
		out[i] = formatStep(c)
	}
	return strings.Join(out, " ")
}

// formatStep labels one canonical chord.
func formatStep(chord string) string {
	parts := chordParts(chord)
	key := parts[len(parts)-1]
	var b strings.Builder
	for _, m := range parts[:len(parts)-1] {
		name := modifierLabels[m]
		if m == "super" {
			name = SuperName
		}
		b.WriteString(name)
		b.WriteByte('+')
	}
	switch {
	case keyLabels[key] != "":
		b.WriteString(keyLabels[key])
	case len([]rune(key)) == 1:
		b.WriteString(strings.ToUpper(key))
	default:
		b.WriteString(key) // function keys keep their spelling
	}
	return b.String()
}
