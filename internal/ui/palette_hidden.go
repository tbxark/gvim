package ui

import (
	"slices"

	palmodel "github.com/tbxark/gvim/internal/palette"
)

// hiddenCommands are the builtin palette rows that can only answer "Not available in gvim":
// Fresh's updater. The palette does not list them, so it offers no command that does nothing.
// The actions still answer when something runs them (see notApplicable).
var hiddenCommands = map[string]bool{"update_fresh": true, "open_update_log": true}

// withoutHidden returns the commands without the hidden rows.
func withoutHidden(in []palmodel.Entry) []palmodel.Entry {
	return slices.DeleteFunc(slices.Clone(in), func(e palmodel.Entry) bool {
		return e.Source == palmodel.SourceBuiltin && hiddenCommands[e.Action]
	})
}
