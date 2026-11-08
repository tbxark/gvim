package plugin

import (
	"context"
	"strconv"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// This file is the test seam of the package: a `T` table in every plugin state with
// helpers that stand in for the API areas later stages own (commands, events, timers),
// extra await kinds for the task tests, and read access to the registry. It is compiled
// into the test binary only.

// DefaultHeapLimit is the heap guard of the editor, for the tests that measure it.
const DefaultHeapLimit = defaultHeapLimit

var spies = map[*Runtime]*[]string{}

// lastTok is the token of the latest t_host wait of each runtime.
var lastTok = map[*Runtime]pluginapi.Token{}

// OnStop registers a function Stop calls before the states are closed.
func OnStop(r *Runtime, f func()) { r.onStop(f) }

// Spy returns the lines plugins wrote with T.spy, in order.
func Spy(r *Runtime) []string {
	if s := spies[r]; s != nil {
		return append([]string(nil), *s...)
	}
	return nil
}

// ResetSpy clears the spy lines.
func ResetSpy(r *Runtime) { delete(spies, r) }

// Registry is a snapshot of what the runtime holds, to assert that unload leaves none of it.
type Registry struct {
	Plugins, Waits, UserQ, CmdQ, CtlQ, Inbox, Events int
	Tasks, Parked, Inflight, Subs, Commands, Handles int
}

// RegistryOf counts the runtime's registries; the per-plugin fields are summed over all plugins.
func RegistryOf(r *Runtime) Registry {
	g := Registry{Plugins: len(r.plugins), Waits: len(r.waits), UserQ: len(r.userq), CmdQ: len(r.cmdq),
		CtlQ: len(r.ctlq), Inbox: len(r.inbox), Events: len(r.evq)}
	for _, p := range r.plugins {
		g.Tasks += len(p.tasks)
		g.Parked += p.parked
		g.Inflight += p.inflight
		g.Subs += p.nsubs
		g.Commands += len(p.commands)
		g.Handles += len(p.handles)
		g.Events += len(p.evq)
	}
	return g
}

// HandleCacheSize is the number of cached Buffer/Window handles of a plugin (-1: unknown plugin).
func HandleCacheSize(r *Runtime, name string) int {
	if p := r.plugins[name]; p != nil {
		return len(p.handles)
	}
	return -1
}

// SetCloseHook calls f(depth, plugin) just before a plugin state is closed.
func SetCloseHook(r *Runtime, f func(depth int, name string)) {
	r.closeHook = func(p *plugin) { f(r.depth, p.name) }
}

// Depth is Runtime.depth.
func Depth(r *Runtime) int { return r.depth }

// Strikes is the number of strikes in the window of a plugin.
func Strikes(r *Runtime, name string) int {
	if p := r.plugins[name]; p != nil {
		return len(p.strikes)
	}
	return 0
}

// Coroutines is the nesting counter of a plugin (must be 0 between slices).
func Coroutines(r *Runtime, name string) int {
	if p := r.plugins[name]; p != nil {
		return p.coDepth
	}
	return -1
}

// ParkedTokens lists the tokens plugins are waiting for.
func ParkedTokens(r *Runtime) []pluginapi.Token {
	var out []pluginapi.Token
	for tok := range r.waits {
		out = append(out, tok)
	}
	return out
}

// ClassifyCause runs the failure classification of a slice that was cancelled with the
// named cause ("deadline", "hard", "memory", "cancelled" or "plain" for none) and returns
// the kind and the message the user would see.
func ClassifyCause(cause string, budget time.Duration) (kind, msg string) {
	r := New(Options{})
	sl := r.newSlice(context.Background(), budget)
	defer sl.finish()
	text := "p/init.lua:3: context canceled"
	switch cause {
	case "deadline":
		sl.cancel(errSliceDeadline)
	case "hard":
		sl.cancel(errHardDeadline)
	case "memory":
		sl.cancel(errMemory)
	case "cancelled":
		sl.cancel(errCancelled)
	default:
		text = "p/init.lua:3: boom"
	}
	err := &lua.ApiError{Type: lua.ApiErrorRun, Object: lua.LString(text), StackTrace: "trace"}
	re := r.classify(&task{p: &plugin{}}, sl, resumeResult{err: err})
	names := map[failKind]string{failLua: "lua", failTimeout: "timeout", failMemory: "memory", failCancelled: "cancelled", failPanic: "panic"}
	return names[re.kind], re.msg
}

// AddSubscription subscribes fn (a Lua function value of that plugin) to event.
func AddSubscription(r *Runtime, plugin, event string, fn *lua.LFunction, once bool) error {
	_, err := r.plugins[plugin].addSub(event, fn, once)
	return err
}

func init() {
	registerAwait("t_echo", func(c *awaitCall) { c.Return(c.args...) })
	registerAwaitV("t_echo_all", func(c *awaitCall) { c.Return(c.args...) })
	registerAwait("t_fail", func(c *awaitCall) { c.Fail(pluginapi.Errf(pluginapi.CodeBusy, "busy now")) })
	registerAwait("t_bad", func(c *awaitCall) { c.Bad(1, "bad", "number expected") })
	registerAwait("t_panic", func(c *awaitCall) { panic("boom in handler") })
	// t_work(value, ms): Work that completes with value after ms milliseconds of real time.
	registerAwait("t_work", func(c *awaitCall) {
		if err := c.CheckPark(true); err != nil {
			c.Fail(err)
			return
		}
		v, _ := c.args[0].(lua.LString)
		ms, _ := argNumber(c.args, 1)
		c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
			select {
			case <-time.After(time.Duration(ms) * time.Millisecond):
				return pluginapi.Completion{Values: []any{string(v)}}
			case <-ctx.Done():
				return pluginapi.Completion{Err: "cancelled: work cancelled"}
			}
		}}})
	})
	registerAwait("t_work_panic", func(c *awaitCall) {
		c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion { panic("work boom") }}})
	})
	registerAwait("t_xcall", func(c *awaitCall) {
		target, _ := c.args[0].(lua.LString)
		c.Park(&parkSpec{Target: string(target)})
	})
	registerAwait("t_host", func(c *awaitCall) {
		tok := c.NewToken()
		lastTok[c.r] = tok
		spy(c.r, "token:"+itoa(int(tok)))
		c.Park(&parkSpec{Token: tok, Cancel: func() { spy(c.r, "cancelled:"+itoa(int(tok))) }})
	})

	extraInstallers = append(extraInstallers, installTestAPI)
}

func spy(r *Runtime, line string) {
	s := spies[r]
	if s == nil {
		s = new([]string)
		spies[r] = s
	}
	*s = append(*s, line)
}

func itoa(n int) string { return strconv.Itoa(n) }

// installTestAPI installs the T table.
func installTestAPI(e *apiEnv) {
	p, r, L := e.p, e.r, e.L
	t := L.NewTable()
	L.SetGlobal("T", t)
	e.set(t, "spy", func(L *lua.LState) int {
		parts := make([]string, 0, L.GetTop())
		for i := 1; i <= L.GetTop(); i++ {
			parts = append(parts, L.ToStringMeta(L.Get(i)).String())
		}
		spy(r, strings.Join(parts, " "))
		return 0
	})
	e.set(t, "command", func(L *lua.LState) int {
		name := L.CheckString(1)
		fn := L.CheckFunction(2)
		err := p.addCommand(&command{id: p.name + "." + name, name: name, fn: fn,
			info: pluginapi.CommandInfo{ID: p.name + "." + name, Title: name, Plugin: p.name, Record: true}})
		if err != nil {
			L.RaiseError("%s", err.Error())
		}
		return 0
	})
	e.set(t, "on", func(L *lua.LState) int {
		if _, err := p.addSub(L.CheckString(1), L.CheckFunction(2), L.ToBool(3)); err != nil {
			L.RaiseError("%s", err.Error())
		}
		return 0
	})
	e.set(t, "on_unload", func(L *lua.LState) int {
		p.unloadFns = append(p.unloadFns, L.CheckFunction(1))
		return 0
	})
	e.set(t, "provides", func(L *lua.LState) int {
		e.provides(L.CheckString(1))
		return 0
	})
	e.set(t, "unregister", func(L *lua.LState) int {
		p.removeCommand(L.CheckString(1))
		return 0
	})
	// callback(class, fn, ...): starts fn as a task of the given origin ("timer", "callback",
	// "event", "command"), queued for the next Drain.
	e.set(t, "callback", func(L *lua.LState) int {
		o := map[string]origin{"timer": originTimer, "callback": originCallback, "event": originEvent, "command": originCommand}[L.CheckString(1)]
		r.startCallback(p, o, "test callback", L.CheckFunction(2), argsFrom(L, 3)...)
		return 0
	})
	e.set(t, "gopanic", func(L *lua.LState) int {
		panic("go boom")
	})
	e.set(t, "run", func(L *lua.LState) int {
		var args map[string]any
		if v := L.Get(2); v != lua.LNil {
			a, err := p.fromLua(v)
			if err != nil {
				L.RaiseError("%s", err.Error())
			}
			args, _ = a.(map[string]any)
		}
		if err := r.Run(L.CheckString(1), args, pluginapi.SrcAPI); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LTrue)
		return 1
	})
	e.set(t, "control", func(L *lua.LState) int {
		w, n := r.Control(pluginapi.Control{Op: L.CheckString(1), Name: L.OptString(2, "")})
		L.Push(lua.LNumber(len(w)))
		L.Push(lua.LNumber(len(n)))
		return 2
	})
	e.set(t, "last_token", func(L *lua.LState) int {
		L.Push(lua.LNumber(lastTok[r]))
		return 1
	})
	e.set(t, "resolve", func(L *lua.LState) int {
		r.Resolve(pluginapi.Completion{Token: pluginapi.Token(L.CheckInt(1)), Values: []any{L.Get(2)}})
		return 0
	})
	e.set(t, "veto", func(L *lua.LState) int {
		cancel, reason := r.Veto(pluginapi.Event{Name: L.CheckString(1)})
		L.Push(lua.LBool(cancel))
		L.Push(lua.LString(reason))
		return 2
	})
	e.set(t, "emit", func(L *lua.LState) int {
		var data map[string]any
		if v := L.Get(2); v != lua.LNil {
			a, err := p.fromLua(v)
			if err != nil {
				L.RaiseError("%s", err.Error())
			}
			data, _ = a.(map[string]any)
		}
		r.emit(pluginapi.Event{Name: L.CheckString(1), Data: data}, p)
		return 0
	})
	e.set(t, "busy", func(L *lua.LState) int {
		end := time.Now().Add(time.Duration(L.CheckInt(1)) * time.Millisecond)
		for time.Now().Before(end) {
		}
		return 0
	})
	e.set(t, "register_all", func(L *lua.LState) int {
		// registers one resource of every kind the host tracks, as the API areas will
		if err := p.addCommand(&command{id: p.name + ".reg", name: "reg", fn: L.NewFunction(func(*lua.LState) int { return 0 }),
			info: pluginapi.CommandInfo{ID: p.name + ".reg", Plugin: p.name}}); err != nil {
			L.RaiseError("%s", err.Error())
		}
		r.host.SetBindings(p.name, []pluginapi.Binding{{Chord: "ctrl+k", Action: p.name + ".reg", Plugin: p.name}})
		r.host.SetModes(p.name, []pluginapi.ModeInfo{{Name: "normal", Plugin: p.name}})
		r.host.SetContext(p.name, "active", true)
		r.host.SetStatusItems(p.name, []pluginapi.StatusItem{{ID: "s", Plugin: p.name, Text: "x"}})
		_ = r.host.SetInputMode(p.name, "normal")
		p.addCleanup(func() { spy(r, "cleanup:"+p.name) })
		return 0
	})
	e.set(t, "depth", func(L *lua.LState) int {
		L.Push(lua.LNumber(r.depth))
		return 1
	})
	e.set(t, "in_slice", func(L *lua.LState) int {
		L.Push(lua.LBool(r.InSlice()))
		return 1
	})
	e.set(t, "yield_raw", func(L *lua.LState) int { return L.Yield(argsFrom(L, 1)...) })
	e.set(t, "buf", func(L *lua.LState) int {
		L.Push(p.toLua(pluginapi.BufferID(L.CheckInt(1))))
		return 1
	})
	e.set(t, "win", func(L *lua.LState) int {
		L.Push(p.toLua(pluginapi.WinID(L.CheckInt(1))))
		return 1
	})
	e.set(t, "cache_size", func(L *lua.LState) int {
		L.Push(lua.LNumber(len(p.handles)))
		return 1
	})
	e.set(t, "lines", func(L *lua.LState) int {
		lines, err := r.guard.Lines(pluginapi.BufferID(L.CheckInt(1)), 0, -1)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(errString(err)))
			return 2
		}
		L.Push(lua.LNumber(len(lines)))
		return 1
	})
	e.set(t, "nested", func(L *lua.LState) int {
		vals, err := p.callNested(L, L.CheckFunction(1))
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LTrue)
		for _, v := range vals {
			L.Push(v)
		}
		return 1 + len(vals)
	})
	e.set(t, "to_host", func(L *lua.LState) int {
		v, err := p.fromLua(L.Get(1))
		if err != nil {
			L.Push(lua.LString("ERR " + errString(err)))
			return 1
		}
		L.Push(lua.LString(describe(v)))
		return 1
	})
	e.set(t, "from_host", func(L *lua.LState) int {
		// builds a value from a compact spec: "buf:3", "win:2", "pos:1,2", "range:1,2,3,4", "version:1.2"
		spec := L.CheckString(1)
		switch kind, rest, _ := strings.Cut(spec, ":"); kind {
		case "buf":
			L.Push(p.toLua(pluginapi.BufferID(atoi(rest))))
		case "win":
			L.Push(p.toLua(pluginapi.WinID(atoi(rest))))
		case "pos":
			a := strings.Split(rest, ",")
			L.Push(p.toLua(pluginapi.Pos{Line: atoi(a[0]), Col: atoi(a[1])}))
		case "range":
			a := strings.Split(rest, ",")
			L.Push(p.toLua(pluginapi.Range{From: pluginapi.Pos{Line: atoi(a[0]), Col: atoi(a[1])}, To: pluginapi.Pos{Line: atoi(a[2]), Col: atoi(a[3])}}))
		case "version":
			v, _ := pluginapi.ParseVersion(rest)
			L.Push(p.toLua(v))
		case "key":
			L.Push(p.toLua(pluginapi.Key{Key: rest, Text: rest, Ctrl: true}))
		case "hunk":
			L.Push(p.toLua(pluginapi.Hunk{Kind: "change", OldStart: 1, OldCount: 2, NewStart: 3, NewCount: 4}))
		case "changerec":
			L.Push(p.toLua(pluginapi.ChangeRec{Range: pluginapi.Range{From: pluginapi.Pos{Line: 1, Col: 2}, To: pluginapi.Pos{Line: 1, Col: 5}}, Text: "new", Removed: "old"}))
		case "caret":
			anchor := pluginapi.Pos{Line: 3, Col: 4}
			L.Push(p.toLua([]pluginapi.Caret{{Pos: pluginapi.Pos{Line: 1, Col: 2}, Anchor: &anchor}, {Pos: pluginapi.Pos{Line: 5}}}))
		case "info":
			L.Push(p.toLua(pluginapi.BufferInfo{ID: 9, Name: "n", LineCount: 3, Version: pluginapi.Version{Gen: 1, Ver: 2}}))
		case "map":
			L.Push(p.toLua(map[string]any{"a": 1, "b": []any{"x", true, 2.5}, "c": map[string]any{}}))
		}
		return 1
	})
	for _, kind := range []string{"t_echo", "t_fail", "t_bad", "t_panic", "t_work", "t_work_panic", "t_xcall", "t_host"} {
		t.RawSetString(strings.TrimPrefix(kind, "t_"), e.async(kind))
	}
	t.RawSetString("echo_all", e.asyncV("t_echo_all"))
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// describe renders a host value compactly and deterministically.
func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = describe(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
				keys[j], keys[j-1] = keys[j-1], keys[j]
			}
		}
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + "=" + describe(x[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	case string:
		return `"` + x + `"`
	case int64:
		return "i" + lua.LNumber(x).String()
	case float64:
		return "f" + lua.LNumber(x).String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	case pluginapi.BufferID:
		return "buf" + lua.LNumber(x).String()
	case pluginapi.WinID:
		return "win" + lua.LNumber(x).String()
	}
	return "?"
}

// NoServices is the Services of a runtime created without one.
func NoServices() pluginapi.Services { return noServices{} }

// TimeoutOf is the effective plugin_timeout_ms of a runtime.
func TimeoutOf(r *Runtime) time.Duration { return r.timeout }

// Budgets are the slice budgets by origin.
func Budgets(r *Runtime) map[string]time.Duration {
	return map[string]time.Duration{
		"load": r.budget(originLoad), "command": r.budget(originCommand), "callback": r.budget(originCallback),
		"event": r.budget(originEvent), "timer": r.budget(originTimer),
	}
}

// ExpectRuntime registers a runtime-level wait (the loader's discovery Work uses one).
func ExpectRuntime(r *Runtime, tok pluginapi.Token, f func(pluginapi.Completion)) {
	r.expectRuntime(tok, f)
}

// SnakeCase is the key conversion of host structs.
func SnakeCase(s string) string { return snakeCase(s) }
