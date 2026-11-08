package keybindings

import (
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
)

// NewKeymap returns the tui.Keymap view of *keymap.Map.
func NewKeymap(km *keymap.Map) tui.Keymap { return adapter{km: km} }

// adapter is the tui.Keymap view of *keymap.Map.
type adapter struct {
	km *keymap.Map
}

// Resolve canonicalizes the key press and looks up its binding in the context.
// A chord that does not canonicalize resolves to nothing.
func (k adapter) Resolve(key tui.KeyPress, ctx tui.KeyContext) (tui.Command, bool) {
	chord, err := keymap.Canonical(key.Name, key.Mods)
	if err != nil {
		return tui.Command{}, false
	}
	b, ok := k.km.Resolve(chord, keymapContext(ctx))
	if !ok {
		return tui.Command{}, false
	}
	return tui.Command{Action: b.Action, Args: b.Args}, true
}

// ResolveSequence looks up the key presses typed in order among the multi-key bindings.
// A press that does not canonicalize ends the sequence.
func (k adapter) ResolveSequence(steps []tui.KeyPress, ctx tui.KeyContext) (cmd tui.Command, ok, prefix bool) {
	chords := make([]string, len(steps))
	for i, s := range steps {
		c, err := keymap.Canonical(s.Name, s.Mods)
		if err != nil {
			return tui.Command{}, false, false
		}
		chords[i] = c
	}
	kctx := keymapContext(ctx)
	if len(chords) > 1 {
		if b, found := k.km.ResolveSequence(chords, kctx); found {
			return tui.Command{Action: b.Action, Args: b.Args}, true, false
		}
	}
	return tui.Command{}, false, k.km.HasSequencePrefix(chords, kctx)
}

// Bindings lists every binding of the keymap, in file order, with its canonical
// chord and when-clause. It implements the root's BindingLister port.
func (k adapter) Bindings() []tui.KeyBinding {
	bs := k.km.Bindings()
	out := make([]tui.KeyBinding, len(bs))
	for i, b := range bs {
		out[i] = keyBinding(b)
	}
	return out
}

// Primary returns the binding the UI shows for action, a multi-key sequence when the action
// has no single chord, and false when it has no binding the editor runs.
func (k adapter) Primary(action string) (tui.KeyBinding, bool) {
	b, ok := k.km.Primary(action)
	if !ok {
		return tui.KeyBinding{}, false
	}
	return keyBinding(b), true
}

// Label formats the primary binding of action, as the palette shows it, or "" when the
// action has none. The root reads it for the plugin commands and for gvim.keymap.label.
func (k adapter) Label(action string) string {
	b, ok := k.km.Primary(action)
	if !ok {
		return ""
	}
	return commands.FormatChord(b.Chord)
}

// keyBinding maps a keymap binding to the root's view of it.
func keyBinding(b keymap.Binding) tui.KeyBinding {
	return tui.KeyBinding{Action: b.Action, Chord: b.Chord, Context: string(b.When)}
}

// keymapContext maps a UI key context to its keymap when-clause.
func keymapContext(ctx tui.KeyContext) keymap.Context {
	if ctx == tui.KeyContextFileExplorer {
		return keymap.ContextFileExplorer
	}
	if ctx == tui.KeyContextTerminal {
		return keymap.ContextTerminal
	}
	return keymap.ContextNormal
}
