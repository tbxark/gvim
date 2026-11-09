package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.plugins (stage S9):
//
//	gvim.plugins.list() -> { {name, status, version, source}, ... }
//	gvim.plugins.export(tbl) -> true     expose the FUNCTIONS of tbl to other plugins; any other
//	                                     value must be plain data (it is copied)
//	gvim.plugins.get(name) -> proxy | nil
//
// Every call through a proxy is ⏳. The arguments are deep-copied plain data (functions,
// userdata and so Buffer and Window handles are rejected; pass an id), the exported function runs as a task in the EXPORTING plugin's
// state, and its results are copied back: no Lua value is ever shared between two states.
// The proxy holds only the plugin name; the exporter is resolved at call time, so a call
// to a plugin that is not loaded returns `nil, "not_found: plugin 'x' is not loaded"`, and
// unloading or reloading the exporter resolves calls in flight with
// `nil, "closed: plugin reloaded"`. A failure inside the exported function returns
// `nil, "plugin 'x': <message>"`. Calls nest at most maxXDepth deep.
const (
	maxXDepth  = 4
	maxExports = 256
)

// plainData reports an error when v, as converted by fromLua, holds a Buffer or Window
// handle: nothing but plain data crosses between plugins (FINAL 5.11).
func plainData(v any) error {
	switch x := v.(type) {
	case pluginapi.BufferID, pluginapi.WinID:
		return invalidErr("a handle cannot be passed to another plugin (pass its id)")
	case []any:
		for _, e := range x {
			if err := plainData(e); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, e := range x {
			if err := plainData(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func init() {
	registerAwaitV("plugin_call", awaitPluginCall)
}

const proxyLua = `
local call, lookup, exists, setmetatable, error, select, unpack = ...
local function pack(...) return { n = select("#", ...), ... } end
return function(name)
  if not exists(name) then return nil end
  local cache = {}
  return setmetatable({}, {
    __index = function(_, key)
      local kind, v = lookup(name, key)
      if kind == "fn" then
        local f = cache[key]
        if not f then
          f = function(...)
            local r = pack(call(name, key, ...))
            return unpack(r, 1, r.n)
          end
          cache[key] = f
        end
        return f
      end
      return v
    end,
    __newindex = function() error("plugin proxies are read-only", 2) end,
    __tostring = function() return "Plugin(" .. name .. ")" end,
    __metatable = "locked",
  })
end
`

func installPlugins(e *apiEnv) {
	p, r := e.p, e.r
	t := e.area("plugins")

	e.set(t, "list", func(L *lua.LState) int {
		infos := r.Plugins()
		out := L.CreateTable(len(infos), 0)
		for _, in := range infos {
			row := L.CreateTable(0, 4)
			row.RawSetString("name", lua.LString(in.Name))
			row.RawSetString("status", lua.LString(in.Status))
			row.RawSetString("version", lua.LString(in.Version))
			row.RawSetString("source", lua.LString(in.Source))
			out.Append(row)
		}
		L.Push(out)
		return 1
	})
	e.set(t, "export", func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		fns := map[string]lua.LValue{}
		data := map[string]any{}
		for k, v := tbl.Next(lua.LNil); k != lua.LNil; k, v = tbl.Next(k) {
			key, ok := k.(lua.LString)
			if !ok {
				L.ArgError(1, "export keys must be strings")
			}
			if len(fns)+len(data) >= maxExports {
				L.ArgError(1, "too many exports")
			}
			if fn, isFn := v.(*lua.LFunction); isFn {
				fns[string(key)] = fn
				continue
			}
			goVal, err := p.fromLua(v)
			if err == nil {
				err = plainData(goVal)
			}
			if err != nil {
				L.ArgError(1, "export '"+string(key)+"': "+err.Error()+" (only functions and plain data can be exported)")
			}
			data[string(key)] = goVal
		}
		if p.data.exports == nil {
			p.addCleanup(func() { p.data.exports, p.data.exportsData = nil, nil })
		}
		p.data.exports, p.data.exportsData = fns, data
		L.Push(lua.LTrue)
		return 1
	})

	exists := e.fn(func(L *lua.LState) int {
		target := r.plugins[L.CheckString(1)]
		L.Push(lua.LBool(target != nil && !target.dead && target.L != nil))
		return 1
	})
	lookup := e.fn(func(L *lua.LState) int {
		target := r.plugins[L.CheckString(1)]
		key := L.CheckString(2)
		if target == nil || target.dead || target.L == nil {
			// Not loaded (any more): every member looks like a function, so calling it
			// reports not_found instead of failing with "attempt to call a nil value".
			L.Push(lua.LString("fn"))
			return 1
		}
		if target.dashboardFunction(key) != nil {
			// Only the bundled dashboard plugin may call a section callback key. Other
			// plugins must not even see a callable proxy for it.
			if !isDashboardConsumer(p) {
				L.Push(lua.LNil)
				return 1
			}
			L.Push(lua.LString("fn"))
			return 1
		}
		if target.exportedFunction(key) != nil {
			L.Push(lua.LString("fn"))
			return 1
		}
		if v, ok := target.data.exportsData[key]; ok {
			L.Push(lua.LString("data"))
			L.Push(p.toLua(v))
			return 2
		}
		L.Push(lua.LNil)
		return 1
	})
	res := e.runLua("gvim/plugins.lua", proxyLua, 1, e.asyncV("plugin_call"), lookup, exists,
		e.L.GetGlobal("setmetatable"), e.L.GetGlobal("error"), e.L.GetGlobal("select"), e.L.GetGlobal("unpack"))
	t.RawSetString("get", res[0])
}

// awaitPluginCall serves proxy.fn(...): c.args = name, key, args...
func awaitPluginCall(c *awaitCall) {
	name, ok1 := argString(c.args, 0)
	key, ok2 := argString(c.args, 1)
	if !ok1 || !ok2 {
		c.Bad(1, "call", "plugin name and function name expected")
		return
	}
	r, caller := c.r, c.p
	target := r.plugins[name]
	if target == nil || target.dead || target.L == nil {
		c.Fail(notFoundErr("plugin '%s' is not loaded", name))
		return
	}
	fn := target.exportedFunction(key)
	if fn == nil {
		c.Fail(notFoundErr("plugin '%s' does not export a function '%s'", name, key))
		return
	}
	dashboardCallback := target.dashboardFunction(key) != nil
	if dashboardCallback && !isDashboardConsumer(caller) {
		c.Fail(invalidErr("dashboard section callbacks may only be called by the bundled dashboard plugin"))
		return
	}
	if c.t.xdepth >= maxXDepth {
		c.Fail(limitErr("plugin calls nested deeper than %d", maxXDepth))
		return
	}
	args := make([]any, 0, len(c.args)-2)
	for i, a := range c.args[2:] {
		v, err := caller.fromLua(a)
		if err == nil {
			err = plainData(v)
		}
		if err != nil {
			c.Fail(invalidErr("argument #%d: %s", i+1, err.Error()))
			return
		}
		args = append(args, v)
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	o := c.t.origin
	if o == originLoad {
		o = originCallback
	}
	t2, err := r.newTask(target, o, fn, "call "+name+"."+key)
	if err != nil {
		c.Fail(err)
		return
	}
	t2.xdepth = c.t.xdepth + 1
	vals := make([]lua.LValue, len(args))
	for i, a := range args {
		vals[i] = target.toLua(a)
	}
	tok := c.NewToken()
	t2.waiters = append(t2.waiters, tok)
	r.enqueue(item{t: t2, args: vals})
	c.Park(&parkSpec{
		Token:  tok,
		Target: name,
		Convert: func(comp pluginapi.Completion) []lua.LValue {
			if comp.Err != "" {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(comp.Err)}
			}
			fail := func(msg string) []lua.LValue { return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(msg)} }
			switch {
			case t2.failed:
				return fail("plugin '" + name + "': " + errorText(t2.errv))
			case t2.cancelled:
				return fail(string(pluginapi.CodeCancelled) + ": call to plugin '" + name + "' cancelled")
			case target.dead || target.L == nil:
				return fail(string(pluginapi.CodeClosed) + ": plugin reloaded")
			}
			out := make([]lua.LValue, 1, 1+len(t2.results))
			out[0] = lua.LTrue
			for _, v := range t2.results {
				goVal, err := target.fromLua(v)
				if err == nil {
					err = plainData(goVal)
				}
				if err != nil {
					return fail(errString(invalidErr("result: %s", err.Error())))
				}
				if dashboardCallback {
					if err := validateDashboardResult(goVal); err != nil {
						return fail(errString(err))
					}
				}
				out = append(out, caller.toLua(goVal))
			}
			return out
		},
	})
}

func argString(args []lua.LValue, i int) (string, bool) {
	if i < len(args) {
		if s, ok := args[i].(lua.LString); ok {
			return string(s), true
		}
	}
	return "", false
}
