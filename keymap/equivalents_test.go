package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// A terminal without the kitty keyboard protocol sends Ctrl+/ as 0x1f, which the
// decoder reads as ctrl+_. Fresh's terminal_key_equivalents make those presses run
// the binding of the chord they stand for.
func TestResolveCtrlSlashFromLegacyTerminal(t *testing.T) {
	m, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := m.Resolve("ctrl+_", keymap.ContextNormal)
	if !ok || b.Action != "toggle_comment" {
		t.Fatalf("Resolve(ctrl+_) = %q, %v; want toggle_comment", b.Action, ok)
	}
	e, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := e.Resolve("ctrl+_", keymap.ContextNormal); !ok || b.Action != "undo" {
		t.Fatalf("emacs Resolve(ctrl+_) = %q, %v; want undo", b.Action, ok)
	}
}

func TestTerminalEquivalentsDoNotOverrideOrInvent(t *testing.T) {
	m, err := keymap.Load([]byte(`{"bindings":[
		{"key":"/","modifiers":["ctrl"],"action":"a","args":{},"when":"normal"},
		{"key":"7","modifiers":["ctrl"],"action":"b","args":{},"when":"normal"},
		{"key":"x","modifiers":["ctrl"],"action":"c","args":{},"when":"normal"},
		{"key":"h","modifiers":["ctrl"],"action":"d","args":{},"when":"normal"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		pressed, action string
		ok              bool
	}{
		{"ctrl+7", "b", true}, // an explicit binding wins over the alias
		{"ctrl+/", "a", true},
		{"ctrl+_", "b", true}, // 0x1f reads as ctrl+7 first
		{"ctrl+x", "c", true},
		{"ctrl+backspace", "d", true},
		{"ctrl+shift+backspace", "", false}, // Ctrl+H aliases Ctrl+Backspace, not the reverse with Shift
		{"alt+_", "", false},                // only Ctrl chords have equivalents
		{"ctrl+y", "", false},
	} {
		b, ok := m.Resolve(tc.pressed, keymap.ContextNormal)
		if ok != tc.ok || b.Action != tc.action {
			t.Errorf("Resolve(%q) = %q, %v; want %q, %v", tc.pressed, b.Action, ok, tc.action, tc.ok)
		}
	}
}
