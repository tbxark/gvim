package plugin

import (
	"fmt"
	"math"
	"sort"

	lua "github.com/yuin/gopher-lua"
)

// gvim.settings and gvim.config.
//
// gvim.settings.define(schema) declares the plugin's settings. The effective value of a
// key is its default, overridden by plugins.<name>.settings.<key> of the user config, and
// (AMENDMENTS C11) overridden again by the project config only for keys declared
// project = true. Every value is type-checked; a bad value keeps the one below it and logs
// a warning. Project settings are untrusted input: a plugin must never use one as a command
// line, a path to delete, a URL or shell text without validating it.
//
// gvim.config.get(key?) is a read-only view of the allowlisted effective editor settings.

// ConfigKeys are the editor settings gvim.config.get exposes. The host may refuse a key it
// does not know; the runtime never asks for a key outside this list.
var ConfigKeys = []string{
	"theme", "tab_size", "insert_spaces", "line_ending", "trim_trailing_whitespace",
	"ensure_final_newline", "auto_indent", "line_numbers", "line_wrap", "highlight_current_line",
	"show_whitespace", "cursor_style", "auto_save_enabled", "auto_save_interval_secs",
}

const maxSettingKeys = 128

type settingSpec struct {
	key, typ string
	def      any
	min, max *float64
	values   []string // enum
	project  bool
}

// settingsState is the schema and the effective values of one plugin.
type settingsState struct {
	specs  map[string]*settingSpec
	keys   []string // sorted
	values map[string]any
}

var settingTypes = map[string]bool{"boolean": true, "integer": true, "number": true, "string": true, "enum": true, "string_list": true}

// check validates v against spec and returns it in canonical form (bool, float64, string,
// []string).
func (s *settingSpec) check(v any) (any, error) {
	switch s.typ {
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("expected a boolean")
	case "integer", "number":
		var f float64
		if n, ok := wholeNumber(v); ok {
			f = float64(n)
		} else if x, ok := v.(float64); ok && s.typ == "number" && !math.IsNaN(x) && !math.IsInf(x, 0) {
			f = x
		} else {
			return nil, fmt.Errorf("expected %s", map[string]string{"integer": "an integer", "number": "a number"}[s.typ])
		}
		if s.min != nil && f < *s.min {
			return nil, fmt.Errorf("%v is below the minimum %v", f, *s.min)
		}
		if s.max != nil && f > *s.max {
			return nil, fmt.Errorf("%v is above the maximum %v", f, *s.max)
		}
		return f, nil
	case "string":
		if str, ok := v.(string); ok {
			return str, nil
		}
		return nil, fmt.Errorf("expected a string")
	case "enum":
		if str, ok := v.(string); ok {
			for _, c := range s.values {
				if c == str {
					return str, nil
				}
			}
		}
		return nil, fmt.Errorf("expected one of %v", s.values)
	case "string_list":
		var list []string
		switch x := v.(type) {
		case []string:
			list = append(list, x...)
		case []any:
			for _, el := range x {
				str, ok := el.(string)
				if !ok {
					return nil, fmt.Errorf("expected a list of strings")
				}
				list = append(list, str)
			}
		default:
			return nil, fmt.Errorf("expected a list of strings")
		}
		return list, nil
	}
	return nil, fmt.Errorf("unknown type")
}

// luaValue converts a canonical setting value to Lua (a fresh table for lists).
func (p *plugin) settingLua(s *settingSpec, v any) lua.LValue {
	switch x := v.(type) {
	case bool:
		return lua.LBool(x)
	case float64:
		return lua.LNumber(x)
	case string:
		return lua.LString(x)
	case []string:
		t := p.L.CreateTable(len(x), 0)
		for _, el := range x {
			t.Append(lua.LString(el))
		}
		return t
	}
	return lua.LNil
}

// parseSpec reads one entry of a define schema.
func (p *plugin) parseSpec(L *lua.LState, key string, t *lua.LTable) *settingSpec {
	bad := func(format string, args ...any) {
		L.ArgError(1, fmt.Sprintf("setting '%s': %s", key, fmt.Sprintf(format, args...)))
	}
	s := &settingSpec{key: key}
	typ, ok := t.RawGetString("type").(lua.LString)
	if !ok || !settingTypes[string(typ)] {
		bad("type must be one of boolean, integer, number, string, enum, string_list")
	}
	s.typ = string(typ)
	num := func(field string) *float64 {
		switch v := t.RawGetString(field).(type) {
		case *lua.LNilType:
			return nil
		case lua.LNumber:
			f := float64(v)
			return &f
		}
		bad("%s must be a number", field)
		return nil
	}
	if s.typ == "integer" || s.typ == "number" {
		s.min, s.max = num("minimum"), num("maximum")
	}
	if s.typ == "enum" {
		vals, ok := t.RawGetString("values").(*lua.LTable)
		if !ok || vals.Len() == 0 {
			bad("an enum needs a non-empty values list")
		}
		for i := 1; i <= vals.Len(); i++ {
			str, ok := vals.RawGetInt(i).(lua.LString)
			if !ok {
				bad("enum values must be strings")
			}
			s.values = append(s.values, string(str))
		}
	}
	switch v := t.RawGetString("project").(type) {
	case *lua.LNilType:
	case lua.LBool:
		s.project = bool(v)
	default:
		bad("project must be a boolean")
	}
	if dv := t.RawGetString("default"); dv != lua.LNil {
		goVal, err := p.fromLua(dv)
		if err != nil {
			bad("default: %v", err)
		}
		if s.def, err = s.check(goVal); err != nil {
			bad("default: %v", err)
		}
	} else {
		switch s.typ {
		case "boolean":
			s.def = false
		case "integer", "number":
			z := 0.0
			if s.min != nil && *s.min > z {
				z = *s.min
			}
			if s.max != nil && *s.max < z {
				z = *s.max
			}
			s.def = z
		case "string":
			s.def = ""
		case "enum":
			s.def = s.values[0]
		default:
			s.def = []string(nil)
		}
	}
	return s
}

// compute fills st.values from the defaults and the two config layers.
func (p *plugin) computeSettings(st *settingsState) {
	st.values = make(map[string]any, len(st.specs))
	for _, k := range st.keys {
		st.values[k] = st.specs[k].def
	}
	for _, k := range st.keys {
		s := st.specs[k]
		if raw, ok := p.data.user[k]; ok {
			if v, err := s.check(raw); err == nil {
				st.values[k] = v
			} else {
				p.r.logf(p.name, levelWarn, fmt.Sprintf("setting '%s': %v; using the default", k, err))
			}
		}
		if raw, ok := p.data.project[k]; ok && s.project {
			if v, err := s.check(raw); err == nil {
				st.values[k] = v
			} else {
				p.r.logf(p.name, levelWarn, fmt.Sprintf("project setting '%s': %v; ignored", k, err))
			}
		}
	}
	var dropped []string
	for k := range p.data.project {
		if s := st.specs[k]; s == nil || !s.project {
			dropped = append(dropped, k)
		}
	}
	sort.Strings(dropped)
	for _, k := range dropped {
		p.r.logf(p.name, levelWarn, fmt.Sprintf("project setting '%s' ignored (not project=true)", k))
	}
}

// UpdateSettings replaces the config settings of a plugin (a config reload). The loader
// keeps them for the plugin's next reload or enable, and a loaded plugin recomputes the
// values of gvim.settings; the live table follows.
func (r *Runtime) UpdateSettings(name string, user, project map[string]any) {
	defer r.enter("UpdateSettings")()
	r.ld.updateSettings(name, PluginSettings{User: user, Project: project})
	p := r.plugins[name]
	if p == nil || p.dead {
		return
	}
	p.data.user, p.data.project = user, project
	if st := p.data.settings; st != nil {
		p.computeSettings(st)
	}
}

func installSettings(e *apiEnv) {
	p := e.p
	t := e.area("settings")

	e.set(t, "define", func(L *lua.LState) int {
		schema := L.CheckTable(1)
		if p.data.settings != nil {
			L.RaiseError("gvim.settings.define was already called")
		}
		st := &settingsState{specs: map[string]*settingSpec{}}
		for k, v := schema.Next(lua.LNil); k != lua.LNil; k, v = schema.Next(k) {
			key, ok := k.(lua.LString)
			spec, isT := v.(*lua.LTable)
			if !ok || key == "" || !isT {
				L.ArgError(1, "the schema maps setting names to tables")
			}
			if len(st.specs) >= maxSettingKeys {
				L.ArgError(1, fmt.Sprintf("more than %d settings", maxSettingKeys))
			}
			st.specs[string(key)] = p.parseSpec(L, string(key), spec)
			st.keys = append(st.keys, string(key))
		}
		sort.Strings(st.keys)
		p.computeSettings(st)
		p.data.settings = st
		p.addCleanup(func() { p.data.settings = nil })

		live := L.NewTable()
		mt := L.NewTable()
		mt.RawSetString("__index", L.NewFunction(func(L *lua.LState) int {
			key, ok := L.Get(2).(lua.LString)
			if cur := p.data.settings; ok && cur != nil {
				if s := cur.specs[string(key)]; s != nil {
					L.Push(p.settingLua(s, cur.values[string(key)]))
					return 1
				}
			}
			L.Push(lua.LNil)
			return 1
		}))
		mt.RawSetString("__newindex", L.NewFunction(func(L *lua.LState) int {
			L.RaiseError("settings are read-only")
			return 0
		}))
		mt.RawSetString("__metatable", lua.LString("locked"))
		L.SetMetatable(live, mt)
		L.Push(live)
		return 1
	})
	e.set(t, "get", func(L *lua.LState) int {
		out := L.NewTable()
		if st := p.data.settings; st != nil {
			for _, k := range st.keys {
				out.RawSetString(k, p.settingLua(st.specs[k], st.values[k]))
			}
		}
		L.Push(out)
		return 1
	})
}

func installConfig(e *apiEnv) {
	p, r := e.p, e.r
	t := e.area("config")
	allowed := map[string]bool{}
	for _, k := range ConfigKeys {
		allowed[k] = true
	}
	e.set(t, "get", func(L *lua.LState) int {
		if L.Get(1) == lua.LNil {
			out := L.NewTable()
			for _, k := range ConfigKeys {
				if v, ok := r.guard.Config(k); ok {
					out.RawSetString(k, p.toLua(v))
				}
			}
			L.Push(out)
			return 1
		}
		key := L.CheckString(1)
		if !allowed[key] {
			return errReturn(L, notFoundErr("unknown config key '%s'", key))
		}
		v, ok := r.guard.Config(key)
		if !ok {
			return errReturn(L, notFoundErr("config key '%s' is not available", key))
		}
		L.Push(p.toLua(v))
		return 1
	})
}
