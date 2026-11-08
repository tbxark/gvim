package ui

import (
	"maps"
	"slices"

	"github.com/tbxark/gvim/internal/editor"
	palmodel "github.com/tbxark/gvim/internal/palette"
)

// KnownActions lists the action names a key binding can name: the editor's actions, the
// actions of the palette's builtin commands, and the ones the root handles itself. The app
// checks the keybindings of the config against it, and the keybinding editor completes
// action names from it. Plugin commands have ids with a dot and are not in the list.
func KnownActions() []string {
	seen := make(map[string]struct{})
	for _, a := range editor.Actions() {
		seen[string(a)] = struct{}{}
	}
	for _, e := range palmodel.Commands(nil) {
		if e.Action != "" {
			seen[e.Action] = struct{}{}
		}
	}
	for _, a := range uiActions {
		seen[a] = struct{}{}
	}
	for a := range reentrantActions {
		seen[a] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen))
}
