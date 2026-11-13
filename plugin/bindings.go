package plugin

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// Key bindings of a plugin. The set is whole-set replace on the host side
// (Host.SetBindings), so each plugin keeps its list here and republishes it after every
// change. command.register{keys=} is sugar for a binding of the new command;
// gvim.keymap.set/del use the same set.
//
// Only bindings of the normal editing context live here: a binding with a mode never
// enters the keymap and belongs to the ui mode table (ModeInfo).

const (
	maxChordSteps = 4  // "ctrl+k ctrl+c" is two steps
	maxChordStep  = 32 // bytes of one step
	maxChordText  = 128
)

// hiddenCommandPrefix starts the name of a command the runtime registers for a Lua
// function bound to a key. It is not a valid command name ([a-z]...), so no plugin can
// register or unregister one directly.
const hiddenCommandPrefix = "__fn"

// bindingSet is the key bindings one plugin has registered, in registration order.
type bindingSet struct {
	p    *plugin
	list []pluginapi.Binding
	at   map[string]int // bindingKey of a chord -> index in list
}

// bindingKey is the identity of a chord: the spelling is the keymap's business (it folds
// case and the order of modifiers), the runtime only treats spellings that differ by case
// as the same key.
func bindingKey(chord string) string { return strings.ToLower(chord) }

// checkChord trims a chord and collapses the spaces between the steps of a sequence. It
// rejects what can never be a key: empty or oversized text, control characters, more
// than maxChordSteps steps. Whether a step names a real key is the keymap's decision
// (Services.ValidateChord, through plugin.checkChord); this keeps garbage out of the
// registry and the host.
func checkChord(chord string) (string, error) {
	fields := strings.Fields(chord)
	if len(fields) == 0 {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "empty key chord")
	}
	if len(fields) > maxChordSteps {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "key sequence %q has more than %d steps", clipMsg(chord, 40), maxChordSteps)
	}
	for _, f := range fields {
		if len(f) > maxChordStep {
			return "", pluginapi.Errf(pluginapi.CodeInvalid, "key %q is too long", clipMsg(f, 40))
		}
		for _, r := range f {
			if unicode.IsControl(r) || r == unicode.ReplacementChar {
				return "", pluginapi.Errf(pluginapi.CodeInvalid, "key %q contains a control or invalid character", clipMsg(f, 40))
			}
		}
	}
	out := strings.Join(fields, " ")
	if len(out) > maxChordText {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "key sequence %q is too long", clipMsg(out, 40))
	}
	return out, nil
}

// checkChord is checkChord plus the keymap's verdict (Services.ValidateChord): a chord that
// names no key is "invalid" here, where the plugin can handle it, not dropped later by the
// host.
func (p *plugin) checkChord(chord string) (string, error) {
	chord, err := checkChord(chord)
	if err != nil {
		return "", err
	}
	if err := p.r.svc.ValidateChord(chord); err != nil {
		msg := err.Error()
		if e, ok := errors.AsType[*pluginapi.Error](err); ok {
			msg = e.Msg
		}
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "%s", clipMsg(msg, 120))
	}
	return chord, nil
}

// bindings returns the plugin's binding set. It lives in the registry of the Lua state,
// which plugin code cannot reach, so it needs no field in plugin and goes away with the
// state. After the state is closed a detached empty set is returned: it publishes nothing.
func (p *plugin) bindings() *bindingSet {
	if p.L == nil {
		return &bindingSet{p: p}
	}
	reg, _ := p.L.Get(lua.RegistryIndex).(*lua.LTable)
	if reg == nil {
		return &bindingSet{p: p}
	}
	if ud, ok := reg.RawGetString("gvim.bindings").(*lua.LUserData); ok {
		if b, ok := ud.Value.(*bindingSet); ok {
			return b
		}
	}
	b := &bindingSet{p: p}
	ud := p.L.NewUserData()
	ud.Value = b
	reg.RawSetString("gvim.bindings", ud)
	return b
}

// has reports whether the plugin has a binding of chord.
func (b *bindingSet) has(chord string) bool {
	_, ok := b.at[bindingKey(chord)]
	return ok
}

// reindex rebuilds the chord index after list changed other than by appending.
func (b *bindingSet) reindex() {
	b.at = make(map[string]int, len(b.list))
	for i, bd := range b.list {
		b.at[bindingKey(bd.Chord)] = i
	}
}

// checkRoom reports whether n more bindings fit the quota.
func (b *bindingSet) checkRoom(n int) error {
	if len(b.list)+n > QuotaBindings {
		return pluginapi.Errf(pluginapi.CodeLimit, "key binding quota (%d)", QuotaBindings)
	}
	return nil
}

// set binds the chord of bd to bd.Action and publishes the set. See put.
func (b *bindingSet) set(bd pluginapi.Binding) error {
	if err := b.put(bd); err != nil {
		return err
	}
	b.publish()
	return nil
}

// put binds the chord of bd to bd.Action, replacing the plugin's earlier binding of the
// same chord, without publishing (a caller that adds several bindings publishes once).
// bd.Chord must have passed checkChord.
func (b *bindingSet) put(bd pluginapi.Binding) error {
	bd.Plugin = b.p.name
	key := bindingKey(bd.Chord)
	if i, ok := b.at[key]; ok {
		old := b.list[i].Action
		b.list[i] = bd
		b.dropIfOrphan(old)
		return nil
	}
	if err := b.checkRoom(1); err != nil {
		return err
	}
	if b.at == nil {
		b.at = map[string]int{}
	}
	b.at[key] = len(b.list)
	b.list = append(b.list, bd)
	return nil
}

// del removes the plugin's binding of chord and reports whether there was one.
func (b *bindingSet) del(chord string) bool {
	i, ok := b.at[bindingKey(chord)]
	if !ok {
		return false
	}
	old := b.list[i].Action
	b.list = append(b.list[:i:i], b.list[i+1:]...)
	b.reindex()
	b.dropIfOrphan(old)
	b.publish()
	return true
}

// dropAction removes every binding of action (the command was unregistered).
func (b *bindingSet) dropAction(action string) {
	kept := b.list[:0:0]
	for _, bd := range b.list {
		if bd.Action != action {
			kept = append(kept, bd)
		}
	}
	if len(kept) != len(b.list) {
		b.list = kept
		b.reindex()
		b.publish()
	}
}

// dropIfOrphan unregisters the hidden command of a Lua function that no binding uses any
// more.
func (b *bindingSet) dropIfOrphan(action string) {
	name, ok := strings.CutPrefix(action, b.p.name+"."+hiddenCommandPrefix)
	if !ok {
		return
	}
	if _, err := strconv.Atoi(name); err != nil {
		return
	}
	for _, bd := range b.list {
		if bd.Action == action {
			return
		}
	}
	b.p.removeCommand(hiddenCommandPrefix + name)
}

// publish replaces the plugin's binding set in the host.
func (b *bindingSet) publish() {
	if b.p.dead {
		return
	}
	b.p.r.host.SetBindings(b.p.name, append([]pluginapi.Binding(nil), b.list...))
}
