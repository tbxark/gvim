package keymap_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

func TestFormatChord(t *testing.T) {
	tests := []struct {
		chord string
		want  string
	}{
		{"ctrl+s", "Ctrl+S"},
		{"ctrl+shift+p", "Ctrl+Shift+P"},
		{"shift+ctrl+P", "Ctrl+Shift+P"},
		{"alt+left", "Alt+Left"},
		{"ctrl+pagedown", "Ctrl+PgDn"},
		{"pageup", "PgUp"},
		{"escape", "Esc"},
		{"F12", "F12"},
		{"shift+F3", "Shift+F3"},
		{"ctrl+space", "Ctrl+Space"},
		{"ctrl+/", "Ctrl+/"},
		{"ctrl++", "Ctrl++"},
		{"alt+shift+down", "Alt+Shift+Down"},
		{"ctrl+x ctrl+s", "Ctrl+X Ctrl+S"},
		{"alt+g g", "Alt+G G"},
		{"ctrl+x 2", "Ctrl+X 2"},
		{"super+d", keymap.SuperName + "+D"},
		{"enter", "Enter"},
		{"backspace", "Backspace"},
		{"not a chord ???", "not a chord ???"},
	}
	for _, tc := range tests {
		if got := keymap.FormatChord(tc.chord); got != tc.want {
			t.Errorf("FormatChord(%q) = %q, want %q", tc.chord, got, tc.want)
		}
	}
}

// TestFormatChordNeverShowsRawSpelling: no label of a preset binding contains the raw
// lower-case modifier text ("super+d", "ctrl+s") that PC54 reported.
func TestFormatChordNeverShowsRawSpelling(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		for _, b := range mustPreset(t, name).Bindings() {
			got := keymap.FormatChord(b.Chord)
			for _, raw := range []string{"super+", "ctrl+", "alt+", "shift+"} {
				if strings.Contains(got, raw) {
					t.Errorf("%s: label %q for %q has a raw modifier %q", name, got, b.Chord, raw)
				}
			}
		}
	}
}

// Emacs has no single chord for Save, so its Primary is the sequence, and the label shows it.
func TestPrimaryReturnsSequencesForEmacs(t *testing.T) {
	em := mustPreset(t, "emacs")
	tests := []struct {
		action string
		chord  string
		label  string
	}{
		{"save", "ctrl+x ctrl+s", "Ctrl+X Ctrl+S"},
		{"open", "ctrl+x ctrl+f", "Ctrl+X Ctrl+F"},
		{"quit", "ctrl+x ctrl+c", "Ctrl+X Ctrl+C"},
		{"select_all", "ctrl+x h", "Ctrl+X H"},
		// Fresh's emacs preset binds undo to C-/, C-_ (via the terminal alias) and C--.
		// Primary shows the simplest chord, and C-- sorts before C-/.
		{"undo", "ctrl+-", "Ctrl+-"},
	}
	for _, tc := range tests {
		b, ok := em.Primary(tc.action)
		if !ok || b.Chord != tc.chord {
			t.Errorf("emacs Primary(%s) = %q (%v), want %q", tc.action, b.Chord, ok, tc.chord)
			continue
		}
		if got := keymap.FormatChord(b.Chord); got != tc.label {
			t.Errorf("emacs label of %s = %q, want %q", tc.action, got, tc.label)
		}
	}
}

// A single chord that starts a sequence is a prefix, so it is not the shortcut of its own
// action: emacs inherits Ctrl+X for Cut, but Ctrl+W is the one that runs.
func TestPrimarySkipsPrefixChords(t *testing.T) {
	b, ok := mustPreset(t, "emacs").Primary("cut")
	if !ok || b.Chord != "ctrl+w" {
		t.Fatalf("emacs Primary(cut) = %q (%v), want ctrl+w", b.Chord, ok)
	}
}
