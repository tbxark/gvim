package plugin

import (
	lua "github.com/yuin/gopher-lua"
)

// apiEnv is what an API installer receives: one per plugin state, valid while the state
// is being built. Installers fill the tables of the gvim API with Go functions that close
// over the plugin.
//
// A stage that owns an area replaces the installer in its api_<area>.go; the list below is
// the only shared place, so stages add files instead of editing a table.
type apiEnv struct {
	r    *Runtime
	p    *plugin
	L    *lua.LState // the main thread: build tables and functions with it, never run plugin code
	gvim *lua.LTable
}

// installers run in this order for every new plugin state.
var installers = []func(*apiEnv){
	installHandleKinds,
	installRoot,
	installAsync,
	installBuf,
	installWin,
	installSeams,
	installLineTargets,
	installExplorer,
	installCmd,
	installEvents,
	installKeys,
	installUI,
	installPanel,
	installGroups,
	installI18n,
	installMenu,
	installDecor,
	installPath,
	installFS,
	installProc,
	installTimer,
	installState,
	installURL,
	installData,
	installWorkspace,
	installPlugins,
	installDashboard,
	installLSP,
	installAgentSessions,
	installHTTP,
}

// extraInstallers run after installers; tests add fakes here (export_test.go).
var extraInstallers []func(*apiEnv)

// installAPI creates the gvim table and runs the installers.
func (p *plugin) installAPI() {
	e := &apiEnv{r: p.r, p: p, L: p.L, gvim: p.L.NewTable()}
	p.gvim = e.gvim
	mt := p.L.NewTable()
	mt.RawSetString("__index", p.L.NewFunction(p.libIndex))
	mt.RawSetString("__metatable", lua.LString("locked"))
	p.L.SetMetatable(e.gvim, mt)
	for _, f := range installers {
		f(e)
	}
	for _, f := range extraInstallers {
		f(e)
	}
}

// area returns the table gvim.<name>, creating it on first use. Areas are also reachable
// as require "gvim.<name>".
func (e *apiEnv) area(name string) *lua.LTable {
	if t := e.p.areas[name]; t != nil {
		return t
	}
	t := e.L.NewTable()
	e.gvim.RawSetString(name, t)
	e.p.areas[name] = t
	return t
}

// set stores the Go function f as t[name], guarded (see guarded).
func (e *apiEnv) set(t *lua.LTable, name string, f lua.LGFunction) {
	t.RawSetString(name, e.L.NewFunction(e.p.guarded(f)))
}

// fn makes a guarded Lua function from f, for functions that are not stored by set.
func (e *apiEnv) fn(f lua.LGFunction) *lua.LFunction { return e.L.NewFunction(e.p.guarded(f)) }

// goPanic is a panic of a Go API function that is not a Lua error.
type goPanic struct {
	value any
	stack string
}

// guarded wraps an API function so that a Go panic (a bug in the runtime or the host) is
// recorded before gopher-lua turns it into a plain Lua error: the runtime then counts it as
// a strike, not as an ordinary error of the plugin. Lua errors raised with RaiseError pass
// through untouched.
func (p *plugin) guarded(f lua.LGFunction) lua.LGFunction {
	return func(L *lua.LState) int {
		defer func() {
			if rec := recover(); rec != nil {
				if !isLuaFailure(rec) {
					p.goPanic = &goPanic{value: rec}
					if p.r.debug {
						p.goPanic.stack = stackString()
					}
				}
				panic(rec)
			}
		}()
		return f(L)
	}
}

// isLuaFailure reports whether a recovered panic is something the plugin's own code caused:
// a Lua error, or the call stack running out (an endless recursion through a metamethod that
// a Go function calls). Neither is a bug of the runtime.
func isLuaFailure(rec any) bool {
	if _, ok := rec.(*lua.ApiError); ok {
		return true
	}
	s, ok := rec.(string)
	return ok && s == "lua callstack overflow"
}

// kind returns the handle kind name (creating it); its methods table is for the installer.
func (e *apiEnv) kind(name string) *handleKind { return e.p.kind(name) }

// async returns the Lua function that performs the async call of kind and returns at most
// two values (value, err); asyncV returns any number. The kind must be registered with
// registerAwait.
func (e *apiEnv) async(kind string) lua.LValue {
	if k, ok := awaitKinds[kind]; !ok || k.variadic {
		panic("plugin: async(" + kind + "): not registered with registerAwait")
	}
	return e.p.mkAwait(kind)
}

func (e *apiEnv) asyncV(kind string) lua.LValue {
	if k, ok := awaitKinds[kind]; !ok || !k.variadic {
		panic("plugin: asyncV(" + kind + "): not registered with registerAwaitV")
	}
	return e.p.mkAwaitV(kind)
}

// provides marks a feature reported by gvim.has.
func (e *apiEnv) provides(feature string) { e.p.feature[feature] = true }

// runLua compiles (once per process) and runs an embedded Lua chunk in the plugin state with
// the given arguments, returning its results. Embedded chunks are trusted and small; they
// capture what they need as arguments, like the prelude.
func (e *apiEnv) runLua(chunk, src string, nret int, args ...lua.LValue) []lua.LValue {
	proto, err := internalProto(chunk, src)
	if err != nil {
		panic("plugin: embedded chunk " + chunk + ": " + err.Error())
	}
	L := e.L
	L.Push(L.NewFunctionFromProto(proto))
	for _, a := range args {
		L.Push(a)
	}
	L.Call(len(args), nret)
	out := make([]lua.LValue, nret)
	for i := range out {
		out[i] = L.Get(-nret + i)
	}
	L.Pop(nret)
	return out
}
