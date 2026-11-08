package tui

import (
	"slices"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/keymap"
)

// noBindingsText is the text the keymap lists show when the keymap cannot list its
// bindings.
const noBindingsText = "No keymap bindings are available"

// KeyBinding is one keymap binding as the help and keybinding views show it.
// Chord is the canonical chord, such as "ctrl+s"; the steps of a multi-key
// sequence are joined by spaces. Context is the when-clause, such as "normal".
type KeyBinding struct {
	Action  string
	Chord   string
	Context string
}

// BindingLister is the optional view of a Keymap that the help and the keybinding
// views read. The app adapter implements it over the keymap. A keymap without it
// shows an empty list with a note.
type BindingLister interface {
	// Bindings lists every binding in file order, inherited bindings first. A later
	// binding with the same chord and context replaces an earlier one.
	Bindings() []KeyBinding
	// Primary is the binding the UI shows for action: a single chord when it has one,
	// else a multi-key sequence whose steps are joined by blanks in Chord. ok is false
	// when the action has no binding the editor runs.
	Primary(action string) (KeyBinding, bool)
}

// contextOrder is the order the help groups its contexts in. Any other context
// follows these, in name order.
var contextOrder = []string{"normal", "file_explorer", "global"}

// contextTitles name the keymap contexts the way the help shows them. They are the contexts
// gvim resolves keys in (keymap.ConsultedContexts); the overlays answer their keys in code.
var contextTitles = map[string]string{
	"normal": "Editor", "global": "Everywhere", "file_explorer": "File Explorer",
	"terminal": "Terminal",
}

// contextTitle is the heading of a context in the help.
func contextTitle(ctx string) string {
	if t, ok := contextTitles[ctx]; ok {
		return t
	}
	return ctx
}

// helpRows returns the help rows: one item per action, with its primary shortcut as the
// UI labels it everywhere ("Ctrl+S", "Ctrl+X Ctrl+S"), grouped under the context of that
// shortcut. describe names an action for people; nil writes the action id as words. An action
// with no binding that runs is left out, as the menus and the palette show it without a key:
// a binding the preset shadows (Emacs's Ctrl+R is Find Previous, so Replace has no key) is not
// listed under its old chord. Bindings in a context gvim does not resolve are left out too. The
// noop action only unbinds a key, so it is left out. A nil lister gives no rows. The action id
// stays in the row's value, so typing it in the filter finds the row.
func helpRows(l BindingLister, describe func(string) string) []listRow {
	if l == nil {
		return nil
	}
	if describe == nil {
		describe = humanAction
	}
	groups := map[string][]listRow{}
	seen := map[string]bool{}
	for _, b := range l.Bindings() {
		if b.Action == "" || b.Action == "noop" || b.Action == "unbind" || seen[b.Action] {
			continue
		}
		seen[b.Action] = true
		p, ok := l.Primary(b.Action)
		if !ok || !keymap.Consulted(keymap.Context(p.Context)) {
			continue
		}
		groups[p.Context] = append(groups[p.Context], listRow{left: describe(b.Action), right: commands.FormatChord(p.Chord), value: b.Action})
	}
	var out []listRow
	for _, ctx := range contextsInOrder(groups) {
		out = append(out, listRow{header: true, left: contextTitle(ctx)})
		out = append(out, groups[ctx]...)
	}
	return out
}

// contextsInOrder returns the context names of groups: the names in contextOrder
// that have rows, then the others sorted by name.
func contextsInOrder(groups map[string][]listRow) []string {
	var out, rest []string
	for _, c := range contextOrder {
		if _, ok := groups[c]; ok {
			out = append(out, c)
		}
	}
	for c := range groups {
		if !slices.Contains(contextOrder, c) {
			rest = append(rest, c)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// openHelp shows the keymap's shortcuts under title. The help is read-only, so
// choosing a row does nothing and the footer names only the keys that work. Typing filters
// the rows.
func (m *Model) openHelp(title string) {
	l, _ := m.keys.(BindingLister)
	v := newListView(title, helpRows(l, m.actionDescriber()), m.listChrome(), nil)
	if l == nil {
		v.empty = noBindingsText
	}
	m.pushOverlay(listOverlay{v: v})
}
