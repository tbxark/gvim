package plugin

import (
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// installState installs persistent state and the well-known directories: gvim.state and
// gvim.dirs. State lives in memory under the plugin's name, so it survives a
// reload; the writer chain in ioruntime.go persists it behind the scenes.
func installState(e *apiEnv) {
	p, r := e.p, e.r
	st := e.area("state")
	entry := func() *stateEntry { return r.io().state(p.name) }
	checkKey := func(L *lua.LState) string {
		k := L.CheckString(1)
		if k == "" || len(k) > maxStateKey || !utf8.ValidString(k) {
			L.ArgError(1, "key must be a non-empty UTF-8 string of at most 256 bytes")
		}
		return k
	}

	e.set(st, "get", func(L *lua.LState) int {
		k := checkKey(L)
		raw, ok := entry().vals[k]
		if !ok {
			L.Push(L.Get(2))
			return 1
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			L.Push(L.Get(2))
			return 1
		}
		L.Push(p.toLua(v))
		return 1
	})
	e.set(st, "set", func(L *lua.LState) int {
		k := checkKey(L)
		if L.Get(2) == lua.LNil {
			entry().delete(k)
			L.Push(lua.LTrue)
			return 1
		}
		v, err := p.fromLua(L.Get(2))
		if err != nil {
			return bufRet(L, err)
		}
		if err := checkJSON(v, 0); err != nil {
			return bufRet(L, err)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "%v", err))
		}
		if err := entry().set(k, raw); err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LTrue)
		return 1
	})
	e.set(st, "delete", func(L *lua.LState) int {
		entry().delete(checkKey(L))
		L.Push(lua.LTrue)
		return 1
	})
	e.set(st, "keys", func(L *lua.LState) int {
		vals := entry().vals
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		L.Push(p.toLua(keys))
		return 1
	})

	d := e.area("dirs")
	dir := func(name string, f func(hostos.Dirs) string) {
		e.set(d, name, func(L *lua.LState) int {
			L.Push(lua.LString(f(r.os.Dirs())))
			return 1
		})
	}
	dir("config", func(d hostos.Dirs) string { return d.Config })
	dir("data", func(d hostos.Dirs) string {
		if d.Config == "" {
			return ""
		}
		return filepath.Join(d.Config, "plugin-data", p.name)
	})
	e.set(d, "plugin", func(L *lua.LState) int {
		L.Push(lua.LString(p.dir))
		return 1
	})
	dir("root", func(d hostos.Dirs) string { return d.Root })
	dir("cwd", func(d hostos.Dirs) string { return d.Cwd })
	dir("home", func(d hostos.Dirs) string { return d.Home })
	dir("temp", func(d hostos.Dirs) string { return d.Temp })
}

// checkJSON verifies that a value from fromLua survives a JSON round trip: only strings
// of valid UTF-8, finite numbers, booleans, lists and objects (handles and the like are
// refused). json.Marshal would silently repair an invalid string.
func checkJSON(v any, depth int) error {
	if depth > MaxJSONDepth {
		return pluginapi.Errf(pluginapi.CodeInvalid, "value is nested too deeply")
	}
	switch x := v.(type) {
	case nil, bool, int64:
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "NaN and infinity cannot be stored")
		}
	case string:
		if !utf8.ValidString(x) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "strings must be valid UTF-8")
		}
	case []any:
		for _, e := range x {
			if err := checkJSON(e, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, e := range x {
			if !utf8.ValidString(k) {
				return pluginapi.Errf(pluginapi.CodeInvalid, "keys must be valid UTF-8")
			}
			if err := checkJSON(e, depth+1); err != nil {
				return err
			}
		}
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "only JSON values can be stored")
	}
	return nil
}
