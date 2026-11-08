package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui"
)

func pluginAdapter() editorAdapter {
	return editorAdapter{ed: editor.New(editor.Options{}), spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
}

// TestNewPluginRuntimeIsCreatedForNoPlugins: --no-plugins still creates the runtime, with
// its loader disabled, so the manager can say why nothing is loaded (FINAL 4.4).
func TestNewPluginRuntimeIsCreatedForNoPlugins(t *testing.T) {
	isolateConfig(t)
	tests := []struct {
		name     string
		opts     Options
		disabled bool
	}{
		{"default", Options{}, false},
		{"--no-plugins", Options{NoPlugins: true}, true},
		{"--no-plugins --plugin", Options{NoPlugins: true, PluginPaths: []string{"x.lua"}}, true},
		{"--plugin", Options{PluginPaths: []string{"x.lua"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw := newPluginRuntime(pluginAdapter(), config.Config{}, tt.opts, keymap.Default(), func() string { return "" })
			if pw == nil || pw.rt == nil || pw.edit == nil || pw.keys == nil {
				t.Fatalf("wiring = %+v, want the runtime, the editor and the key builder", pw)
			}
			_, notices := pw.rt.Control(pluginapi.Control{Op: "reload_all"})
			got := len(notices) == 1 && strings.Contains(notices[0].Text, "disabled by --no-plugins")
			if got != tt.disabled {
				t.Fatalf("reload_all notices = %+v, disabled = %v, want %v", notices, got, tt.disabled)
			}
		})
	}
}

// TestBuildRuntimeWiresThePluginPorts: the model gets the runtime, the editor port and the
// key builder, with plugins on or off.
func TestBuildRuntimeWiresThePluginPorts(t *testing.T) {
	isolateConfig(t)
	var got ui.RootPorts
	prev := newUI
	t.Cleanup(func() { newUI = prev })
	newUI = func(d ui.Deps) *ui.Model {
		got = d.Root
		return prev(d)
	}
	for _, tt := range []struct {
		name string
		opts Options
	}{{"on", Options{Root: t.TempDir()}}, {"--no-plugins", Options{Root: t.TempDir(), NoPlugins: true}}} {
		t.Run(tt.name, func(t *testing.T) {
			got = ui.RootPorts{}
			rt, err := buildRuntime(t.Context(), tt.opts, false)
			must(t, err)
			defer rt.close()
			if got.Plugins == nil || got.PluginEditor == nil || got.PluginKeys == nil {
				t.Fatalf("plugin ports = %+v, want all set", got)
			}
		})
	}
}

// TestPluginConfigReachesTheLoader checks that plugins.<name>.enabled and .settings of the
// config files, and plugin_timeout_ms, reach the runtime.
func TestPluginConfigReachesTheLoader(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{
  "plugin_timeout_ms": 80,
  "plugins": {"cfg": {"settings": {"greeting": "hi from config", "count": 3}}, "off": {"enabled": false}}
}`)
	writeUserFile(t, "plugins/cfg.lua", `
local gvim = require "gvim"
local s = gvim.settings.define{
  greeting = { type = "string", default = "default" },
  count = { type = "integer", default = 1 },
}
gvim.command.register{ name = "show", title = "Show Settings", run = function()
  gvim.notify(s.greeting .. " " .. s.count)
end }
`)
	writeUserFile(t, "plugins/off.lua", `error("must not load")`)
	s := newSession(t, Options{})
	s.waitStatus("cfg", "loaded")
	s.waitStatus("off", "disabled")
	s.runCommand("Show Settings")
	if got := s.statusRow(); !strings.Contains(got, "hi from config 3") {
		t.Fatalf("status row = %q, want the settings of the config", got)
	}
}

func TestPluginKeymapLayers(t *testing.T) {
	cfg := config.Config{}
	userSaves := config.Config{Keybindings: json.RawMessage(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"save_all","when":"normal"}]}`)}
	resolve := func(km *keymap.Map, chord string) string {
		t.Helper()
		b, ok := km.Resolve(chord, keymap.ContextNormal)
		if !ok {
			return ""
		}
		return b.Action
	}
	preset := keymap.Default()
	if got := resolve(preset, "ctrl+s"); got != "save" {
		t.Fatalf("preset ctrl+s = %q", got)
	}

	t.Run("a plugin binding beats the preset", func(t *testing.T) {
		km, err := pluginKeymap(preset, cfg, []pluginapi.Binding{{Chord: "ctrl+s", Action: "hello.hello", Plugin: "hello"}})
		must(t, err)
		if got := resolve(km, "ctrl+s"); got != "hello.hello" {
			t.Fatalf("ctrl+s = %q, want the plugin command", got)
		}
		if got := resolve(preset, "ctrl+s"); got != "save" {
			t.Fatalf("the preset was changed: ctrl+s = %q", got)
		}
	})
	t.Run("the user's keybindings beat the plugin", func(t *testing.T) {
		km, err := pluginKeymap(preset, userSaves, []pluginapi.Binding{{Chord: "ctrl+s", Action: "hello.hello", Plugin: "hello"}})
		must(t, err)
		if got := resolve(km, "ctrl+s"); got != "save_all" {
			t.Fatalf("ctrl+s = %q, want the user's binding", got)
		}
	})
	t.Run("a new chord, a modifier combination, the plus key and a sequence", func(t *testing.T) {
		km, err := pluginKeymap(preset, cfg, []pluginapi.Binding{
			{Chord: "ctrl+alt+shift+j", Action: "p.one", Plugin: "p"},
			{Chord: "Alt+F9", Action: "p.two", Plugin: "p"},
			{Chord: "ctrl++", Action: "p.plus", Plugin: "p"},
			{Chord: "ctrl+k ctrl+j", Action: "p.seq", Plugin: "p", Args: map[string]any{"n": float64(2)}},
		})
		must(t, err)
		for chord, want := range map[string]string{"ctrl+alt+shift+j": "p.one", "alt+F9": "p.two", "ctrl++": "p.plus"} {
			if got := resolve(km, chord); got != want {
				t.Errorf("%s = %q, want %q", chord, got, want)
			}
		}
		b, ok := km.ResolveSequence([]string{"ctrl+k", "ctrl+j"}, keymap.ContextNormal)
		if !ok || b.Action != "p.seq" || b.Args["n"] != float64(2) {
			t.Errorf("sequence = %+v, %v", b, ok)
		}
	})
	t.Run("a bad chord is dropped and named, the others stay", func(t *testing.T) {
		km, err := pluginKeymap(preset, cfg, []pluginapi.Binding{
			{Chord: "ctrl+nonsense", Action: "p.bad", Plugin: "p"}, {Chord: "", Action: "p.empty", Plugin: "q"}, {Chord: "ctrl+alt+j", Action: "p.good", Plugin: "p"},
		})
		if err == nil || !strings.Contains(err.Error(), "ctrl+nonsense") || !strings.Contains(err.Error(), "q:") {
			t.Fatalf("error = %v, want it to name the bad chords", err)
		}
		if km == nil || resolve(km, "ctrl+alt+j") != "p.good" {
			t.Fatalf("the good binding is lost: %v", km)
		}
	})
	t.Run("no plugin bindings: the preset with the user's layer", func(t *testing.T) {
		km, err := pluginKeymap(preset, userSaves, nil)
		must(t, err)
		if got := resolve(km, "ctrl+s"); got != "save_all" {
			t.Fatalf("ctrl+s = %q", got)
		}
	})
	t.Run("a broken user entry is left out and the rest applies", func(t *testing.T) {
		bad := config.Config{Keybindings: json.RawMessage(`[{"key":"s","modifiers":["bogus"],"action":"x"},{"key":"j","modifiers":["alt"],"action":"save_all"}]`)}
		km, err := pluginKeymap(preset, bad, nil)
		must(t, err)
		if got := resolve(km, "alt+j"); got != "save_all" {
			t.Fatalf("alt+j = %q, want the good entry", got)
		}
		if got := resolve(km, "ctrl+s"); got != "save" {
			t.Fatalf("ctrl+s = %q, want the preset's save", got)
		}
	})
}

func TestSplitChord(t *testing.T) {
	tests := []struct {
		chord, key string
		mods       []string
	}{
		{"a", "a", []string{}},
		{"ctrl+s", "s", []string{"ctrl"}},
		{"ctrl+alt+shift+F5", "F5", []string{"ctrl", "alt", "shift"}},
		{"+", "+", []string{}},
		{"ctrl++", "+", []string{"ctrl"}},
		{"ctrl+alt++", "+", []string{"ctrl", "alt"}},
	}
	for _, tt := range tests {
		key, mods := splitChord(tt.chord)
		if key != tt.key || !slices.Equal(mods, tt.mods) {
			t.Errorf("splitChord(%q) = %q, %v, want %q, %v", tt.chord, key, mods, tt.key, tt.mods)
		}
	}
}

func TestKeymapAdapterLabel(t *testing.T) {
	k := keymapAdapter{km: keymap.Default()}
	if got := k.Label("save"); got == "" || !strings.HasSuffix(got, "S") {
		t.Errorf("Label(save) = %q, want a formatted ctrl+s", got)
	}
	if got := k.Label("no_such_action"); got != "" {
		t.Errorf("Label of an unbound action = %q", got)
	}
	// A plugin command bound through the layered keymap gets a label.
	km, err := pluginKeymap(keymap.Default(), config.Config{}, []pluginapi.Binding{{Chord: "ctrl+alt+j", Action: "hello.hello", Plugin: "hello"}})
	must(t, err)
	if got := (keymapAdapter{km: km}).Label("hello.hello"); got == "" {
		t.Error("Label of a plugin command bound by a plugin is empty")
	}
}

// TestPluginKeysPortBuildsALayeredKeymap: the RootPorts.PluginKeys function returns a
// keymap the root can resolve with.
func TestPluginKeysPortBuildsALayeredKeymap(t *testing.T) {
	pw := newPluginRuntime(pluginAdapter(), config.Config{}, Options{}, keymap.Default(), func() string { return "" })
	k, err := pw.keys("", []pluginapi.Binding{{Chord: "ctrl+alt+j", Action: "hello.hello", Plugin: "hello"}})
	must(t, err)
	cmd, ok := k.Resolve(ui.KeyPress{Name: "j", Mods: []string{"ctrl", "alt"}}, ui.KeyContextNormal)
	if !ok || cmd.Action != "hello.hello" {
		t.Fatalf("resolved %+v, %v", cmd, ok)
	}
	if cmd, ok := k.Resolve(ui.KeyPress{Name: "s", Mods: []string{"ctrl"}}, ui.KeyContextNormal); !ok || cmd.Action != "save" {
		t.Fatalf("the preset bindings are lost: %+v, %v", cmd, ok)
	}
	// The error of a partial failure comes with a usable keymap; a total one with none.
	k, err = pw.keys("", []pluginapi.Binding{{Chord: "bogus+", Action: "x", Plugin: "p"}})
	if err == nil || k == nil {
		t.Fatalf("keys = %v, %v, want the keymap and the error about the bad chord", k, err)
	}
}

func TestProjectOpenerTracksTheRoot(t *testing.T) {
	isolateConfig(t)
	first, second := t.TempDir(), t.TempDir()
	p := newProjectOpener(t.Context(), nil, first, nil)
	if got := p.rootDir(); got != first {
		t.Fatalf("rootDir = %q, want %q", got, first)
	}
	if _, err := p.Open(second); err != nil {
		t.Fatal(err)
	}
	defer p.close()
	want, _ := filepath.EvalSymlinks(second)
	if got, _ := filepath.EvalSymlinks(p.rootDir()); got != want {
		t.Fatalf("rootDir after Open = %q, want %q", p.rootDir(), second)
	}
}

// TestPluginDirsFollowTheWorkspaceRoot: the directories the plugins get report the current
// root, which a project switch changes, and the config directory and the home.
func TestPluginDirsFollowTheWorkspaceRoot(t *testing.T) {
	isolateConfig(t)
	root := "/first"
	dirs := pluginDirs("/cfg/gvim", func() string { return root })
	d := dirs()
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	if d.Root != "/first" || d.Config != "/cfg/gvim" || d.Home != home || d.Cwd != cwd || d.Temp != os.TempDir() {
		t.Fatalf("dirs = %+v", d)
	}
	root = "/second"
	if got := dirs().Root; got != "/second" {
		t.Fatalf("Root after the switch = %q, want /second", got)
	}
}

// TestOneBadBindingKeepsOthers: the keymap rejects the bindings of one plugin (an empty
// action, an argument JSON cannot hold), and only that plugin's bindings are dropped.
func TestOneBadBindingKeepsOthers(t *testing.T) {
	preset := keymap.Default()
	tests := []struct {
		name string
		bad  pluginapi.Binding
	}{
		{"empty action", pluginapi.Binding{Chord: "ctrl+alt+m", Action: "", Plugin: "c"}},
		{"unmarshalable args", pluginapi.Binding{Chord: "ctrl+alt+m", Action: "c.x", Plugin: "c", Args: map[string]any{"f": func() {}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km, err := pluginKeymap(preset, config.Config{}, []pluginapi.Binding{
				{Chord: "ctrl+alt+j", Action: "a.one", Plugin: "a"},
				tt.bad,
				{Chord: "ctrl+alt+n", Action: "d.x", Plugin: "d"},
				{Chord: "ctrl+alt+o", Action: "c.ok", Plugin: "c"}, // a second binding of the dropped plugin
			})
			if err == nil || !strings.Contains(err.Error(), "plugin 'c'") {
				t.Fatalf("error = %v, want plugin 'c' named", err)
			}
			if km == nil {
				t.Fatal("no keymap: one plugin's bad binding unbound the others")
			}
			for chord, want := range map[string]string{"ctrl+alt+j": "a.one", "ctrl+alt+n": "d.x", "ctrl+alt+m": "", "ctrl+alt+o": ""} {
				got := ""
				if b, ok := km.Resolve(chord, keymap.ContextNormal); ok {
					got = b.Action
				}
				if got != want {
					t.Errorf("%s = %q, want %q", chord, got, want)
				}
			}
		})
	}
}

// TestPluginBindingConflictDropsOnlyLoser: the first plugin in load order keeps a chord,
// the loser is named, and the loser's other bindings stay.
func TestPluginBindingConflictDropsOnlyLoser(t *testing.T) {
	km, err := pluginKeymap(keymap.Default(), config.Config{}, []pluginapi.Binding{
		{Chord: "ctrl+alt+j", Action: "a.one", Plugin: "a"}, {Chord: "Alt+Ctrl+J", Action: "b.one", Plugin: "b"},
		{Chord: "ctrl+alt+k", Action: "b.two", Plugin: "b"},
	})
	if err == nil || !strings.Contains(err.Error(), "plugin 'b': key ctrl+alt+j already bound by plugin 'a'") {
		t.Fatalf("error = %v, want the loser reported", err)
	}
	if km == nil {
		t.Fatal("no keymap")
	}
	for chord, want := range map[string]string{"ctrl+alt+j": "a.one", "ctrl+alt+k": "b.two"} {
		if b, ok := km.Resolve(chord, keymap.ContextNormal); !ok || b.Action != want {
			t.Errorf("%s = %+v, %v, want %q", chord, b, ok, want)
		}
	}
}
