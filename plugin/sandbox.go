package plugin

import (
	_ "embed"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

//go:embed lua/prelude.lua
var preludeSrc string

// preludeChunk is the chunk name of the prelude; frames in it are skipped when a message
// needs the plugin's own position.
const preludeChunk = "gvim-prelude"

// internalProtos caches the compiled prototypes of the runtime's own Lua sources (the
// prelude and the chunks the API files embed). A prototype is state independent.
var internalProtos sync.Map // chunk name -> *lua.FunctionProto

func internalProto(chunk, src string) (*lua.FunctionProto, error) {
	if v, ok := internalProtos.Load(chunk); ok {
		return v.(*lua.FunctionProto), nil
	}
	proto, err := CompileSource(chunk, src)
	if err != nil {
		return nil, err
	}
	internalProtos.Store(chunk, proto)
	return proto, nil
}

// stateOptions are the lua.Options of every plugin state and coroutine: small stacks and
// registries that grow on demand (a default lua.Options{} has a registry that cannot grow,
// so table.concat of 3000 items dies), at most 1<<20 slots (about 16 MiB worst case).
func stateOptions() lua.Options {
	return lua.Options{
		SkipOpenLibs:        true,
		CallStackSize:       256,
		RegistrySize:        256,
		RegistryMaxSize:     1 << 20,
		RegistryGrowStep:    256,
		MinimizeStackMemory: true,
	}
}

// stripped are the globals removed from a fresh state: dynamic code loading, file access,
// environment tricks and process-wide effects.
var stripped = []string{
	"dofile", "loadfile", "load", "loadstring", "module", "newproxy", "collectgarbage",
	"getfenv", "setfenv", "_printregs", "_GOPHER_LUA_VERSION", "require",
}

// buildState creates the sandboxed state of p and installs the API.
func (p *plugin) buildState() error {
	// 1. Only base, table, string, math and coroutine are opened. io, os, debug, package and
	// channel never are (a reduced os is installed with the stdlib below).
	L := lua.NewState(stateOptions())
	p.L = L
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.CoroutineLibName, lua.OpenCoroutine},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}

	// Originals the runtime keeps; plugin tampering with globals cannot affect them.
	co := L.GetGlobal("coroutine").(*lua.LTable)
	str := L.GetGlobal("string").(*lua.LTable)
	create := co.RawGetString("create")
	status := co.RawGetString("status")
	stockResume := co.RawGetString("resume").(*lua.LFunction)
	unpack := L.GetGlobal("unpack")
	selectF := L.GetGlobal("select")
	typeF := L.GetGlobal("type")
	errorF := L.GetGlobal("error")
	p.format = str.RawGetString("format")
	p.tostring = L.GetGlobal("tostring")

	// 2. Strip.
	for _, n := range stripped {
		L.SetGlobal(n, lua.LNil)
	}
	str.RawSetString("dump", lua.LNil)
	str.RawSetString("gfind", lua.LNil)
	math := L.GetGlobal("math").(*lua.LTable)
	math.RawSetString("mod", lua.LNil)
	math.RawSetString("pow", lua.LNil)
	L.SetGlobal("print", L.NewFunction(p.luaPrint))
	L.SetGlobal("_VERSION", lua.LString("Lua 5.1 (gopher-lua)"))

	// 3. Lock the string metatable: setmetatable("x", {...}) would otherwise rewrite the
	// metatable shared by all strings of the state.
	if mt, ok := L.GetMetatable(lua.LString("")).(*lua.LTable); ok {
		mt.RawSetString("__metatable", lua.LString("locked"))
	}

	// 4. Repaired standard library (tonumber, string.rep, table.concat, patterns ...).
	installStdlib(p)

	// 5. Prelude.
	p.tag = L.NewUserData()
	rawYield := L.NewFunction(func(L *lua.LState) int {
		p.yielded = true
		return L.Yield(argsFrom(L, 1)...)
	})
	pubYield := L.NewFunction(func(L *lua.LState) int {
		if L == p.L || p.roots[L] != nil {
			L.RaiseError("attempt to yield from outside a coroutine")
		}
		p.yielded = true
		return L.Yield(argsFrom(L, 1)...)
	})
	rawResume := L.NewFunction(func(L *lua.LState) int { return p.rawResume(L, stockResume) })
	await := L.NewFunction(func(L *lua.LState) int {
		if L == p.L {
			L.RaiseError("async function called outside a task")
		}
		if inGoCall(L) {
			// A Go function (tostring, table.sort, string.gsub ...) is calling this Lua
			// function: a yield cannot cross it and would corrupt the call.
			L.RaiseError("%s", errAwaitInGoCallback)
		}
		p.awaitFrom = L
		p.yielded = true
		vals := append([]lua.LValue{p.tag}, argsFrom(L, 1)...)
		return L.Yield(vals...)
	})
	goT := L.CreateTable(0, 4)
	goT.RawSetString("find", L.NewFunction(p.requireFind))
	goT.RawSetString("done", L.NewFunction(p.requireDone))
	goT.RawSetString("is_go", L.NewFunction(func(L *lua.LState) int {
		f, ok := L.Get(1).(*lua.LFunction)
		L.Push(lua.LBool(ok && f.IsG))
		return 1
	}))
	goT.RawSetString("can_yield", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LBool(L != p.L && p.roots[L] == nil))
		return 1
	}))

	proto, err := internalProto(preludeChunk, preludeSrc)
	if err != nil {
		return err
	}
	L.Push(L.NewFunctionFromProto(proto))
	for _, v := range []lua.LValue{p.tag, rawYield, rawResume, await, create, status, unpack, selectF, typeF, pubYield, errorF, goT} {
		L.Push(v)
	}
	L.Call(12, 1)
	pre := L.Get(-1).(*lua.LTable)
	L.Pop(1)
	p.pre = pre

	L.SetGlobal("pcall", pre.RawGetString("pcall"))
	L.SetGlobal("xpcall", pre.RawGetString("xpcall"))
	co.RawSetString("resume", pre.RawGetString("resume"))
	co.RawSetString("create", pre.RawGetString("create"))
	co.RawSetString("wrap", pre.RawGetString("wrap"))
	co.RawSetString("yield", pre.RawGetString("yield"))
	co.RawSetString("running", L.NewFunction(func(L *lua.LState) int {
		if L == p.L || p.roots[L] != nil {
			L.Push(lua.LNil) // a root task is not a coroutine the plugin may resume
			return 1
		}
		L.Push(L)
		return 1
	}))

	// 6. require and the gvim API.
	L.SetGlobal("require", pre.RawGetString("require"))
	p.installAPI()
	return nil
}

// errAwaitInGoCallback is the error of an async call made inside a function that a Go
// function (not a Lua one) is calling.
const errAwaitInGoCallback = "async call inside a Go callback (table.sort comparator, string.gsub callback, metamethod or for-in iterator function): it must not yield"

// inGoCall reports whether a Go function is on the call stack of L below the running
// function: some Go function called the Lua code that is running.
func inGoCall(L *lua.LState) bool {
	for lvl := 1; ; lvl++ {
		dbg, ok := L.GetStack(lvl)
		if !ok {
			return false
		}
		if _, err := L.GetInfo("S", dbg, lua.LNil); err != nil {
			return false
		}
		if dbg.What == "G" {
			return true
		}
	}
}

// argsFrom returns the arguments of a Go function from index n on.
func argsFrom(L *lua.LState, n int) []lua.LValue {
	top := L.GetTop()
	if top < n {
		return nil
	}
	out := make([]lua.LValue, 0, top-n+1)
	for i := n; i <= top; i++ {
		out = append(out, L.Get(i))
	}
	return out
}

// rawResume is the coroutine resume the prelude drives. It refuses roots and the main
// thread (so a plugin cannot resume a parked task), refuses a thread that is already inside a
// resume (the running one or an ancestor), bounds the nesting depth and refreshes
// the child's context with the caller's: a coroutine created in an earlier slice would
// otherwise die with "context canceled".
func (p *plugin) rawResume(L *lua.LState, stock *lua.LFunction) int {
	th, ok := L.Get(1).(*lua.LState)
	if !ok {
		L.ArgError(1, "coroutine expected")
		return 0
	}
	if th == p.L || p.roots[th] != nil {
		L.RaiseError("cannot resume this thread")
	}
	if p.coDepth >= maxCoDepth {
		L.Push(lua.LFalse)
		L.Push(lua.LString("C stack overflow"))
		return 2
	}
	if _, active := p.resuming[th]; active || th == L {
		L.Push(lua.LFalse)
		L.Push(lua.LString("cannot resume non-suspended coroutine"))
		return 2
	}
	if p.resuming == nil {
		p.resuming = map[*lua.LState]struct{}{}
	}
	p.resuming[th] = struct{}{}
	p.coDepth++
	defer func() {
		p.coDepth--
		delete(p.resuming, th)
	}()
	if ctx := L.Context(); ctx != nil {
		th.SetContext(ctx)
	}
	top := L.GetTop()
	L.Push(stock)
	for i := 1; i <= top; i++ {
		L.Push(L.Get(i))
	}
	L.Call(top, lua.MultRet)
	return L.GetTop() - top
}

// luaPrint replaces print: it writes to the plugin log at info level.
func (p *plugin) luaPrint(L *lua.LState) int {
	n := L.GetTop()
	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	p.r.logf(p.name, levelInfo, strings.Join(parts, "\t"))
	return 0
}

// mkAwait returns the Lua function that performs the async call of kind (value, err).
func (p *plugin) mkAwait(kind string) lua.LValue { return p.makeAwait(kind, "mk", p.mk) }

// mkAwaitV is mkAwait for calls that return any number of values.
func (p *plugin) mkAwaitV(kind string) lua.LValue { return p.makeAwait(kind, "mkv", p.mkv) }

func (p *plugin) makeAwait(kind, factory string, cache map[string]lua.LValue) lua.LValue {
	if f, ok := cache[kind]; ok {
		return f
	}
	L := p.L
	L.Push(p.pre.RawGetString(factory))
	L.Push(lua.LString(kind))
	L.Call(1, 1)
	f := L.Get(-1)
	L.Pop(1)
	cache[kind] = f
	return f
}
