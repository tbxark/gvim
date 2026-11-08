package tui

import (
	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/i18n"
)

// translateCommands resolves the extracted palette titles through i18n for the active
// locale. The translation lives in the UI layer (commands is a TUI-free table and does
// not import i18n); commands.Entry keeps the catalogue key and its English Name, which
// is the explicit fallback when a key has no translation anywhere — the display never
// shows a bare catalogue key. Routing, keybindings and action ids are unchanged, so
// translated titles are searched and dispatched exactly like the English ones.
func translateCommands(entries []commands.Entry) []commands.Entry {
	out := make([]commands.Entry, len(entries))
	copy(out, entries)
	for i := range out {
		if out[i].I18nKey == "" {
			continue
		}
		out[i].Name = i18n.TOr("", out[i].I18nKey, out[i].Name, nil)
	}
	return out
}
