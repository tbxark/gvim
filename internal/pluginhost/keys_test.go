package pluginhost

import (
	"encoding/json"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// resolveNormal resolves a single-step chord in the normal context.
func resolveNormal(t *testing.T, k tui.Keymap, name string, mods ...string) string {
	t.Helper()
	cmd, ok := k.Resolve(tui.KeyPress{Name: name, Mods: mods}, tui.KeyContextNormal)
	if !ok {
		return ""
	}
	return cmd.Action
}

// TestUserBindingBeatsPluginBinding: the user's keybindings are the last layer, for the
// startup preset and for the preset a keymap switch chose.
func TestUserBindingBeatsPluginBinding(t *testing.T) {
	cfg := config.Config{Keybindings: json.RawMessage(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"save_all","when":"normal"}]}`)}
	pw := New(cfg, editor.New(editor.Options{}), Options{Preset: keymap.Default(), Root: func() string { return "" }})
	bs := []pluginapi.Binding{
		{Chord: "ctrl+s", Action: "hello.save", Plugin: "hello"},
		{Chord: "ctrl+alt+j", Action: "hello.jump", Plugin: "hello"},
	}
	for _, preset := range []string{"", "emacs", "vscode"} {
		km, err := pw.Keys(preset, bs)
		if err != nil {
			t.Fatalf("preset %q: %v", preset, err)
		}
		k := keybindings.NewKeymap(km)
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
	pw := New(config.Config{}, editor.New(editor.Options{}), Options{Preset: keymap.Default()})
	emacs, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	// A chord that the emacs preset binds differently from the startup preset. A binding the
	// emacs preset itself replaces (Ctrl+L of default.json) is not in effect, so it is skipped.
	var chord keymap.Binding
	for _, b := range emacs.Bindings() {
		if e, ok := emacs.Resolve(b.Chord, keymap.ContextNormal); !ok || e.Action != b.Action {
			continue
		}
		if d, ok := keymap.Default().Resolve(b.Chord, keymap.ContextNormal); b.When == keymap.ContextNormal && b.Sequence == nil && (!ok || d.Action != b.Action) {
			chord = b
			break
		}
	}
	if chord.Chord == "" {
		t.Fatal("no chord tells the emacs preset from the default one")
	}
	bs := []pluginapi.Binding{{Chord: "ctrl+alt+j", Action: "hello.jump", Plugin: "hello"}}
	km, err := pw.Keys("emacs", bs)
	if err != nil {
		t.Fatal(err)
	}
	k := keybindings.NewKeymap(km)
	if got := resolveNormal(t, k, "j", "ctrl", "alt"); got != "hello.jump" {
		t.Errorf("the plugin binding is lost by the switch: %q", got)
	}
	steps, _, err := keybindings.ChordSteps(chord.Chord)
	if err != nil {
		t.Fatal(err)
	}
	mods := steps[0]["modifiers"].([]string)
	if got := resolveNormal(t, k, steps[0]["key"].(string), mods...); got != chord.Action {
		t.Errorf("%s = %q, want the emacs preset's %q", chord.Chord, got, chord.Action)
	}
	if _, err := pw.Keys("no-such-keymap", bs); err == nil {
		t.Error("an unknown preset did not fail")
	}
}
