package plugin

import (
	"fmt"

	"github.com/tbxark/gvim/pluginapi"
)

// lintBindings names key bindings of an action that nothing provides: neither the editor nor a
// command of a loaded plugin. A load that ends says each one once, so a typo is seen at startup
// and a reload does not say it again. Without a list of the editor's actions it says nothing.
func (r *Runtime) lintBindings() {
	actions := r.guard.Actions()
	if len(actions) == 0 {
		return
	}
	known := make(map[string]bool, len(actions))
	for _, a := range actions {
		known[a] = true
	}
	for _, p := range r.order {
		if p.dead {
			continue
		}
		for _, c := range p.commands {
			known[c.id] = true
		}
	}
	for _, p := range r.order {
		if p.dead || p.muted() || p.L == nil {
			continue
		}
		check := func(chord, action, where string) {
			if action == "" || known[action] {
				return
			}
			key := "bind\x00" + where + chord + "\x00" + action
			if p.bindWarned[key] {
				return
			}
			if p.bindWarned == nil {
				p.bindWarned = map[string]bool{}
			}
			p.bindWarned[key] = true
			msg := fmt.Sprintf("key %s%s is bound to '%s', which is no action or command", where, chord, action)
			r.logf(p.name, levelWarn, msg)
			r.notify(p, pluginapi.LevelWarn, p.name+": "+msg)
		}
		for _, b := range p.bindings().list {
			check(b.Chord, b.Action, "")
		}
		k := p.keys()
		for _, name := range k.order {
			md := k.modes[name]
			for chord, action := range md.info.Bindings {
				check(chord, action, "of mode "+name+" ")
			}
		}
	}
}
