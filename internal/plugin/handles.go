package plugin

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// Handle kinds that are identified by a host id and cached per state (D7): the same id
// always gives the same userdata, so `a == b` and table keys work.
const (
	kindBuffer = "Buffer"
	kindWindow = "Window"
)

// Handle kinds that wrap a runtime object (created, never looked up by id).
const (
	kindTask = "Task"
)

// idHandle is the payload of a cached handle userdata.
type idHandle struct {
	kind string
	id   uint64
}

// object is the payload of an object handle userdata (Task, Subscription, Timer, ...).
type object struct {
	kind  string
	value any
}

// stringer lets an object payload choose its tostring text: "<Kind>(<handleString>)".
type stringer interface{ handleString() string }

// handleKind is the metatable and method table of one handle type in one state.
type handleKind struct {
	name    string
	cached  bool
	mt      *lua.LTable
	methods *lua.LTable // installers fill this table; the metatable's __index reads it
}

// kind returns the handle kind name, creating its locked metatable on first use. API
// stages add methods with `e.kind("Buffer").methods.RawSetString(...)`.
func (p *plugin) kind(name string) *handleKind {
	if k := p.kinds[name]; k != nil {
		return k
	}
	L := p.L
	k := &handleKind{name: name, cached: name == kindBuffer || name == kindWindow}
	k.methods = L.NewTable()
	k.mt = L.NewTable()
	if k.cached {
		methods := k.methods
		k.mt.RawSetString("__index", L.NewFunction(func(L *lua.LState) int {
			ud := L.CheckUserData(1)
			if key, ok := L.Get(2).(lua.LString); ok {
				if key == "id" {
					if h, ok := ud.Value.(idHandle); ok {
						L.Push(lua.LNumber(h.id))
						return 1
					}
				}
				L.Push(methods.RawGetString(string(key)))
				return 1
			}
			L.Push(lua.LNil)
			return 1
		}))
		k.mt.RawSetString("__eq", L.NewFunction(func(L *lua.LState) int {
			a, aok := L.CheckUserData(1).Value.(idHandle)
			b, bok := L.CheckUserData(2).Value.(idHandle)
			L.Push(lua.LBool(aok && bok && a == b))
			return 1
		}))
	} else {
		k.mt.RawSetString("__index", k.methods)
	}
	k.mt.RawSetString("__newindex", L.NewFunction(func(L *lua.LState) int {
		L.RaiseError("attempt to modify a %s handle", name)
		return 0
	}))
	k.mt.RawSetString("__tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(handleText(name, L.CheckUserData(1))))
		return 1
	}))
	k.mt.RawSetString("__metatable", lua.LString("locked"))
	p.kinds[name] = k
	return k
}

func handleText(kind string, ud *lua.LUserData) string {
	switch v := ud.Value.(type) {
	case idHandle:
		return fmt.Sprintf("%s(%d)", kind, v.id)
	case *object:
		if s, ok := v.value.(stringer); ok {
			return kind + "(" + s.handleString() + ")"
		}
	}
	return kind
}

// idHandle returns the cached userdata for (kind, id), creating it on a miss.
func (p *plugin) idHandle(kind string, id uint64) lua.LValue {
	key := idHandle{kind, id}
	if ud, ok := p.handles[key]; ok {
		return ud
	}
	if len(p.handles) >= maxHandles {
		p.purgeHandles()
		for k := range p.handles { // still full: evict a tenth, arbitrary
			if len(p.handles) < maxHandles-maxHandles/10 {
				break
			}
			delete(p.handles, k)
		}
	}
	ud := p.L.NewUserData()
	ud.Value = key
	p.L.SetMetatable(ud, p.kind(kind).mt)
	p.handles[key] = ud
	return ud
}

// dropHandle forgets the cached handle of (kind, id), for example after buffer_closed.
// A handle a plugin still holds keeps working as a dead handle.
func (p *plugin) dropHandle(kind string, id uint64) { delete(p.handles, idHandle{kind, id}) }

// purgeHandles removes the cached handles whose host object is gone. The windows are
// listed once for the whole purge: a listing lays out every pane, and a plugin that
// splits and closes windows leaves thousands of dead handles to check.
func (p *plugin) purgeHandles() {
	var wins map[uint64]bool
	for k := range p.handles {
		if k.kind == kindWindow && wins == nil {
			list := p.r.host.Windows()
			wins = make(map[uint64]bool, len(list))
			for _, w := range list {
				wins[uint64(w.ID)] = true
			}
		}
		if k.kind == kindWindow && !wins[k.id] || k.kind != kindWindow && !p.handleValid(k.kind, k.id) {
			delete(p.handles, k)
		}
	}
}

// handleValid asks the host whether the object behind an id handle still exists.
func (p *plugin) handleValid(kind string, id uint64) bool {
	h := p.r.host
	switch kind {
	case kindBuffer:
		_, ok := h.Buffer(pluginapi.BufferID(id))
		return ok
	case kindWindow:
		for _, w := range h.Windows() {
			if uint64(w.ID) == id {
				return true
			}
		}
	}
	return false
}

// newObject wraps a runtime object in a userdata of the given kind.
func (p *plugin) newObject(kind string, v any) *lua.LUserData {
	ud := p.L.NewUserData()
	ud.Value = &object{kind: kind, value: v}
	p.L.SetMetatable(ud, p.kind(kind).mt)
	return ud
}

// checkID returns the id of the handle argument n, which must be of the given kind.
func checkID(L *lua.LState, n int, kind string) uint64 {
	ud := L.CheckUserData(n)
	if h, ok := ud.Value.(idHandle); ok && h.kind == kind {
		return h.id
	}
	L.ArgError(n, kind+" expected")
	return 0
}

// checkObject returns the value of the object handle argument n of the given kind.
func checkObject(L *lua.LState, n int, kind string) any {
	ud := L.CheckUserData(n)
	if o, ok := ud.Value.(*object); ok && o.kind == kind {
		return o.value
	}
	L.ArgError(n, kind+" expected")
	return nil
}

// installHandleKinds gives the cached kinds their S2 methods: `valid`. The rest of the
// Buffer and Window methods belong to the API stages that own those areas.
func installHandleKinds(e *apiEnv) {
	p := e.p
	for _, name := range []string{kindBuffer, kindWindow} {
		name := name
		p.kind(name).methods.RawSetString("valid", e.fn(func(L *lua.LState) int {
			id := checkID(L, 1, name)
			ok := p.handleValid(name, id)
			if !ok {
				p.dropHandle(name, id)
			}
			L.Push(lua.LBool(ok))
			return 1
		}))
	}
}
