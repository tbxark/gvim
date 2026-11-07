package plugin

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/plugin/bundled"
)

// maxRequireDepth bounds nested module loads.
const maxRequireDepth = 16

// moduleNameOK checks the charset [A-Za-z0-9_.-], no "..", no leading or trailing dot.
func moduleNameOK(name string) bool {
	if name == "" || len(name) > 128 || strings.Contains(name, "..") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.', c == '-':
		default:
			return false
		}
	}
	return true
}

// requireFind is go.find of the prelude's require. For a module that is already loaded (or
// is being loaded: a cycle yields true) it returns nil and the value; otherwise it
// returns the loader function, which the prelude runs in Lua so a module may await.
//
// Resolution order (FINAL 8.3): (1) host modules "gvim" and "gvim.<area>", also the
// bundled library modules "gvim.<name>" (cannot be overridden); (2) plugin-local files
// lua/a/b.lua, lua/a/b/init.lua, a/b.lua, a/b/init.lua from the compiled file map of the
// plugin; (3) nothing else: no package.path, no cross-plugin require, no filesystem.
func (p *plugin) requireFind(L *lua.LState) int {
	name := L.CheckString(1)
	if !moduleNameOK(name) {
		L.RaiseError("invalid module name %q", name)
	}
	if v, ok := p.loaded[name]; ok {
		L.Push(lua.LNil)
		L.Push(v)
		return 2
	}
	if p.loading[name] {
		L.Push(lua.LNil)
		L.Push(lua.LTrue)
		return 2
	}
	if v := p.hostModule(name); v != nil {
		p.loaded[name] = v
		L.Push(lua.LNil)
		L.Push(v)
		return 2
	}
	if p.reqDepth >= maxRequireDepth {
		L.RaiseError("module nesting too deep (limit %d) at %q", maxRequireDepth, name)
	}
	proto, err := p.moduleProto(name)
	if err != nil {
		L.RaiseError("%s", err.Error())
	}
	p.loading[name] = true
	p.reqDepth++
	L.Push(L.NewFunctionFromProto(proto))
	return 1
}

// requireDone is go.done: bookkeeping after the loader ran. It returns the value to cache.
func (p *plugin) requireDone(L *lua.LState) int {
	name := L.CheckString(1)
	ok := L.ToBool(2)
	delete(p.loading, name)
	p.reqDepth--
	if !ok {
		return 0
	}
	v := L.Get(3)
	if v == lua.LNil {
		v = lua.LTrue
	}
	p.loaded[name] = v
	L.Push(v)
	return 1
}

// hostModule returns the gvim table or area for "gvim" / "gvim.<x>", or nil. Library
// modules written in Lua ("gvim.git") are loaded through moduleProto.
func (p *plugin) hostModule(name string) lua.LValue {
	if name == "gvim" {
		return p.gvim
	}
	if area, ok := strings.CutPrefix(name, "gvim."); ok {
		if t := p.areas[area]; t != nil {
			return t
		}
	}
	return nil
}

// moduleProto finds the prototype of a module that is not a host table: a bundled library
// ("gvim.<name>") or a plugin-local file.
func (p *plugin) moduleProto(name string) (*lua.FunctionProto, error) {
	if lib, ok := strings.CutPrefix(name, "gvim."); ok {
		src, found := bundled.Lib(lib)
		if !found {
			return nil, fmt.Errorf("module %q not found", name)
		}
		return internalProto("gvim/lib/"+lib+".lua", src)
	}
	rel := strings.ReplaceAll(name, ".", "/")
	for _, cand := range []string{"lua/" + rel + ".lua", "lua/" + rel + "/init.lua", rel + ".lua", rel + "/init.lua"} {
		c := p.files[cand]
		if c == nil {
			continue
		}
		if c.Err != nil {
			return nil, fmt.Errorf("module %q failed to compile: %v", name, c.Err)
		}
		return c.Proto, nil
	}
	return nil, fmt.Errorf("module %q not found in plugin %q", name, p.name)
}

// libIndex is the __index of the gvim table: a field that is not set yet is looked up as a
// bundled library module ("gvim.git") and loaded on first use, so a new library is a new
// file under bundled/lib and no edit of this package.
func (p *plugin) libIndex(L *lua.LState) int {
	key, ok := L.Get(2).(lua.LString)
	if !ok || !moduleNameOK(string(key)) {
		L.Push(lua.LNil)
		return 1
	}
	if _, found := bundled.Lib(string(key)); !found {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(p.pre.RawGetString("require"))
	L.Push(lua.LString("gvim." + string(key)))
	L.Call(1, 1)
	v := L.Get(-1)
	L.Pop(1)
	p.gvim.RawSetString(string(key), v)
	L.Push(v)
	return 1
}
