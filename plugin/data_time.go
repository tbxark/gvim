package plugin

import (
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// installTime installs gvim.time (milliseconds since the epoch, from the runtime clock).
//
//	gvim.time.now_ms()                 wall clock, ms since 1970
//	gvim.time.monotonic_ms()           ms since the plugin was loaded; never goes back
//	gvim.time.format(fmt, t_ms?)       strftime subset of os.date; t defaults to now;
//	                                   a leading "!" in fmt means UTC
func installTime(e *apiEnv) {
	r := e.r
	base := r.now()
	t := e.area("time")
	e.set(t, "now_ms", func(L *lua.LState) int {
		L.Push(lua.LNumber(r.now().UnixMilli()))
		return 1
	})
	e.set(t, "monotonic_ms", func(L *lua.LState) int {
		L.Push(lua.LNumber(r.now().Sub(base).Milliseconds()))
		return 1
	})
	e.set(t, "format", func(L *lua.LState) int {
		format := L.CheckString(1)
		if len(format) > MaxDateFormat {
			L.RaiseError("date format too long (max %d bytes)", MaxDateFormat)
		}
		tm := r.now()
		if L.Get(2) != lua.LNil {
			ms := clampF(float64(L.CheckNumber(2)))
			tm = time.UnixMilli(int64(ms))
		}
		if strings.HasPrefix(format, "!") {
			tm, format = tm.UTC(), format[1:]
		} else {
			tm = tm.Local()
		}
		out, err := strftime(tm, format)
		if err != nil {
			L.RaiseError("%s", err.Error())
		}
		L.Push(lua.LString(out))
		return 1
	})
}

// installTheme installs gvim.theme: name(), color(key) -> {r=,g=,b=} | nil, is_dark().
func installTheme(e *apiEnv) {
	r := e.r
	t := e.area("theme")
	e.set(t, "name", func(L *lua.LState) int {
		L.Push(lua.LString(r.guard.Theme().Name))
		return 1
	})
	e.set(t, "is_dark", func(L *lua.LState) int {
		L.Push(lua.LBool(r.guard.Theme().Dark))
		return 1
	})
	e.set(t, "color", func(L *lua.LState) int {
		cr, cg, cb, ok := r.guard.ThemeColor(L.CheckString(1))
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		c := L.CreateTable(0, 3)
		c.RawSetString("r", lua.LNumber(cr))
		c.RawSetString("g", lua.LNumber(cg))
		c.RawSetString("b", lua.LNumber(cb))
		L.Push(c)
		return 1
	})
	e.set(t, "builtin_themes", func(L *lua.LState) int {
		names := r.guard.ThemeNames()
		out := L.CreateTable(len(names), 0)
		for _, name := range names {
			out.Append(lua.LString(name))
		}
		L.Push(out)
		return 1
	})
	// gvim.theme.override{ ["section.key"] = "#RRGGBB", ... } layers session color
	// overrides over the active theme; gvim.theme.clear() drops them. The editor keeps them
	// until the next theme change (which clears them) and rebuilds its styles at once.
	e.set(t, "override", func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		over := make(map[string]string, tbl.Len())
		var bad string
		n := 0
		tbl.ForEach(func(k, v lua.LValue) {
			if bad != "" {
				return
			}
			ks, ok := k.(lua.LString)
			if !ok {
				bad = "a theme override key must be a string"
				return
			}
			vs, ok := v.(lua.LString)
			if !ok {
				bad = "a theme override value must be a color string"
				return
			}
			if len(ks) > maxThemeOverrideKey || len(vs) > maxThemeOverrideValue {
				bad = "a theme override key or value is too long"
				return
			}
			n++
			if n > maxThemeOverrides {
				bad = "too many theme overrides"
				return
			}
			over[string(ks)] = string(vs)
		})
		if bad != "" {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "%s", bad))
		}
		return bufTrue(L, r.guard.SetThemeOverrides(over))
	})
	e.set(t, "clear", func(L *lua.LState) int {
		return bufTrue(L, r.guard.SetThemeOverrides(nil))
	})
	e.set(t, "overrides", func(L *lua.LState) int {
		over, _ := r.guard.ThemeOverrides()
		out := L.CreateTable(0, len(over))
		for k, v := range over {
			out.RawSetString(k, lua.LString(v))
		}
		L.Push(out)
		return 1
	})

	// Theme files (tier 1b): the theme editor and the theme pickers a port needs.
	e.set(t, "user_dir", func(L *lua.LState) int {
		L.Push(lua.LString(r.guard.ThemeUserDir()))
		return 1
	})
	e.set(t, "file_exists", func(L *lua.LState) int {
		L.Push(lua.LBool(r.guard.ThemeFileExists(L.CheckString(1))))
		return 1
	})
	e.set(t, "data", func(L *lua.LState) int {
		data, err := r.guard.ThemeData(L.CheckString(1))
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		L.Push(lua.LString(data))
		return 1
	})
	e.set(t, "all", func(L *lua.LState) int {
		files := r.guard.ThemeFiles()
		out := L.CreateTable(len(files), 0)
		for _, f := range files {
			row := L.CreateTable(0, 3)
			row.RawSetString("name", lua.LString(f.Name))
			row.RawSetString("ref", lua.LString(f.Ref))
			row.RawSetString("builtin", lua.LBool(f.Builtin))
			out.Append(row)
		}
		L.Push(out)
		return 1
	})
	e.set(t, "save_file", func(L *lua.LState) int {
		name := L.CheckString(1)
		data := L.CheckString(2)
		if len(data) > maxThemeFileBytes {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "a theme file is at most %d KiB", maxThemeFileBytes>>10))
		}
		ref, err := r.guard.SaveThemeFile(name, data)
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		L.Push(lua.LString(ref))
		return 1
	})
	e.set(t, "delete_file", func(L *lua.LState) int {
		return bufTrue(L, r.guard.DeleteThemeFile(L.CheckString(1)))
	})
	e.set(t, "reload", func(L *lua.LState) int {
		ref := ""
		if v := L.Get(1); v != lua.LNil {
			ref = L.CheckString(1)
		}
		return bufTrue(L, r.guard.ReloadThemes(ref))
	})
	e.set(t, "schema", func(L *lua.LState) int {
		schema := r.guard.ThemeSchema()
		out := L.CreateTable(0, 3)
		keys := L.CreateTable(len(schema.Keys), 0)
		for _, k := range schema.Keys {
			row := L.CreateTable(0, 3)
			row.RawSetString("name", lua.LString(k.Name))
			row.RawSetString("kind", lua.LString(k.Kind))
			if k.Fallback != "" {
				row.RawSetString("fallback", lua.LString(k.Fallback))
			}
			keys.Append(row)
		}
		out.RawSetString("keys", keys)
		mods := L.CreateTable(len(schema.ModifierOnly), 0)
		for _, m := range schema.ModifierOnly {
			mods.Append(lua.LString(m))
		}
		out.RawSetString("modifier_only", mods)
		names := L.CreateTable(len(schema.NamedColors), 0)
		for _, n := range schema.NamedColors {
			names.Append(lua.LString(n))
		}
		out.RawSetString("named_colors", names)
		L.Push(out)
		return 1
	})
}

// maxThemeFileBytes bounds a theme file a plugin writes.
const maxThemeFileBytes = 1 << 20

// Caps of a session theme override set.
const (
	maxThemeOverrides     = 64
	maxThemeOverrideKey   = 64
	maxThemeOverrideValue = 64
)
