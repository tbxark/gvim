package tui

import (
	"slices"

	commands "github.com/tbxark/gvim/commands"
)

// hiddenCommands are the builtin palette rows that can only answer "Not available in gvim":
// Fresh's updater. The palette does not list them, so it offers no command that does nothing.
// The actions still answer when something runs them (see notApplicable).
var hiddenCommands = map[string]bool{"update_fresh": true, "open_update_log": true}

// hiddenCommandRows returns the hidden updater rows, so the model can offer them when
// an update command is configured. The other hidden rows have no substitute.
func hiddenCommandRows(in []commands.Entry) []commands.Entry {
	var out []commands.Entry
	for _, e := range in {
		if e.Source == commands.SourceBuiltin && hiddenCommands[e.Action] {
			out = append(out, e)
		}
	}
	return out
}

// withoutHidden returns the commands without the hidden rows.
func withoutHidden(in []commands.Entry) []commands.Entry {
	return slices.DeleteFunc(slices.Clone(in), func(e commands.Entry) bool {
		return e.Source == commands.SourceBuiltin && hiddenCommands[e.Action]
	})
}
