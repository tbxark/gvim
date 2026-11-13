package keybindings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// PluginKeymap layers the plugins' key bindings into a keymap: the preset, then the
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
func PluginKeymap(preset *keymap.Map, cfg config.Config, bs []pluginapi.Binding) (*keymap.Map, error) {
	km, err := PluginLayer(preset, bs)
	if km == nil {
		return nil, err
	}
	return ApplyOverrides(km, cfg), err
}

// PluginLayer is the preset with the plugins' key bindings layered over it, and without the
// user's keybindings. The keybinding editor previews its pending list over this layer, so a
// plugin's binding stays in the table while the list is being changed. PluginKeymap is this
// layer plus the saved user keybindings.
func PluginLayer(preset *keymap.Map, bs []pluginapi.Binding) (*keymap.Map, error) {
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
	// ValidateChord has ruled out an empty chord and one of too many steps.
	fields := strings.Fields(chord)
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

// ChordSteps splits a chord of the plugin API ("ctrl+shift+f", or "g g" for a sequence)
// into the key and modifiers of each step in the keymap file schema, and returns the
// canonical spelling of the whole chord.
func ChordSteps(chord string) (steps []map[string]any, canon string, err error) {
	return chordSteps(chord)
}
