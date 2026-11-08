package keymap

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MenuMnemonic returns the letter a menu's label underlines: the character of an
// Alt+<letter> binding that opens the menu, so that the menus can show it. name is a
// menu title as the menu bar writes it; the match is without case ("File", "file" and
// "FILE" name the same menu).
//
// The lookup mirrors Fresh's KeybindingResolver::find_menu_mnemonic: it searches the
// normal context and then the global one, and it takes the character of a single-key
// binding whose action is "menu_open" and whose "name" argument is the menu's. Only a
// chord with the alt modifier alone counts. Only bindings that are in effect are seen,
// so a child keymap or a user binding that replaces the chord replaces its mnemonic too.
//
// A menu can be shadowed by a context binding: Fresh's normal context binds Alt+L to
// to_lower_case while the global context binds Alt+L to the LSP menu, and Resolve runs
// the normal binding. MenuMnemonic still reports 'l', and the label underlines it, as
// Fresh's lookup does.
func (m *Map) MenuMnemonic(name string) (rune, bool) {
	for _, ctx := range []Context{ContextNormal, ContextGlobal} {
		for i := len(m.bindings) - 1; i >= 0; i-- {
			b := m.bindings[i]
			if b.When != ctx || b.Sequence != nil || b.Action != menuOpenAction {
				continue
			}
			if m.index[lookupKey{ctx, b.Chord}] != i {
				continue
			}
			if !strings.EqualFold(stringArg(b.Args, "name"), name) {
				continue
			}
			if letter, ok := altLetter(b.Chord); ok {
				return letter, true
			}
		}
	}
	return 0, false
}

// menuOpenAction is the action that opens a menu by name, as Fresh's keymaps bind it.
const menuOpenAction = "menu_open"

// stringArg reads a string argument from a binding's args.
func stringArg(args map[string]any, name string) string {
	s, _ := args[name].(string)
	return s
}

// altLetter returns the character of a chord that is exactly Alt plus one character,
// in lower case, as a menu mnemonic is written. Any other chord has no mnemonic.
func altLetter(chord string) (rune, bool) {
	rest, ok := strings.CutPrefix(chord, "alt+")
	if !ok || utf8.RuneCountInString(rest) != 1 {
		return 0, false
	}
	return unicode.ToLower([]rune(rest)[0]), true
}
