package plugin

import (
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
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
}
