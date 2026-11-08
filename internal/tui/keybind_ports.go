package tui

import "github.com/tbxark/gvim/pluginapi"

// CustomBinding is one entry of the user's keybindings list in the config file: the
// chord (the steps of a sequence joined by blanks), the action, and the when-clause. The
// action is "unbind" for an entry that removes a keymap binding and "noop" for one that
// disables a key. Args are the action's arguments, kept as the file has them.
type CustomBinding struct {
	Chord   string
	Action  string
	Context string
	Args    map[string]any
}

// KeybindingStore is the user's custom keybindings, as the keybinding editor reads and
// writes them. The app implements it over the user config file. Save does disk work, so
// the root calls it from a command, never from Update.
type KeybindingStore interface {
	// Custom lists the user's bindings, in file order.
	Custom() []CustomBinding
	// Preview returns the keymap of the built-in preset called preset with list applied,
	// and writes nothing. The editor shows pending changes through it. It does no IO. A
	// list that cannot be applied is an error.
	Preview(list []CustomBinding, preset string) (Keymap, error)
	// Save writes list as the keybindings of the user config file and returns the keymap
	// of the built-in preset called preset with list applied, which the root uses for the
	// next key press. A list that cannot be applied is an error and nothing is written.
	Save(list []CustomBinding, preset string) (Keymap, error)
}

// PluginPreviewer is the optional part of KeybindingStore that previews a list with the plugin
// bindings layered over the preset, as the keymap applies them: the preset, then the plugins,
// then list. The editor uses it when the store has it, so a plugin's key stays in the table
// while the list changes. A store without it previews over the preset alone.
type PluginPreviewer interface {
	PreviewWithPlugins(list []CustomBinding, preset string, plugins []pluginapi.Binding) (Keymap, error)
}

// escapeTaker is the optional part of an Overlay that gets Esc itself: the root closes an
// overlay on Esc unless TakesEscape reports true.
type escapeTaker interface{ TakesEscape() bool }

// takesEscape reports whether o wants the Esc key passed to Update.
func takesEscape(o Overlay) bool {
	t, ok := o.(escapeTaker)
	return ok && t.TakesEscape()
}
