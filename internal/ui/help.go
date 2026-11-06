package ui

import "slices"

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
	// Primary is the binding the UI shows for action. ok is false when the action
	// has no single-chord binding.
	Primary(action string) (KeyBinding, bool)
}

// contextOrder is the order the help groups its contexts in. Any other context
// follows these, in name order.
var contextOrder = []string{"normal", "file_explorer", "global"}

// helpRows returns the help rows: one item per action, with its primary chord,
// grouped under the context of that chord. An action with no primary binding is
// grouped under the context of its first binding. The noop action only unbinds a
// key, so it is left out. A nil lister gives no rows.
func helpRows(l BindingLister) []listRow {
	if l == nil {
		return nil
	}
	groups := map[string][]listRow{}
	seen := map[string]bool{}
	for _, b := range l.Bindings() {
		if b.Action == "" || b.Action == "noop" || seen[b.Action] {
			continue
		}
		seen[b.Action] = true
		p, ok := l.Primary(b.Action)
		if !ok {
			p = b
		}
		groups[p.Context] = append(groups[p.Context], listRow{left: b.Action, right: p.Chord})
	}
	var out []listRow
	for _, ctx := range contextsInOrder(groups) {
		out = append(out, listRow{header: true, left: ctx})
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

// keybindingRows returns the keybinding rows: one per binding in effect, with its
// chord and context. A binding that a later one of the same chord and context
// replaces is left out. A nil lister gives no rows.
func keybindingRows(l BindingLister) []listRow {
	if l == nil {
		return nil
	}
	type slot struct{ context, chord string }
	list := l.Bindings()
	last := make(map[slot]int, len(list))
	for i, b := range list {
		last[slot{b.Context, b.Chord}] = i
	}
	var rows []listRow
	for i, b := range list {
		if last[slot{b.Context, b.Chord}] != i {
			continue
		}
		rows = append(rows, listRow{left: b.Action, right: b.Chord + "  " + b.Context})
	}
	return rows
}

// openHelp shows the keymap's shortcuts under title. The help is read-only, so
// choosing a row does nothing. Typing filters the rows.
func (m *Model) openHelp(title string) {
	l, _ := m.keys.(BindingLister)
	v := newListView(title, helpRows(l), m.listChrome(), nil)
	if l == nil {
		v.empty = noBindingsText
	}
	m.pushOverlay(listOverlay{v: v})
}
