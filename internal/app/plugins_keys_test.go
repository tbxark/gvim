package app

import (
	"encoding/json"
	"testing"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui"
)

// resolveNormal resolves a single-step chord in the normal context.
func resolveNormal(t *testing.T, k ui.Keymap, name string, mods ...string) string {
	t.Helper()
	cmd, ok := k.Resolve(ui.KeyPress{Name: name, Mods: mods}, ui.KeyContextNormal)
	if !ok {
		return ""
	}
	return cmd.Action
}

// TestUserBindingBeatsPluginBinding: the user's keybindings are the last layer, for the
// startup preset and for the preset a keymap switch chose.
func TestUserBindingBeatsPluginBinding(t *testing.T) {
	cfg := config.Config{Keybindings: json.RawMessage(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"save_all","when":"normal"}]}`)}
	pw := newPluginRuntime(pluginAdapter(), cfg, Options{}, keymap.Default(), func() string { return "" })
	bs := []pluginapi.Binding{
		{Chord: "ctrl+s", Action: "hello.save", Plugin: "hello"},
		{Chord: "ctrl+alt+j", Action: "hello.jump", Plugin: "hello"},
	}
	for _, preset := range []string{"", "emacs", "vscode"} {
		k, err := pw.keys(preset, bs)
		if err != nil {
			t.Fatalf("preset %q: %v", preset, err)
		}
		if got := resolveNormal(t, k, "s", "ctrl"); got != "save_all" {
			t.Errorf("preset %q: ctrl+s = %q, want the user's save_all", preset, got)
		}
		if got := resolveNormal(t, k, "j", "ctrl", "alt"); got != "hello.jump" {
			t.Errorf("preset %q: ctrl+alt+j = %q, want the plugin's binding", preset, got)
		}
	}
}

// TestSwitchKeymapKeepsPluginBindings: the PluginKeys port given a preset name builds that
// preset with the plugin bindings over it, so switch_keybinding_map keeps them.
func TestSwitchKeymapKeepsPluginBindings(t *testing.T) {
	pw := newPluginRuntime(pluginAdapter(), config.Config{}, Options{}, keymap.Default(), func() string { return "" })
	emacs, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	// A chord that the emacs preset binds differently from the startup preset.
	var chord keymap.Binding
	for _, b := range emacs.Bindings() {
		if d, ok := keymap.Default().Resolve(b.Chord, keymap.ContextNormal); b.When == keymap.ContextNormal && b.Sequence == nil && (!ok || d.Action != b.Action) {
			chord = b
			break
		}
	}
	if chord.Chord == "" {
		t.Fatal("no chord tells the emacs preset from the default one")
	}
	bs := []pluginapi.Binding{{Chord: "ctrl+alt+j", Action: "hello.jump", Plugin: "hello"}}
	k, err := pw.keys("emacs", bs)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveNormal(t, k, "j", "ctrl", "alt"); got != "hello.jump" {
		t.Errorf("the plugin binding is lost by the switch: %q", got)
	}
	steps, _, err := chordSteps(chord.Chord)
	if err != nil {
		t.Fatal(err)
	}
	mods := steps[0]["modifiers"].([]string)
	if got := resolveNormal(t, k, steps[0]["key"].(string), mods...); got != chord.Action {
		t.Errorf("%s = %q, want the emacs preset's %q", chord.Chord, got, chord.Action)
	}
	if _, err := pw.keys("no-such-keymap", bs); err == nil {
		t.Error("an unknown preset did not fail")
	}
}
