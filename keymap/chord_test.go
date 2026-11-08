package keymap_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

func TestNormalizeChord(t *testing.T) {
	tests := []struct {
		chord string
		want  string // "" means an error
	}{
		{"ctrl+s", "ctrl+s"},
		{"Shift+Ctrl+F", "ctrl+shift+f"},
		{"  ctrl+k   ctrl+c ", "ctrl+k ctrl+c"},
		{"g g", "g g"},
		{"F10", "F10"},
		{"ctrl++", "ctrl++"},
		{"a b c d", "a b c d"},
		{"", ""},
		{"   ", ""},
		{"a b c d e", ""},
		{"ctrl+nonsense", ""},
		{"ctrl+", ""},
		{"ctrl+a ctrl+nonsense", ""},
	}
	for _, tt := range tests {
		t.Run(tt.chord, func(t *testing.T) {
			got, err := keymap.NormalizeChord(tt.chord)
			if (err != nil) != (tt.want == "") {
				t.Fatalf("NormalizeChord(%q) = %q, %v", tt.chord, got, err)
			}
			if err == nil && got != tt.want {
				t.Fatalf("NormalizeChord(%q) = %q, want %q", tt.chord, got, tt.want)
			}
			if verr := keymap.ValidateChord(tt.chord); (verr != nil) != (err != nil) {
				t.Fatalf("ValidateChord(%q) = %v, but NormalizeChord gave %v", tt.chord, verr, err)
			}
		})
	}
}

// panicConflicts lists the bindings of m that a panic chord shadows.
//
// The panic chords clear every plugin mode and key capture and are handled before any
// keymap (AMENDMENTS C27), so a binding of one would bind a key that can never reach the
// editor. Chord 1 is ctrl+^, which a legacy terminal also spells ctrl+6; chord 2 is
// ctrl+alt+shift+esc. No preset may bind any spelling of them, in a single key or in a
// sequence.
//
// An enhanced keyboard reports ctrl+^ as ctrl+shift+6 (the same physical keys), and every
// preset binds ctrl+shift+6 to set_bookmark 6, as Fresh does. The ui matches that alias as a
// panic chord only while a plugin holds the keyboard, so the binding keeps working
// otherwise. The check allows that one binding of the alias: another one would be shadowed
// silently.
func panicConflicts(m *keymap.Map) []string {
	forbidden := map[string]bool{"ctrl+^": true, "ctrl+6": true, "ctrl+alt+shift+escape": true}
	const alias = "ctrl+shift+6"
	var out []string
	for _, b := range m.Bindings() {
		for _, s := range append([]string{b.Chord}, b.Sequence...) {
			canon, err := keymap.NormalizeChord(s)
			if err != nil {
				continue // not a key chord of this vocabulary
			}
			for _, step := range strings.Fields(canon) {
				if forbidden[step] || (step == alias && b.Action != "set_bookmark") {
					out = append(out, fmt.Sprintf("%q is bound to %q in the %s context", s, b.Action, b.When))
				}
			}
		}
	}
	return out
}

func TestNoPresetBindsAPanicChord(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		m, err := keymap.ByName(name)
		if err != nil {
			t.Fatalf("ByName(%q): %v", name, err)
		}
		for _, c := range panicConflicts(m) {
			t.Errorf("preset %q: %s", name, c)
		}
	}
}

// TestPanicConflictsSeesABinding: the check above is not vacuous. A keymap that binds
// ctrl+^, ctrl+6, ctrl+alt+shift+esc, or the alias to another action than set_bookmark, in
// any spelling, is reported.
func TestPanicConflictsSeesABinding(t *testing.T) {
	tests := []struct {
		name, binding string
		want          bool
	}{
		{"ctrl caret", `{"key":"^","modifiers":["ctrl"],"action":"save","when":"normal"}`, true},
		{"ctrl 6", `{"key":"6","modifiers":["ctrl"],"action":"save","when":"normal"}`, true},
		{"alias to save", `{"key":"6","modifiers":["shift","ctrl"],"action":"save","when":"normal"}`, true},
		{"all modifiers and escape", `{"key":"escape","modifiers":["shift","alt","ctrl"],"action":"save","when":"normal"}`, true},
		{"second step of a sequence", `{"keys":[{"key":"k","modifiers":["ctrl"]},{"key":"^","modifiers":["ctrl"]}],"action":"save","when":"normal"}`, true},
		{"another chord", `{"key":"7","modifiers":["ctrl"],"action":"save","when":"normal"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := keymap.Default().WithOverrides([]byte(`{"bindings":[` + tt.binding + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(panicConflicts(m)) > 0; got != tt.want {
				t.Fatalf("panicConflicts = %v, want conflict %v", panicConflicts(m), tt.want)
			}
		})
	}
}
