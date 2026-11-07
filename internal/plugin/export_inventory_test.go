package plugin

import (
	"sort"

	lua "github.com/yuin/gopher-lua"
)

// Inventory lists what a plugin state exposes, for the stubs drift test: the functions
// reachable from the gvim table (dotted paths, "gvim.buf.current"), the non-function
// leaves ("gvim.api_version"), and the methods of every handle kind ("Buffer:line").
type Inventory struct {
	Funcs, Values, Methods []string
}

// APIInventory walks the gvim table and the handle kinds of the named loaded plugin. The
// bundled library modules (gvim.git) are part of the walk once the plugin has touched them.
func APIInventory(r *Runtime, plugin string) (Inventory, bool) {
	p := r.plugins[plugin]
	if p == nil {
		return Inventory{}, false
	}
	var inv Inventory
	seen := map[*lua.LTable]bool{}
	var walk func(prefix string, t *lua.LTable)
	walk = func(prefix string, t *lua.LTable) {
		if seen[t] {
			return
		}
		seen[t] = true
		t.ForEach(func(k, v lua.LValue) {
			name, ok := k.(lua.LString)
			if !ok {
				return
			}
			path := prefix + "." + string(name)
			switch x := v.(type) {
			case *lua.LFunction:
				inv.Funcs = append(inv.Funcs, path)
			case *lua.LTable:
				walk(path, x)
			case *lua.LNilType:
			default:
				inv.Values = append(inv.Values, path)
			}
		})
	}
	walk("gvim", p.gvim)
	for name, k := range p.kinds {
		k.methods.ForEach(func(key, v lua.LValue) {
			if s, ok := key.(lua.LString); ok {
				inv.Methods = append(inv.Methods, name+":"+string(s))
			}
		})
	}
	sort.Strings(inv.Funcs)
	sort.Strings(inv.Values)
	sort.Strings(inv.Methods)
	return inv, true
}
