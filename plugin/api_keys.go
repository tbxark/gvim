package plugin

import (
	"sort"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.keymap, gvim.mode, gvim.context and gvim.input (stage S14).
//
//   - A binding of the normal editing context goes into the plugin's bindingSet
//     (bindings.go), which the host layers between the preset and the user's keybindings.
//   - A binding of a mode never enters the keymap: it belongs to the mode, which the host
//     keeps in its own table and consults before the keymap (ModeInfo).
//   - A Lua function given as the action becomes a hidden command "<plugin>.__fn<N>", so the
//     host only ever sees action ids.
//
// Modes are scoped to their plugin (the host key is plugin + name); the editor has one input
// mode at a time, wherever it comes from. The runtime keeps no copy of which one is active:
// the ui can clear it on its own (the panic chord, an unload), so gvim.mode.current asks
// Host.Holds(pluginapi.ModeContext(...)).

const (
	kindBinding = "Binding"
	kindMode    = "Mode"

	maxModes         = 32   // modes one plugin defines
	maxModeBindings  = 1024 // bindings of one mode
	maxContexts      = 64   // plugin contexts one plugin sets
	maxContextName   = 64
	maxStatusText    = 80 // cells of a status item or a mode status label
	defaultKeyWaitMS = 10_000
	maxKeyWaitMS     = 60_000
)

// reservedContextPrefix starts the when-clause names the host answers itself (the active
// input mode), which a plugin cannot set.
const reservedContextPrefix = "mode:"

// modeDef is a mode as the plugin defined it. info is what the host sees.
type modeDef struct {
	name string
	info pluginapi.ModeInfo
}

// keyState is the key-related state of one plugin: its modes and contexts, and the ids that
// tell a Binding handle whether its binding is still the one it made. Like the bindingSet it
// lives in the registry of the Lua state, out of reach of plugin code.
type keyState struct {
	p        *plugin
	modes    map[string]*modeDef
	order    []string // mode names in definition order
	contexts map[string]bool
	seq      uint64
	ids      map[string]uint64 // "<mode>\x00<bindingKey>" -> id of the binding a handle names
	items    []*statusItem     // status items in creation order (api_status.go)
	itemSeq  int
}

// keys returns the plugin's key state. After the state is closed it returns a detached empty
// one that publishes nothing.
func (p *plugin) keys() *keyState {
	if p.L == nil {
		return &keyState{p: p}
	}
	reg, _ := p.L.Get(lua.RegistryIndex).(*lua.LTable)
	if reg == nil {
		return &keyState{p: p}
	}
	if ud, ok := reg.RawGetString("gvim.keystate").(*lua.LUserData); ok {
		if k, ok := ud.Value.(*keyState); ok {
			return k
		}
	}
	k := &keyState{p: p, modes: map[string]*modeDef{}, contexts: map[string]bool{}, ids: map[string]uint64{}}
	ud := p.L.NewUserData()
	ud.Value = k
	reg.RawSetString("gvim.keystate", ud)
	return k
}

// idKey is the key of ids: the mode ("" for the normal context) and the chord.
func idKey(mode, chord string) string { return mode + "\x00" + bindingKey(chord) }

// modeBindingCount counts the bindings of every mode of the plugin.
func (k *keyState) modeBindingCount() int {
	n := 0
	for _, m := range k.modes {
		n += len(m.info.Bindings)
	}
	return n
}

// room reports whether n more bindings fit the plugin's quota, modes and normal context
// together.
func (k *keyState) room(n int) error {
	if len(k.p.bindings().list)+k.modeBindingCount()+n > QuotaBindings {
		return pluginapi.Errf(pluginapi.CodeLimit, "key binding quota (%d)", QuotaBindings)
	}
	return nil
}

// referenced reports whether some binding or mode of the plugin still runs action.
func (k *keyState) referenced(action string) bool {
	for _, b := range k.p.bindings().list {
		if b.Action == action {
			return true
		}
	}
	for _, m := range k.modes {
		if m.info.TextAction == action {
			return true
		}
		for _, a := range m.info.Bindings {
			if a == action {
				return true
			}
		}
	}
	return false
}

// dropIfOrphan unregisters the hidden command action when nothing runs it any more.
func (k *keyState) dropIfOrphan(action string) {
	name, ok := strings.CutPrefix(action, k.p.name+"."+hiddenCommandPrefix)
	if !ok {
		return
	}
	if _, err := strconv.Atoi(name); err != nil || k.referenced(action) {
		return
	}
	k.p.removeCommand(hiddenCommandPrefix + name)
}

// publishModes replaces the plugin's mode set in the host.
func (k *keyState) publishModes() {
	p := k.p
	if p.dead || p.L == nil {
		return
	}
	infos := make([]pluginapi.ModeInfo, 0, len(k.order))
	for _, n := range k.order {
		infos = append(infos, cloneMode(k.modes[n].info))
	}
	p.r.host.SetModes(p.name, infos)
}

// cloneMode copies the maps of a ModeInfo, so the host never shares them with the runtime.
func cloneMode(m pluginapi.ModeInfo) pluginapi.ModeInfo {
	b := make(map[string]string, len(m.Bindings))
	for c, a := range m.Bindings {
		b[c] = a
	}
	m.Bindings = b
	if m.Args != nil {
		a := make(map[string]map[string]any, len(m.Args))
		for c, v := range m.Args {
			a[c] = v
		}
		m.Args = a
	}
	return m
}

// hiddenCommandTitled is hiddenCommand with a palette title, for a function bound to a key
// with a description.
func (p *plugin) hiddenCommandTitled(fn *lua.LFunction, title string) (string, error) {
	p.fnSeq++
	name := hiddenCommandPrefix + strconv.Itoa(p.fnSeq)
	id := p.name + "." + name
	if title == "" {
		title = name
	}
	err := p.addCommand(&command{id: id, name: name, fn: fn,
		info: pluginapi.CommandInfo{ID: id, Title: title, Plugin: p.name, Hidden: true, Record: true}})
	if err != nil {
		return "", err
	}
	return id, nil
}

// bindingHandle is the payload of a Binding handle.
type bindingHandle struct {
	k     *keyState
	mode  string
	chord string
	id    uint64
}

func (h *bindingHandle) handleString() string { return h.chord }

func (h *bindingHandle) active() bool {
	return !h.k.p.dead && h.k.ids[idKey(h.mode, h.chord)] == h.id
}

// modeHandle is the payload of a Mode handle.
type modeHandle struct {
	p    *plugin
	name string
}

func (h *modeHandle) handleString() string { return h.name }

// keymapOpts are the options of gvim.keymap.set.
type keymapOpts struct {
	mode string
	desc string
	args map[string]any
	when []string
}

func init() {
	registerAwait("next_key", awaitNextKey)
}

// installKeys installs gvim.keymap, gvim.mode, gvim.context and gvim.input, and the status
// item API of gvim.ui.statusbar (api_status.go).
func installKeys(e *apiEnv) {
	e.provides("input")
	installKeymap(e)
	installMode(e)
	installContext(e)
	installInput(e)
	installStatus(e)
}

// ---------- gvim.keymap ----------

func installKeymap(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("keymap")

	bmethods := e.kind(kindBinding).methods
	e.set(bmethods, "del", func(L *lua.LState) int {
		h := checkObject(L, 1, kindBinding).(*bindingHandle)
		if !h.active() {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeNotFound, "key %q is no longer bound by this binding", h.chord))
		}
		h.k.del(h.mode, h.chord)
		return bufTrue(L, nil)
	})
	e.set(bmethods, "active", func(L *lua.LState) int {
		L.Push(lua.LBool(checkObject(L, 1, kindBinding).(*bindingHandle).active()))
		return 1
	})

	e.set(area, "set", func(L *lua.LState) int {
		chord, err := p.checkChord(L.CheckString(1))
		if err != nil {
			return p.registrationFailed(L, "keymap.set", err)
		}
		target := L.Get(2)
		switch v := target.(type) {
		case lua.LString:
			if v == "" || len(v) > maxActionName {
				L.ArgError(2, "action name expected")
			}
		case *lua.LFunction:
		default:
			L.ArgError(2, "action name or function expected, got "+target.Type().String())
		}
		o, err := readKeymapOpts(L, p, 3)
		if err != nil {
			return p.registrationFailed(L, "keymap.set", err)
		}
		k := p.keys()
		var md *modeDef
		switch {
		case o.mode != "":
			if md = k.modes[o.mode]; md == nil {
				return p.registrationFailed(L, "keymap.set", pluginapi.Errf(pluginapi.CodeNotFound, "mode %q is not defined", o.mode))
			}
			if len(o.when) > 0 {
				return p.registrationFailed(L, "keymap.set", pluginapi.Errf(pluginapi.CodeInvalid, "when applies to bindings of the normal context, not of a mode"))
			}
			if _, replace := md.info.Bindings[bindingKey(chord)]; !replace && len(md.info.Bindings) >= maxModeBindings {
				return p.registrationFailed(L, "keymap.set", pluginapi.Errf(pluginapi.CodeLimit, "mode binding quota (%d)", maxModeBindings))
			}
		case strings.Contains(chord, " "):
			return p.registrationFailed(L, "keymap.set", pluginapi.Errf(pluginapi.CodeInvalid, "key sequence %q needs a mode: the editing keymap resolves single keys only", clipMsg(chord, 40)))
		}
		isNew := !p.bindings().has(chord)
		if md != nil {
			isNew = !k.replaces(md, chord)
		}
		if isNew {
			if err := k.room(1); err != nil {
				return p.registrationFailed(L, "keymap.set", err)
			}
		}
		action, made, err := k.action(L, target, o.desc)
		if err != nil {
			return p.registrationFailed(L, "keymap.set", err)
		}
		if md != nil {
			old := md.info.Bindings[bindingKey(chord)]
			md.info.Bindings[bindingKey(chord)] = action
			if o.args != nil {
				if md.info.Args == nil {
					md.info.Args = map[string]map[string]any{}
				}
				md.info.Args[bindingKey(chord)] = o.args
			} else {
				delete(md.info.Args, bindingKey(chord))
			}
			k.dropIfOrphan(old)
			k.publishModes()
		} else {
			bd := pluginapi.Binding{Chord: chord, Action: action, Args: o.args, When: o.when}
			if err := p.bindings().set(bd); err != nil {
				if made {
					k.dropIfOrphan(action)
				}
				return p.registrationFailed(L, "keymap.set", err)
			}
		}
		k.seq++
		k.ids[idKey(o.mode, chord)] = k.seq
		L.Push(p.newObject(kindBinding, &bindingHandle{k: k, mode: o.mode, chord: chord, id: k.seq}))
		return 1
	})

	e.set(area, "del", func(L *lua.LState) int {
		chord, err := p.checkChord(L.CheckString(1))
		if err != nil {
			return bufRet(L, err)
		}
		mode := ""
		if t := bufOptTable(L, 2); t != nil {
			switch v := t.RawGetString("mode").(type) {
			case *lua.LNilType:
			case lua.LString:
				mode = string(v)
			default:
				L.ArgError(2, "option 'mode' must be a string")
			}
		}
		k := p.keys()
		if mode != "" && k.modes[mode] == nil {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeNotFound, "mode %q is not defined", mode))
		}
		k.del(mode, chord) // idempotent: an unbound chord is the state the caller asked for
		return bufTrue(L, nil)
	})

	e.set(area, "label", func(L *lua.LState) int {
		action := checkActionName(L, 1)
		if s := r.guard.KeyLabel(action); s != "" {
			L.Push(lua.LString(s))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	})
}

// replaces reports whether chord already has a binding in mode md.
func (k *keyState) replaces(md *modeDef, chord string) bool {
	_, ok := md.info.Bindings[bindingKey(chord)]
	return ok
}

// del removes the plugin's binding of chord in mode ("" is the normal context) and reports
// whether there was one.
func (k *keyState) del(mode, chord string) bool {
	delete(k.ids, idKey(mode, chord))
	if mode == "" {
		return k.p.bindings().del(chord)
	}
	md := k.modes[mode]
	if md == nil {
		return false
	}
	key := bindingKey(chord)
	old, ok := md.info.Bindings[key]
	if !ok {
		return false
	}
	delete(md.info.Bindings, key)
	delete(md.info.Args, key)
	k.dropIfOrphan(old)
	k.publishModes()
	return true
}

// action turns the action argument of a binding into an action id: a string is one, a
// function becomes a hidden command. made says that a hidden command was registered.
func (k *keyState) action(L *lua.LState, v lua.LValue, desc string) (id string, made bool, err error) {
	switch v := v.(type) {
	case lua.LString:
		return string(v), false, nil
	case *lua.LFunction:
		id, err := k.p.hiddenCommandTitled(v, desc)
		return id, err == nil, err
	}
	return "", false, pluginapi.Errf(pluginapi.CodeInvalid, "action name or function expected")
}

// readKeymapOpts reads the options of keymap.set. A field of the wrong type raises; a value
// that is not acceptable is an expected failure.
func readKeymapOpts(L *lua.LState, p *plugin, n int) (keymapOpts, error) {
	var o keymapOpts
	t := bufOptTable(L, n)
	if t == nil {
		return o, nil
	}
	str := func(name string) (string, bool) {
		switch v := t.RawGetString(name).(type) {
		case *lua.LNilType:
			return "", false
		case lua.LString:
			return string(v), true
		default:
			L.ArgError(n, "option '"+name+"' must be a string")
		}
		return "", false
	}
	if s, ok := str("mode"); ok {
		o.mode = s
		if !nameRE.MatchString(s) {
			return o, pluginapi.Errf(pluginapi.CodeInvalid, "invalid mode name %q", clipMsg(s, 48))
		}
	}
	if s, ok := str("desc"); ok {
		if len(s) > maxCommandTitle {
			return o, pluginapi.Errf(pluginapi.CodeInvalid, "desc is too long")
		}
		o.desc = oneLine(s)
	}
	switch v := t.RawGetString("args").(type) {
	case *lua.LNilType:
	case *lua.LTable:
		args, err := argsFromTable(L, p, n, v)
		if err != nil {
			return o, err
		}
		o.args = args
	default:
		L.ArgError(n, "option 'args' must be a table")
	}
	switch v := t.RawGetString("when").(type) {
	case *lua.LNilType:
	case *lua.LTable:
		list, err := stringList(L, v, "when", maxCommandWhen)
		if err != nil {
			return o, err
		}
		for _, w := range list {
			if !validWhen(w) {
				return o, pluginapi.Errf(pluginapi.CodeInvalid, "invalid when condition %q", clipMsg(w, 48))
			}
		}
		o.when = list
	default:
		L.ArgError(n, "option 'when' must be an array of strings")
	}
	return o, nil
}

// ---------- gvim.mode ----------

func installMode(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("mode")

	enter := func(L *lua.LState, name string) int {
		k := p.keys()
		if k.modes[name] == nil {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeNotFound, "mode %q is not defined", name))
		}
		if p.muted() {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeClosed, "plugin %q is suspended", p.name))
		}
		return bufTrue(L, r.guard.SetInputMode(p.name, name))
	}
	mmethods := e.kind(kindMode).methods
	e.set(mmethods, "enter", func(L *lua.LState) int {
		return enter(L, checkObject(L, 1, kindMode).(*modeHandle).name)
	})
	e.set(mmethods, "name", func(L *lua.LState) int {
		L.Push(lua.LString(checkObject(L, 1, kindMode).(*modeHandle).name))
		return 1
	})

	e.set(area, "define", func(L *lua.LState) int {
		spec := L.CheckTable(1)
		md, err := parseModeSpec(L, p, spec)
		if err != nil {
			return bufRet(L, err)
		}
		k := p.keys()
		k.modes[md.name] = md
		k.order = append(k.order, md.name)
		k.publishModes()
		L.Push(p.newObject(kindMode, &modeHandle{p: p, name: md.name}))
		return 1
	})

	e.set(area, "enter", func(L *lua.LState) int { return enter(L, L.CheckString(1)) })

	e.set(area, "leave", func(L *lua.LState) int {
		return bufTrue(L, r.guard.SetInputMode(p.name, ""))
	})

	e.set(area, "current", func(L *lua.LState) int {
		for _, q := range r.order {
			if q.dead || q.L == nil {
				continue
			}
			k := q.keys()
			for _, n := range k.order {
				if r.guard.Holds(pluginapi.ModeContext(q.name, n)) {
					L.Push(lua.LString(n))
					L.Push(lua.LString(q.name))
					return 2
				}
			}
		}
		L.Push(lua.LNil)
		return 1
	})
}

// parseModeSpec validates the table given to mode.define and registers the hidden commands
// of its functions. On failure nothing stays registered.
func parseModeSpec(L *lua.LState, p *plugin, spec *lua.LTable) (*modeDef, error) {
	field := func(name string, want lua.LValueType) lua.LValue {
		v := spec.RawGetString(name)
		if v != lua.LNil && v.Type() != want {
			L.RaiseError("gvim.mode.define: field %q must be a %s, got %s", name, want, v.Type())
		}
		return v
	}
	nameV, _ := field("name", lua.LTString).(lua.LString)
	if nameV == "" {
		L.RaiseError("gvim.mode.define: field %q is required", "name")
	}
	name := string(nameV)
	if !nameRE.MatchString(name) {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "invalid mode name %q (want [a-z][a-z0-9_]*, at most 40 characters)", clipMsg(name, 48))
	}
	k := p.keys()
	if k.modes[name] != nil {
		return nil, pluginapi.Errf(pluginapi.CodeConflict, "mode %q is already defined", name)
	}
	if len(k.modes) >= maxModes {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "mode quota (%d)", maxModes)
	}
	info := pluginapi.ModeInfo{Name: name, Plugin: p.name, Bindings: map[string]string{}, Fallthrough: true}
	if b, ok := field("fallthrough", lua.LTBool).(lua.LBool); ok {
		info.Fallthrough = bool(b)
	}
	if s, ok := field("status", lua.LTString).(lua.LString); ok {
		info.Status = p.r.svc.Truncate(oneLine(clipMsg(string(s), 4*maxStatusText)), maxStatusText, "…")
	}

	// text: "pass" | "swallow" | function
	var textFn *lua.LFunction
	switch v := spec.RawGetString("text").(type) {
	case *lua.LNilType:
	case lua.LString:
		switch v {
		case "pass":
		case "swallow":
			info.Text = pluginapi.TextSwallow
		default:
			return nil, pluginapi.Errf(pluginapi.CodeInvalid, `text must be "pass", "swallow" or a function`)
		}
	case *lua.LFunction:
		info.Text, textFn = pluginapi.TextHandler, v
	default:
		L.RaiseError("gvim.mode.define: field %q must be a string or a function, got %s", "text", v.Type())
	}
	// paste: "pass" | "swallow" | "text" (the text function runs with paste = true)
	switch v := field("paste", lua.LTString).(type) {
	case lua.LString:
		switch v {
		case "pass":
		case "swallow":
			info.Paste = pluginapi.TextSwallow
		case "text":
			if textFn == nil {
				return nil, pluginapi.Errf(pluginapi.CodeInvalid, `paste = "text" needs a text function`)
			}
			info.Paste = pluginapi.TextHandler
		default:
			return nil, pluginapi.Errf(pluginapi.CodeInvalid, `paste must be "pass", "swallow" or "text"`)
		}
	}

	// bindings: chord -> action name | function
	type pending struct {
		chord string
		name  string
		fn    *lua.LFunction
	}
	var binds []pending
	if bt, ok := field("bindings", lua.LTTable).(*lua.LTable); ok {
		seen := map[string]bool{}
		var rerr error
		bt.ForEach(func(key, val lua.LValue) {
			if rerr != nil {
				return
			}
			ks, ok := key.(lua.LString)
			if !ok {
				L.RaiseError("gvim.mode.define: bindings must be keyed by chord strings")
			}
			chord, err := p.checkChord(string(ks))
			if err != nil {
				rerr = err
				return
			}
			if seen[bindingKey(chord)] {
				rerr = pluginapi.Errf(pluginapi.CodeInvalid, "key %q is bound twice", clipMsg(chord, 40))
				return
			}
			seen[bindingKey(chord)] = true
			switch v := val.(type) {
			case lua.LString:
				if v == "" || len(v) > maxActionName {
					L.RaiseError("gvim.mode.define: the action of %q must be an action name or a function", chord)
				}
				binds = append(binds, pending{chord: chord, name: string(v)})
			case *lua.LFunction:
				binds = append(binds, pending{chord: chord, fn: v})
			default:
				L.RaiseError("gvim.mode.define: the action of %q must be an action name or a function, got %s", chord, val.Type())
			}
		})
		if rerr != nil {
			return nil, rerr
		}
		if len(binds) > maxModeBindings {
			return nil, pluginapi.Errf(pluginapi.CodeLimit, "mode binding quota (%d)", maxModeBindings)
		}
		if err := k.room(len(binds)); err != nil {
			return nil, err
		}
		sort.Slice(binds, func(i, j int) bool { return binds[i].chord < binds[j].chord })
	}

	var made []string
	undo := func() {
		for _, id := range made {
			p.removeCommand(strings.TrimPrefix(id, p.name+"."))
		}
	}
	for _, b := range binds {
		action := b.name
		if b.fn != nil {
			id, err := p.hiddenCommandTitled(b.fn, "")
			if err != nil {
				undo()
				return nil, err
			}
			made = append(made, id)
			action = id
		}
		info.Bindings[bindingKey(b.chord)] = action
	}
	if textFn != nil {
		id, err := p.hiddenCommandTitled(textFn, "")
		if err != nil {
			undo()
			return nil, err
		}
		made = append(made, id)
		info.TextAction = id
	}
	return &modeDef{name: name, info: info}, nil
}

// ---------- gvim.context ----------

func installContext(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("context")

	check := func(L *lua.LState) (string, error) {
		name := L.CheckString(1)
		if name == "" || len(name) > maxContextName || strings.HasPrefix(name, "!") || !validWhen(name) {
			return "", pluginapi.Errf(pluginapi.CodeInvalid, "invalid context name %q", clipMsg(name, 48))
		}
		if strings.HasPrefix(name, reservedContextPrefix) {
			return "", pluginapi.Errf(pluginapi.CodeInvalid, "context names starting with %q are the host's", reservedContextPrefix)
		}
		return name, nil
	}
	e.set(area, "set", func(L *lua.LState) int {
		name, err := check(L)
		if err != nil {
			return bufRet(L, err)
		}
		on := L.CheckBool(2)
		k := p.keys()
		if _, known := k.contexts[name]; !known && on && len(k.contexts) >= maxContexts {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "context quota (%d)", maxContexts))
		}
		if on {
			k.contexts[name] = true
		} else {
			delete(k.contexts, name)
		}
		r.guard.SetContext(p.name, name, on)
		return bufTrue(L, nil)
	})
	// get answers the plugin's own value, and for a name it never set, the host's: a built-in
	// context such as read_only, a context another plugin set, or the active mode.
	e.set(area, "get", func(L *lua.LState) int {
		name := L.CheckString(1)
		if v, ok := p.keys().contexts[name]; ok {
			L.Push(lua.LBool(v))
			return 1
		}
		L.Push(lua.LBool(name != "" && r.guard.Holds(name)))
		return 1
	})
}

// ---------- gvim.input ----------

func installInput(e *apiEnv) {
	area := e.area("input")
	area.RawSetString("next_key", e.async("next_key"))
}

// awaitNextKey is gvim.input.next_key(opts?). The host captures the next key press before
// the keymap sees it; the wait ends with the key, a timeout, or a cancel (panic chord,
// unload).
func awaitNextKey(c *awaitCall) {
	ms := defaultKeyWaitMS
	if len(c.args) > 0 && c.args[0] != lua.LNil {
		t, ok := c.args[0].(*lua.LTable)
		if !ok {
			c.Bad(1, "next_key", "table expected, got "+c.args[0].Type().String())
			return
		}
		switch v := t.RawGetString("timeout_ms").(type) {
		case *lua.LNilType:
		case lua.LNumber:
			if !(v >= 1 && v <= maxKeyWaitMS) {
				c.Bad(1, "next_key", "option 'timeout_ms' must be in 1.."+strconv.Itoa(maxKeyWaitMS))
				return
			}
			ms = int(v)
		default:
			c.Bad(1, "next_key", "option 'timeout_ms' must be a number")
			return
		}
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok := c.NewToken()
	if err := c.r.guard.WaitKey(tok, ms); err != nil {
		c.Fail(err)
		return
	}
	p := c.p
	g := c.r.guard
	c.Park(&parkSpec{
		Token:  tok,
		Cancel: func() { g.CancelWait(tok) },
		Convert: func(cp pluginapi.Completion) []lua.LValue {
			if cp.Err != "" {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(cp.Err)}
			}
			if len(cp.Values) > 0 {
				if key, ok := cp.Values[0].(pluginapi.Key); ok {
					return []lua.LValue{lua.LTrue, p.keyTable(key)}
				}
			}
			return []lua.LValue{lua.LTrue, lua.LNil, lua.LString("io: key wait finished without a key")}
		},
	})
}

// keyTable converts a key press to the Lua Key table; text is nil for a key that types
// nothing.
func (p *plugin) keyTable(k pluginapi.Key) *lua.LTable {
	t := p.L.CreateTable(0, 6)
	t.RawSetString("key", lua.LString(k.Key))
	if k.Text != "" {
		t.RawSetString("text", lua.LString(k.Text))
	}
	t.RawSetString("ctrl", lua.LBool(k.Ctrl))
	t.RawSetString("alt", lua.LBool(k.Alt))
	t.RawSetString("shift", lua.LBool(k.Shift))
	t.RawSetString("super", lua.LBool(k.Super))
	return t
}

// registrationFailed returns the failure pair of a registration call (a command, a key) and
// says so once in the log and on the status row: a plugin that does not look at the result
// would otherwise have no command or key and no hint why.
func (p *plugin) registrationFailed(L *lua.LState, what string, err error) int {
	msg := what + ": " + errString(err)
	p.r.logf(p.name, levelWarn, msg)
	p.r.notify(p, pluginapi.LevelWarn, p.name+": "+msg)
	return bufRet(L, err)
}
