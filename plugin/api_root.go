package plugin

import (
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// maxUnloadFns bounds gvim.plugin.on_unload registrations.
const maxUnloadFns = 16

var allCodes = map[string]bool{
	string(pluginapi.CodeReadOnly): true, string(pluginapi.CodeClosed): true, string(pluginapi.CodeTooLarge): true,
	string(pluginapi.CodeInvalid): true, string(pluginapi.CodeNotFound): true, string(pluginapi.CodeBusy): true,
	string(pluginapi.CodeTimeout): true, string(pluginapi.CodeCancelled): true, string(pluginapi.CodeIO): true,
	string(pluginapi.CodeExec): true, string(pluginapi.CodeUnsupported): true, string(pluginapi.CodeConflict): true,
	string(pluginapi.CodeLimit): true, string(pluginapi.CodeStale): true,
}

// installRoot installs the gvim root: api_version version platform has err_code log notify
// status plugin pos range pos_cmp range_eq.
func installRoot(e *apiEnv) {
	p, g := e.p, e.gvim
	g.RawSetString("api_version", lua.LNumber(APIVersion))
	g.RawSetString("version", lua.LString(p.r.version))
	g.RawSetString("platform", lua.LString(p.r.platform))

	e.set(g, "has", func(L *lua.LState) int {
		name := L.CheckString(1)
		if name == "git" { // the binary can appear or vanish while gvim runs: look each time
			_, found := p.r.os.LookPath("git")
			L.Push(lua.LBool(found))
			return 1
		}
		if name == "lsp" { // a language server is configured, which the host says by answering
			_, err := p.r.guard.Diagnostics(0)
			L.Push(lua.LBool(err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported))
			return 1
		}
		L.Push(lua.LBool(p.feature[name]))
		return 1
	})
	e.set(g, "err_code", func(L *lua.LState) int {
		s, ok := L.Get(1).(lua.LString)
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		code, _, found := strings.Cut(string(s), ": ")
		if !found || !allCodes[code] {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LString(code))
		return 1
	})

	// gvim.log.<level>(fmt, ...)
	logT := e.L.CreateTable(0, 4)
	for _, lvl := range []string{levelDebug, levelInfo, levelWarn, levelError} {
		lvl := lvl
		e.set(logT, lvl, func(L *lua.LState) int {
			p.logLine(L, lvl)
			return 0
		})
	}
	g.RawSetString("log", logT)

	e.set(g, "notify", func(L *lua.LState) int {
		level := pluginapi.LevelInfo
		if v := L.Get(2); v != lua.LNil {
			s, ok := v.(lua.LString)
			if !ok || (s != "info" && s != "warn" && s != "error") {
				L.ArgError(2, `level must be "info", "warn" or "error"`)
			}
			level = pluginapi.Level(s)
		}
		p.notifyUser(level, L.CheckString(1))
		L.Push(lua.LTrue)
		return 1
	})
	e.set(g, "status", func(L *lua.LState) int {
		p.notifyUser(pluginapi.LevelInfo, L.CheckString(1))
		L.Push(lua.LTrue)
		return 1
	})

	pl := e.L.CreateTable(0, 5)
	pl.RawSetString("name", lua.LString(p.name))
	pl.RawSetString("dir", lua.LString(p.dir))
	pl.RawSetString("version", lua.LString(p.version))
	pl.RawSetString("source", lua.LString(p.kindStr))
	e.set(pl, "on_unload", func(L *lua.LState) int {
		fn := L.CheckFunction(1)
		if len(p.unloadFns) >= maxUnloadFns {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "too many on_unload handlers (%d)", maxUnloadFns))
		}
		p.unloadFns = append(p.unloadFns, fn)
		L.Push(lua.LTrue)
		return 1
	})
	g.RawSetString("plugin", pl)

	e.set(g, "pos", func(L *lua.LState) int {
		L.Push(p.posTable(pluginapi.Pos{Line: clampInt(float64(L.CheckNumber(1))), Col: clampInt(float64(L.OptNumber(2, 0)))}))
		return 1
	})
	e.set(g, "range", func(L *lua.LState) int {
		L.Push(p.rangeTable(pluginapi.Range{
			From: pluginapi.Pos{Line: clampInt(float64(L.CheckNumber(1))), Col: clampInt(float64(L.CheckNumber(2)))},
			To:   pluginapi.Pos{Line: clampInt(float64(L.CheckNumber(3))), Col: clampInt(float64(L.CheckNumber(4)))},
		}))
		return 1
	})
	e.set(g, "pos_cmp", func(L *lua.LState) int {
		a := checkPos(L, 1)
		b := checkPos(L, 2)
		L.Push(lua.LNumber(cmpPos(a, b)))
		return 1
	})
	e.set(g, "range_eq", func(L *lua.LState) int {
		a := checkRange(L, 1)
		b := checkRange(L, 2)
		L.Push(lua.LBool(a == b))
		return 1
	})
}

func checkPos(L *lua.LState, n int) pluginapi.Pos {
	v, err := posFromLua(L.Get(n))
	if err != nil {
		L.ArgError(n, err.Error())
	}
	return v
}

func checkRange(L *lua.LState, n int) pluginapi.Range {
	v, err := rangeFromLua(L.Get(n))
	if err != nil {
		L.ArgError(n, err.Error())
	}
	return v
}

func cmpPos(a, b pluginapi.Pos) int {
	switch {
	case a.Line != b.Line:
		if a.Line < b.Line {
			return -1
		}
		return 1
	case a.Col < b.Col:
		return -1
	case a.Col > b.Col:
		return 1
	}
	return 0
}

// logLine implements gvim.log.<level>(fmt, ...): string.format semantics when the first
// argument is a string, otherwise the arguments joined by spaces through tostring. It runs
// inside the plugin's slice, so a __tostring metamethod is budgeted. warn and error also
// reach the user's warnings log.
func (p *plugin) logLine(L *lua.LState, level string) {
	msg := p.formatArgs(L)
	p.r.logf(p.name, level, msg)
	if level == levelWarn || level == levelError {
		lvl := pluginapi.LevelWarn
		if level == levelError {
			lvl = pluginapi.LevelError
		}
		p.r.notify(p, lvl, p.name+": "+firstLine(msg))
	}
}

func (p *plugin) formatArgs(L *lua.LState) string {
	n := L.GetTop()
	if n == 0 {
		return ""
	}
	if s, ok := L.Get(1).(lua.LString); ok {
		if n == 1 {
			return string(s)
		}
		if f, ok := p.format.(*lua.LFunction); ok {
			err := L.CallByParam(lua.P{Fn: f, NRet: 1, Protect: true}, argsFrom(L, 1)...)
			if err == nil {
				out := L.Get(-1).String()
				L.Pop(1)
				return out
			}
		}
	}
	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	return strings.Join(parts, " ")
}

// notifyUser implements gvim.notify: newlines become spaces and the message is clipped.
func (p *plugin) notifyUser(level pluginapi.Level, msg string) {
	msg = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(msg)
	msg = clipMsg(msg, maxNotify)
	p.r.logf(p.name, string(level), msg)
	p.r.host.Notify(p.name, level, msg)
}
