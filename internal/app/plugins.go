package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/pluginapi"
)

// pluginWiring is what the Lua plugin runtime adds to a session: the runtime, the editor as
// the plugins see it, and the builder of the keymap that holds their bindings.
type pluginWiring struct {
	rt   *plugin.Runtime
	edit *pluginEditor
	keys func(preset string, b []pluginapi.Binding) (tui.Keymap, error)
}

// newPluginRuntime builds the plugin runtime of a session. It starts nothing: the model's
// Init starts the runtime, whose first Work discovers the plugins, and the plugins load
// when that Work comes back. The sources are the plugins embedded in the binary, the user's
// plugin folder and init.lua (config.Dir()), and the --plugin paths. root reports the
// workspace root as it is now, since opening a project changes it. preset is the keymap
// before the user's keybindings, which the plugin bindings are layered on.
//
// With --no-plugins the runtime is still created, with the loader disabled: it discovers
// and loads nothing but the --plugin paths, and the plugin manager can say why.
func newPluginRuntime(a editorAdapter, cfg config.Config, opts Options, preset *keymap.Map, root func() string) *pluginWiring {
	cfgDir, _ := config.Dir()
	ld := &plugin.LoaderOptions{
		Paths:    opts.PluginPaths,
		Disabled: opts.NoPlugins,
		Enabled:  map[string]bool{},
		Settings: map[string]plugin.PluginSettings{},
	}
	if cfgDir != "" {
		ld.UserDir = filepath.Join(cfgDir, "plugins")
		ld.InitFile = filepath.Join(cfgDir, "init.lua")
		ld.StateFile = filepath.Join(cfgDir, "plugin-state.json")
	}
	for name, pc := range cfg.Plugins {
		if pc.Enabled != nil {
			ld.Enabled[name] = *pc.Enabled
		}
		if pc.Settings != nil || pc.ProjectSettings != nil {
			ld.Settings[name] = plugin.PluginSettings{User: pc.Settings, Project: pc.ProjectSettings}
		}
	}
	osys := newPluginOS(pluginDirs(cfgDir, root))
	return &pluginWiring{
		rt: plugin.New(plugin.Options{
			Timeout:  cfg.PluginTimeout,
			OS:       osys,
			Services: pluginServices{},
			Version:  gvimVersion(),
			LogSink:  opts.pluginLog,
			Loader:   ld,
		}),
		edit: newPluginEditor(a),
		keys: func(name string, bs []pluginapi.Binding) (tui.Keymap, error) {
			base := preset
			if name != "" {
				// A keymap switch: the plugin bindings go over the preset it chose.
				b, err := keymapFor(name)
				if err != nil {
					return nil, err
				}
				base = b
			}
			km, err := pluginKeymap(base, cfg, bs)
			if km == nil {
				return nil, err
			}
			return keymapAdapter{km: km}, err
		},
	}
}

// pluginDirs reports the directories gvim.dirs gives the plugins. It is evaluated at every
// call, because the workspace root changes when a project is opened.
func pluginDirs(cfgDir string, root func() string) func() plugin.Dirs {
	home, _ := os.UserHomeDir()
	return func() plugin.Dirs {
		cwd, _ := os.Getwd()
		return plugin.Dirs{Config: cfgDir, Home: home, Temp: os.TempDir(), Cwd: cwd, Root: root()}
	}
}

// gvimVersion is the module version of the binary, or "dev" for a build from a work tree.
func gvimVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// pluginKeymap layers the plugins' key bindings into a keymap: the preset, then the
// bindings, then the user's own keybindings, so the user always wins. Plugin bindings
// are bindings of the normal context; the ui checks their when-clauses when the key is
// pressed.
//
// When two bindings take the same chord the first in load order wins, and the loser is
// reported. A binding that is not a valid chord is left out. If the keymap rejects the
// bindings together, each plugin's bindings are tried alone and only the plugins whose
// bindings fail are left out. The error lists everything that was left out; the map is
// still returned with the rest, so one bad binding does not unbind the others. If the
// user's own layer cannot be applied there is no map.
func pluginKeymap(preset *keymap.Map, cfg config.Config, bs []pluginapi.Binding) (*keymap.Map, error) {
	km, err := pluginLayer(preset, bs)
	if km == nil {
		return nil, err
	}
	return keymapWithOverrides(km, cfg), err
}

// pluginLayer is the preset with the plugins' key bindings layered over it, and without the
// user's keybindings. The keybinding editor previews its pending list over this layer, so a
// plugin's binding stays in the table while the list is being changed. pluginKeymap is this
// layer plus the saved user keybindings.
func pluginLayer(preset *keymap.Map, bs []pluginapi.Binding) (*keymap.Map, error) {
	var dropped []string
	var order []string // plugins, in the order of their first binding
	groups := map[string][]bindingEntry{}
	owner := map[string]string{} // canonical chord -> plugin that holds it
	for _, b := range bs {
		steps, canon, err := chordSteps(b.Chord)
		if err != nil {
			dropped = append(dropped, fmt.Sprintf("%s: %q: %v", b.Plugin, b.Chord, err))
			continue
		}
		if first, taken := owner[canon]; taken {
			dropped = append(dropped, fmt.Sprintf("plugin '%s': key %s already bound by plugin '%s'", b.Plugin, canon, first))
			continue
		}
		owner[canon] = b.Plugin
		if _, seen := groups[b.Plugin]; !seen {
			order = append(order, b.Plugin)
		}
		groups[b.Plugin] = append(groups[b.Plugin], bindingEntry{
			Keys: steps, Action: b.Action, Args: b.Args, When: string(keymap.ContextNormal),
		})
	}
	km := preset
	var all []bindingEntry
	for _, name := range order {
		all = append(all, groups[name]...)
	}
	if len(all) > 0 {
		var err error
		if km, err = withBindings(preset, all); err != nil {
			// Find the plugins that fail on their own, then build again without them.
			all = all[:0]
			for _, name := range order {
				if _, err := withBindings(preset, groups[name]); err != nil {
					dropped = append(dropped, fmt.Sprintf("plugin '%s': %v", name, err))
					continue
				}
				all = append(all, groups[name]...)
			}
			km = preset
			if len(all) > 0 {
				if km, err = withBindings(preset, all); err != nil {
					return nil, fmt.Errorf("plugin key bindings: %w", err)
				}
			}
		}
	}
	if len(dropped) > 0 {
		return km, fmt.Errorf("plugin key bindings ignored: %s", strings.Join(dropped, "; "))
	}
	return km, nil
}

// bindingEntry is a binding in the schema of the keymap override file.
type bindingEntry struct {
	Keys   []map[string]any `json:"keys"`
	Action string           `json:"action"`
	Args   map[string]any   `json:"args,omitempty"`
	When   string           `json:"when"`
}

// withBindings is the keymap m with the bindings es layered over it.
func withBindings(m *keymap.Map, es []bindingEntry) (*keymap.Map, error) {
	data, err := json.Marshal(map[string]any{"bindings": es})
	if err != nil {
		return nil, err
	}
	return m.WithOverrides(data)
}

// chordSteps splits a chord of the plugin API ("ctrl+shift+f", or "g g" for a sequence)
// into the key and modifiers of each step in the keymap file schema, and returns the
// canonical spelling of the whole chord.
func chordSteps(chord string) (steps []map[string]any, canon string, err error) {
	if err := keymap.ValidateChord(chord); err != nil {
		return nil, "", err
	}
	fields := strings.Fields(chord)
	if len(fields) == 0 {
		return nil, "", errors.New("empty chord")
	}
	if len(fields) > 4 {
		return nil, "", errors.New("a sequence has at most 4 steps")
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		c, err := keymap.Normalize(f)
		if err != nil {
			return nil, "", err
		}
		parts[i] = c
		key, mods := splitChord(c)
		steps = append(steps, map[string]any{"key": key, "modifiers": mods})
	}
	return steps, strings.Join(parts, " "), nil
}

// splitChord splits a canonical chord into its key and its modifiers. The plus key is "+"
// alone or after the modifiers, as in "ctrl++".
func splitChord(c string) (key string, mods []string) {
	switch {
	case c == "+":
		return "+", []string{}
	case strings.HasSuffix(c, "++"):
		return "+", strings.Split(strings.TrimSuffix(c, "++"), "+")
	}
	parts := strings.Split(c, "+")
	return parts[len(parts)-1], append([]string{}, parts[:len(parts)-1]...)
}
