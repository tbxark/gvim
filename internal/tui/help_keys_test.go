package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// presetLister is the BindingLister the app adapter is, over a built-in keymap. The ui package
// cannot import the app adapter, so the test reads the keymap directly, as the adapter does.
type presetLister struct{ m *keymap.Map }

func (l presetLister) Bindings() []KeyBinding {
	var out []KeyBinding
	for _, b := range l.m.Bindings() {
		out = append(out, KeyBinding{Action: b.Action, Chord: b.Chord, Context: string(b.When)})
	}
	return out
}

func (l presetLister) Primary(action string) (KeyBinding, bool) {
	b, ok := l.m.Primary(action)
	if !ok {
		return KeyBinding{}, false
	}
	return KeyBinding{Action: b.Action, Chord: b.Chord, Context: string(b.When)}, true
}

// resolveAs looks up chord as the root does in the context a binding names: a single chord
// through Resolve, a multi-key chord through ResolveSequence.
func resolveAs(m *keymap.Map, chord string, when string) (keymap.Binding, bool) {
	ctx := keymap.ContextNormal
	switch when {
	case string(keymap.ContextFileExplorer):
		ctx = keymap.ContextFileExplorer
	case string(keymap.ContextTerminal):
		ctx = keymap.ContextTerminal
	}
	if steps := strings.Fields(chord); len(steps) > 1 {
		return m.ResolveSequence(steps, ctx)
	}
	return m.Resolve(chord, ctx)
}

// TestHelpChordsRunTheirAction checks every row of the help for every built-in keymap: the key
// the row shows is the one the root runs for that action, so the help cannot contradict the
// menus or the palette. The old help showed Emacs's Ctrl+R as Replace, which runs Find Previous.
func TestHelpChordsRunTheirAction(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		m, err := keymap.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		l := presetLister{m}
		rows := 0
		for _, r := range helpRows(l, nil) {
			if r.Header {
				continue
			}
			rows++
			p, ok := l.Primary(r.Value)
			if !ok {
				t.Errorf("%s: help row %q has no binding that runs", name, r.Value)
				continue
			}
			if want := keymap.FormatChord(p.Chord); r.Right != want {
				t.Errorf("%s: help row %q shows %q, want %q", name, r.Value, r.Right, want)
			}
			b, ok := resolveAs(m, p.Chord, p.Context)
			if !ok || b.Action != r.Value {
				t.Errorf("%s: help shows %s for %s, but the key runs %q (%v)", name, p.Chord, r.Value, b.Action, ok)
			}
		}
		if rows < 60 {
			t.Errorf("%s: the help lists %d actions, want the common set", name, rows)
		}
	}
}

// TestHelpLeavesOutActionsThatTheirPresetShadows: an action whose only binding is shadowed by
// the preset has no key, so the help does not list it with the shadowing key. Each of these is
// the emacs preset's shadowing of a default binding, and the menus show no key for them either.
func TestHelpLeavesOutActionsThatTheirPresetShadows(t *testing.T) {
	m, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, r := range helpRows(presetLister{m}, nil) {
		if !r.Header {
			listed[r.Value] = true
		}
	}
	for _, action := range []string{"replace", "focus_file_explorer", "select_line_start", "expand_line_selection"} {
		if listed[action] {
			t.Errorf("emacs help lists %s, which has no binding that runs in emacs", action)
		}
	}
}
