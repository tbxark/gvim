package keymap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ActionUnbind is the action of a user binding that removes a keymap binding: the
// chord falls through to whatever else binds it. ActionNoop binds the chord to nothing.
const (
	ActionUnbind = "unbind"
	ActionNoop   = "noop"
)

// UserContext is the when-clause of a user binding that names none, as Fresh's
// keybinding editor documents ("when ... defaults to normal").
const UserContext = ContextNormal

// WithUserBindings applies the keybindings of a config file to m and reports what it
// could not apply. data is the "keybindings" value in either form: the array that
// Fresh documents, [{"key": "s", "modifiers": ["ctrl"], "action": "save", "when": "normal"}],
// or an object {"bindings": [...]}. An entry needs "action" and either "key" or "keys"; its
// when-clause defaults to "normal".
//
// Nothing here stops startup. An entry with an unreadable key or modifier, an unknown
// when-clause or an unknown field is skipped with a warning, as is one whose action
// known rejects (a nil known accepts every action; "noop" and "unbind" are always
// accepted, and so is an action with a "." in it, which is the id of a plugin command).
// Two entries for one chord and context keep the later one. The warnings are one line
// each, in entry order, and name the entry by its position.
//
// The rules follow Fresh. A user binding replaces the keymap's entry for its chord, and a
// global one also replaces it in the contexts that bind the chord. An "unbind" entry removes
// the keymap's binding of its chord and context, so the chord falls through to a global
// binding or to nothing. A single-key binding also frees its key from the keymap
// sequences that start with it, so binding Alt+G beats the Emacs chord Alt+G G. m is
// not changed.
func (m *Map) WithUserBindings(data []byte, known func(action string) bool) (*Map, []string) {
	own, warnings := ParseUserBindings(data, known)
	if len(own) == 0 {
		return m, warnings
	}
	return m.layer(own), warnings
}

// ParseUserBindings reads the keybindings value of a config file into its bindings, with
// the warnings WithUserBindings reports for the entries it leaves out. A value that is not
// a list of bindings gives no bindings and one warning.
func ParseUserBindings(data []byte, known func(action string) bool) ([]Binding, []string) {
	own, _, warnings := ParseUserBindingsRaw(data, known)
	return own, warnings
}

// ParseUserBindingsRaw is ParseUserBindings that also returns the entries it skipped, as the
// JSON the file holds, in file order. A rewrite of the file keeps them, so an entry that cannot
// apply yet (a typo, or an action of a plugin that is not loaded) is not lost on save.
func ParseUserBindingsRaw(data []byte, known func(action string) bool) (own []Binding, skipped []json.RawMessage, warnings []string) {
	raws, err := splitUserBindings(data)
	if err != nil {
		return nil, nil, []string{"keybindings: " + err.Error()}
	}
	index := map[lookupKey]int{} // position in own of the entry that holds a chord
	for i, raw := range raws {
		b, err := userBinding(raw)
		if err == nil && known != nil && !knownUserAction(b.Action, known) {
			err = fmt.Errorf("unknown action %q", b.Action)
		}
		if err == nil && !Consulted(b.When) {
			err = fmt.Errorf("when %q is not used by gvim (keys run in %s)", b.When, strings.Join(ConsultedNames(), ", "))
		}
		if err == nil && IsOverlayContext(b.When) && len(b.Sequence) > 0 {
			err = fmt.Errorf("when %q takes single-key bindings only; sequences are skipped", b.When)
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("keybindings[%d]: %v; skipped", i, err))
			skipped = append(skipped, raw)
			continue
		}
		k := lookupKey{b.When, b.Chord}
		if at, dup := index[k]; dup {
			warnings = append(warnings, fmt.Sprintf("keybindings[%d]: %s in %s is already bound to %q; the later entry wins", i, b.Chord, b.When, own[at].Action))
			own[at] = b
			continue
		}
		index[k] = len(own)
		own = append(own, b)
	}
	return own, skipped, warnings
}

// WithUser returns m with the user bindings own applied over it, the way WithUserBindings
// applies the ones it parses. m is not changed.
func (m *Map) WithUser(own []Binding) *Map {
	if len(own) == 0 {
		return m
	}
	return m.layer(own)
}

// userKeyNames spells the named keys the way Fresh's documentation does, for the entries
// UserEntry writes. Normalize reads them back in any case.
var userKeyNames = map[string]string{
	"backspace": "Backspace", "delete": "Delete", "down": "Down", "end": "End",
	"enter": "Enter", "escape": "Escape", "home": "Home", "insert": "Insert",
	"left": "Left", "pagedown": "PageDown", "pageup": "PageUp", "right": "Right",
	"tab": "Tab", "up": "Up", "space": "Space",
}

// UserKey is one key press of a user binding in the config file: the key and its modifiers.
type UserKey struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers"`
}

// UserSingle is the JSON form of a single-key user binding, with the fields in the order
// Fresh's documentation shows them.
type UserSingle struct {
	Key       string         `json:"key"`
	Modifiers []string       `json:"modifiers"`
	Action    string         `json:"action"`
	When      string         `json:"when"`
	Args      map[string]any `json:"args,omitempty"`
}

// UserSequence is the JSON form of a multi-key user binding.
type UserSequence struct {
	Keys   []UserKey      `json:"keys"`
	Action string         `json:"action"`
	When   string         `json:"when"`
	Args   map[string]any `json:"args,omitempty"`
}

// UserEntry is the JSON form of a user binding, as Fresh's keybinding editor writes it
// into the keybindings list: a UserSingle, {"key": "s", "modifiers": ["ctrl"], "action":
// "save", "when": "normal"}, or a UserSequence with one key press per step.
func UserEntry(b Binding) any {
	step := func(chord string) UserKey {
		parts := chordParts(chord)
		key := parts[len(parts)-1]
		if name, ok := userKeyNames[key]; ok {
			key = name
		}
		return UserKey{Key: key, Modifiers: append([]string{}, parts[:len(parts)-1]...)}
	}
	if b.Sequence == nil {
		k := step(b.Chord)
		return UserSingle{Key: k.Key, Modifiers: k.Modifiers, Action: b.Action, When: string(b.When), Args: b.Args}
	}
	keys := make([]UserKey, len(b.Sequence))
	for i, c := range b.Sequence {
		keys[i] = step(c)
	}
	return UserSequence{Keys: keys, Action: b.Action, When: string(b.When), Args: b.Args}
}

// knownUserAction reports whether a user binding may name action.
func knownUserAction(action string, known func(string) bool) bool {
	return action == ActionNoop || action == ActionUnbind || strings.Contains(action, ".") || known(action)
}

// splitUserBindings reads the two forms of the keybindings value into its entries.
func splitUserBindings(data []byte) ([]json.RawMessage, error) {
	data = bytes.TrimSpace(data)
	var list []json.RawMessage
	switch {
	case len(data) == 0:
		return nil, nil
	case data[0] == '[':
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
	case data[0] == '{':
		var obj struct {
			Bindings []json.RawMessage `json:"bindings"`
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&obj); err != nil {
			return nil, err
		}
		list = obj.Bindings
	default:
		return nil, fmt.Errorf("must be a list of bindings")
	}
	return list, nil
}

// userBinding reads one entry. A field the schema does not have is an error, because a
// misspelt "modifiers" would otherwise bind the bare key.
func userBinding(raw json.RawMessage) (Binding, error) {
	var rb rawBinding
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rb); err != nil {
		return Binding{}, err
	}
	if rb.When == "" {
		rb.When = string(UserContext)
	}
	return rb.binding()
}

// layer returns m with the user bindings own applied over it.
func (m *Map) layer(own []Binding) *Map {
	base := make([]Binding, 0, len(m.bindings))
	for _, b := range m.bindings {
		if !userRemoves(own, b) {
			base = append(base, b)
		}
	}
	var bindings []Binding
	for _, b := range own {
		if b.Action != ActionUnbind {
			bindings = append(bindings, b)
		}
	}
	list := make([]Binding, 0, len(base)+2*len(bindings))
	list = append(list, base...)
	list = append(list, (&Map{bindings: base}).expandGlobal(bindings)...)
	list = append(list, bindings...)
	return newMap(list)
}

// userRemoves reports whether a user entry takes b out of the keymap: an unbind of the
// same chord and context, or a single-key binding of the first key of a keymap sequence
// in that context (or in any context for a global one).
func userRemoves(own []Binding, b Binding) bool {
	for _, u := range own {
		if u.Action == ActionUnbind {
			if u.When == b.When && u.Chord == b.Chord {
				return true
			}
			continue
		}
		if u.Sequence == nil && b.Sequence != nil && b.Sequence[0] == u.Chord &&
			(u.When == b.When || u.When == ContextGlobal) {
			return true
		}
	}
	return false
}

// Replacements lists the bindings of m that the user's keybindings in data take over, one
// line each, in keymap order. A line names the entry and the binding it replaces, so that a
// user who binds a chord the keymap uses can see it. An unbind or noop entry is deliberate,
// and an entry that gives a chord the action it already has changes nothing, so neither is
// listed. The lines do not stop the bindings from applying: WithUserBindings applies them.
// A single key that starts a sequence frees every sequence of that prefix, whatever its action,
// so each of those is listed.
func (m *Map) Replacements(data []byte, known func(string) bool) []string {
	own, _ := ParseUserBindings(data, known)
	var out []string
	seen := make(map[string]bool)
	for _, b := range m.bindings {
		for _, u := range own {
			if u.Action == ActionUnbind || u.Action == ActionNoop || !takesOver(u, b) ||
				(u.Action == b.Action && bindingKeys(u) == bindingKeys(b)) {
				continue
			}
			line := fmt.Sprintf("%s in %s replaces the keymap's %q on %s", bindingKeys(u), u.When, b.Action, bindingKeys(b))
			if !seen[line] {
				seen[line] = true
				out = append(out, line)
			}
			break
		}
	}
	return out
}

// takesOver reports whether user entry u takes the keys of binding b of the keymap: the same
// chord, or for a single key the first key of b's sequence, or the same sequence, in b's
// context or, for a global entry, in any context. It is the rule layer applies.
func takesOver(u, b Binding) bool {
	if u.When != b.When && u.When != ContextGlobal {
		return false
	}
	switch {
	case u.Sequence == nil && b.Sequence == nil:
		return u.Chord == b.Chord
	case u.Sequence == nil:
		return b.Sequence[0] == u.Chord
	default:
		return slices.Equal(u.Sequence, b.Sequence)
	}
}

// bindingKeys spells the keys of a binding: its chord, or its sequence with a space between the
// chords.
func bindingKeys(b Binding) string {
	if b.Sequence == nil {
		return b.Chord
	}
	return strings.Join(b.Sequence, " ")
}
