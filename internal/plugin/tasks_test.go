package plugin_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// cmd starts a plugin "t" whose command t.go runs body (a Lua function body taking ctx),
// runs the command and settles. Extra top-level Lua goes in prelude.
func cmd(t *testing.T, prelude, body string, o ...plugintest.Options) *plugintest.Harness {
	t.Helper()
	h := start(t, `
local gvim = require "gvim"
`+prelude+`
T.command("go", function(ctx)
`+body+`
end)`, o...)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return h
}

func TestAwait(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"plain", `T.spy(T.echo("a"))`, "a"},
		{"two values", `local a, b = T.echo("x", "y") T.spy(a, b)`, "x y"},
		{"extra values dropped", `local a, b, c = T.echo("x", "y", "z") T.spy(a, b, tostring(c))`, "x y nil"},
		{"variadic wrapper", `T.spy(T.echo_all(1, 2, 3, 4))`, "1 2 3 4"},
		{"inside pcall", `local ok, v = pcall(function() return T.echo("p") end) T.spy(ok, v)`, "true p"},
		{"pcall error after await", `local ok, e = pcall(function() T.echo("x") error("boom", 0) end) T.spy(ok, e)`, "false boom"},
		{"pcall returns all values", `T.spy(pcall(function() return 1, 2, 3 end))`, "true 1 2 3"},
		{"xpcall handler", `T.spy(xpcall(function() T.echo(1) error("e", 0) end, function(m) return "H:" .. m end))`, "false H:e"},
		{"in tail position", `local function f() return T.echo("tail") end T.spy(f())`, "tail"},
		{"chain of tail calls", `local function a() return T.echo("c") end local function b() return a() end T.spy(b())`, "c"},
		{"in for-in pairs", `for k, v in pairs({1, 2}) do T.spy(k, T.echo(v)) end`, "1 1|2 2"},
		{"in a while loop over a generator", `local gen = coroutine.wrap(function() for i = 1, 2 do coroutine.yield(T.echo(i)) end end) while true do local v = gen() if v == nil then break end T.spy(v) end`, "1|2"},
		{"generator without awaits in for-in", `for v in coroutine.wrap(function() for i = 1, 2 do coroutine.yield(i) end end) do T.spy(v) end`, "1|2"},
		{"expected failure", `local v, e = T.fail() T.spy(tostring(v), e, gvim.err_code(e))`, "nil busy: busy now busy"},
		{"programmer error carries the position", `local ok, e = pcall(function() T.bad() end) T.spy(ok, e)`, "false t/init.lua:5: bad argument"},
		{"programmer error text", `local ok, e = pcall(function() T.bad() end) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #1 to 'bad' (number expected)"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			if got := spyText(h); !strings.HasPrefix(got, r.want) {
				t.Fatalf("spy = %q, want prefix %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestAwaitInsideWrap(t *testing.T) {
	h := cmd(t, "", `
local f = coroutine.wrap(function() local x = T.echo("w") coroutine.yield(x) coroutine.yield("g2") end)
T.spy(f(), f())`)
	if got := spyText(h); got != "w g2" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestAwaitInsideNestedWrap(t *testing.T) {
	h := cmd(t, "", `
local f = coroutine.wrap(function()
  local g = coroutine.wrap(function() coroutine.yield(T.echo("in")) end)
  coroutine.yield(g() .. "!")
end)
T.spy(f())`)
	if got := spyText(h); got != "in!" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestAwaitInsidePcallInsideWrap(t *testing.T) {
	h := cmd(t, "", `
local f = coroutine.wrap(function()
  local ok, v = pcall(function() return T.echo("p") end)
  coroutine.yield(tostring(ok) .. v)
end)
T.spy(f())`)
	if got := spyText(h); got != "truep" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestGeneratorYieldsReachCaller(t *testing.T) {
	// Yields that are not awaits belong to the user: they come back unchanged, also through pcall.
	h := cmd(t, "", `
local co = coroutine.create(function(a)
  local ok, b = pcall(function() return coroutine.yield(a + 1) end)
  local c = coroutine.yield(b * 2)
  return "end", c
end)
T.spy(coroutine.resume(co, 1))
T.spy(coroutine.resume(co, 10))
T.spy(coroutine.resume(co, "last"))
T.spy(coroutine.status(co))`)
	want := "true 2|true 20|true end last|dead"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q want %q log=%v", got, want, h.LogText(""))
	}
}

func TestTailYieldInGenerator(t *testing.T) {
	// `return coroutine.yield(x)` must not crash the state (tail call of a Go function).
	h := cmd(t, "", `
local f = coroutine.wrap(function(a) return coroutine.yield(a) end)
T.spy(f(1))
T.spy(f("back"))`)
	if got := spyText(h); got != "1|back" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestWrapErrorPropagatesWithObject(t *testing.T) {
	h := cmd(t, "", `
local f = coroutine.wrap(function() error({code = 42}) end)
local ok, e = pcall(f)
T.spy(ok, type(e), e.code)
local g = coroutine.wrap(function() error("msg", 0) end)
local ok2, e2 = pcall(g)
T.spy(ok2, e2)`)
	if got := spyText(h); got != "false table 42|false msg" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestResumeDeadCoroutine(t *testing.T) {
	h := cmd(t, "", `
local co = coroutine.create(function() return 1 end)
T.spy(coroutine.resume(co))
local ok, e = coroutine.resume(co)
T.spy(ok, e ~= nil)
local w = coroutine.wrap(function() end)
w()
T.spy(pcall(w))`)
	got := spy(h)
	if len(got) != 3 || got[0] != "true 1" || got[1] != "false true" || !strings.HasPrefix(got[2], "false ") {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestAsyncSpawnBodyBuiltOnWrap(t *testing.T) {
	// A spawned body may use wrap generators that await, as the ports do.
	h := cmd(t, "", `
local task = gvim.async.spawn(function()
  local gen = coroutine.wrap(function() for i = 1, 3 do coroutine.yield(T.echo(i)) end end)
  local sum = 0
  while true do
    local v = gen()
    if v == nil then break end
    sum = sum + v
  end
  return sum
end)
T.spy(task:wait())`)
	if got := spyText(h); got != "6" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

// heapGrowth is the bytes allocated since before (a TotalAlloc reading).
func heapGrowth(before uint64) uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc - before
}

func totalAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

func TestPcallRecursionBounded(t *testing.T) {
	before := totalAlloc()
	start := time.Now()
	h := cmd(t, "", `
local depth = 0
local function f() depth = depth + 1 pcall(f) end
f()
T.spy("returned", depth > 50, depth < 400)`)
	if got := spyText(h); got != "returned true true" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	// C9: returns in under 50 ms (a CI-safe multiple here) and grows the heap by under 32 MiB.
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Fatalf("took %v, want about 7 ms (spec bound 50 ms)", d)
	}
	if grown := heapGrowth(before); grown > 32<<20 {
		t.Fatalf("heap grew by %d MiB, want under 32", grown>>20)
	}
	if n := plugin.Coroutines(h.RT, "t"); n != 0 {
		t.Fatalf("coroutine depth = %d after the slice", n)
	}
	// The state is usable afterwards.
	plugin.ResetSpy(h.RT)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "returned true true" {
		t.Fatalf("second run: spy = %q", got)
	}
}

func TestWrapRecursionBounded(t *testing.T) {
	before := totalAlloc()
	start := time.Now()
	h := cmd(t, "", `
local function f() coroutine.wrap(f)() end
local ok, e = pcall(f)
T.spy("returned", ok)`)
	if got := spyText(h); got != "returned false" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Fatalf("took %v, want about 7 ms (spec bound 50 ms)", d)
	}
	if grown := heapGrowth(before); grown > 32<<20 {
		t.Fatalf("heap grew by %d MiB, want under 32", grown>>20)
	}
	if n := plugin.Coroutines(h.RT, "t"); n != 0 {
		t.Fatalf("coroutine depth = %d after the slice", n)
	}
	// The state is usable afterwards.
	plugin.ResetSpy(h.RT)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "returned false" {
		t.Fatalf("second run: spy = %q", got)
	}
}

func TestPcallNilReturnsFalse(t *testing.T) {
	h := cmd(t, "", `local ok, e = pcall(nil) T.spy(ok, e ~= nil)`)
	if got := spyText(h); got != "false true" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestPcallCallableTable(t *testing.T) {
	h := cmd(t, "", `
local t = setmetatable({}, {__call = function(_, a) return a end})
T.spy(pcall(t, 7))`)
	if got := spyText(h); got != "true 7" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestAsync(t *testing.T) {
	rows := []struct {
		name, prelude, body, want string
	}{
		{"spawn and wait", ``, `local k = gvim.async.spawn(function(a, b) return T.echo(a .. b) end, "x", "y") T.spy(k:wait())`, "xy"},
		{"spawn runs after the spawner yields control", ``, `local k = gvim.async.spawn(function() T.spy("child") end) T.spy("parent") k:wait()`, "parent|child"},
		{"nested spawn", ``, `
local outer = gvim.async.spawn(function()
  local inner = gvim.async.spawn(function() return "deep" end)
  return inner:wait() .. "!"
end)
T.spy(outer:wait())`, "deep!"},
		{"task done", ``, `local k = gvim.async.spawn(function() end) T.spy(k:done()) k:wait() T.spy(k:done())`, "false|true"},
		{"wait re-raises", ``, `local k = gvim.async.spawn(function() error("kaput", 0) end) T.spy(pcall(k.wait, k))`, "false kaput"},
		{"wait returns all results", ``, `local k = gvim.async.spawn(function() return 1, 2, 3 end) T.spy(k:wait())`, "1 2 3"},
		{"all in order", ``, `
local r = gvim.async.all({
  function() return T.echo("a") end,
  function() return T.echo("b"), "c" end,
})
T.spy(r[1][1], r[2][1], r[2][2], r[2].n)`, "a b c 2"},
		{"all with erroring child settles every child first", ``, `
local ok, e = pcall(gvim.async.all, {
  function() error("first", 0) end,
  function() T.echo(1) T.spy("sibling ran") end,
})
T.spy(ok, e)`, "sibling ran|false first"},
		{"all rejects more than 16", ``, `
local fns = {} for i = 1, 17 do fns[i] = function() end end
T.spy((pcall(gvim.async.all, fns)))`, "false"},
		{"suspend resumed synchronously", ``, `T.spy(gvim.async.suspend(function(resume) resume("a", "b", "c") end))`, "a b c"},
		{"suspend error in fn propagates", ``, `T.spy(pcall(gvim.async.suspend, function() error("in fn", 0) end))`, "false in fn"},
		{"cancel a task", ``, `
local k = gvim.async.spawn(function() T.spy("never") end)
k:cancel()
T.spy(pcall(k.wait, k))`, "false task cancelled"},
		{"wait on self is an error", ``, `
local me
me = gvim.async.spawn(function()
  T.echo(1)
  local _, e = pcall(me.wait, me)
  T.spy(e:match("cannot wait for itself") ~= nil)
end)
me:wait()`, "true"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, r.prelude, r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestSuspendResumedLater(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local saved
T.command("wait", function()
  local v, w = gvim.async.suspend(function(resume) saved = resume end)
  T.spy("got", v, w)
end)
T.command("wake", function()
  saved("x", "y")
  saved("ignored")
  T.spy("woke")
end)`)
	if err := h.Run("t.wait", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "" {
		t.Fatalf("parked task must not have run on: %q", got)
	}
	if err := h.Run("t.wake", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "woke|got x y" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if r := plugin.RegistryOf(h.RT); r.Parked != 0 || r.Waits != 0 || r.Tasks != 0 {
		t.Fatalf("leftovers: %+v", r)
	}
}

func TestWorkRoundTrip(t *testing.T) {
	h := cmd(t, "", `T.spy(T.work("payload", 5))`)
	if got := spyText(h); got != "payload" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestCoroutineCreatedInOneSliceResumedInAnother(t *testing.T) {
	// A coroutine created under the context of slice 1 must run under slice 2's context.
	h := start(t, `
local co
T.command("make", function()
  co = coroutine.create(function() while true do coroutine.yield("tick") end end)
  T.spy(coroutine.resume(co))
end)
T.command("again", function()
  T.spy(coroutine.resume(co))
  T.spy(coroutine.status(co))
end)`)
	if err := h.Run("t.make", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // slice 1's context is long gone
	if err := h.Run("t.again", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "true tick|true tick|suspended" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestParkedTaskResumesWithFreshBudget(t *testing.T) {
	// Parked for longer than the whole budget: only Lua running time is charged.
	h := cmd(t, "", `
T.work("p", 150)
T.spy("done")`, plugintest.Options{Timeout: 100 * time.Millisecond})
	if got := spyText(h); got != "done" {
		t.Fatalf("spy = %q log=%v notices=%v", got, h.LogText(""), h.NoticeTexts())
	}
	if n := plugin.Strikes(h.RT, "t"); n != 0 {
		t.Fatalf("strikes = %d", n)
	}
}

func TestInvalidYieldStrike(t *testing.T) {
	forged := map[string]string{
		"untagged string":    `T.yield_raw("fs_write", "/x", "y")`,
		"untagged table":     `T.yield_raw({}, "fs_write")`,
		"no values":          `T.yield_raw()`,
		"string equals kind": `T.yield_raw("t_echo", "t_echo")`,
	}
	for name, body := range forged {
		t.Run(name, func(t *testing.T) {
			h := cmd(t, "", body)
			if !logHas(h, "t", "invalid yield") {
				t.Fatalf("log = %v", h.LogText(""))
			}
			if n := plugin.Strikes(h.RT, "t"); n != 1 {
				t.Fatalf("strikes = %d", n)
			}
			if !noticeHas(h, "strike 1/3") {
				t.Fatalf("notices = %v", h.NoticeTexts())
			}
			if plugin.RegistryOf(h.RT).Tasks != 0 {
				t.Fatal("aborted task is still registered")
			}
		})
	}
	t.Run("three strikes suspend", func(t *testing.T) {
		h := cmd(t, "", `T.yield_raw("x")`)
		for i := 0; i < 2; i++ {
			if err := h.Run("t.go", nil); err != nil {
				t.Fatal(err)
			}
		}
		if status(h, "t") != "suspended" {
			t.Fatalf("status = %q notices=%v", status(h, "t"), h.NoticeTexts())
		}
	})
}

func TestHandlerPanicIsAStrike(t *testing.T) {
	h := cmd(t, "", `T.panic()`)
	if !logHas(h, "t", "panic in async call") || plugin.Strikes(h.RT, "t") != 1 {
		t.Fatalf("log=%v strikes=%d", h.LogText(""), plugin.Strikes(h.RT, "t"))
	}
	// The plugin and the runtime keep working.
	plugin.ResetSpy(h.RT)
	h2 := cmd(t, "", `T.spy("alive")`)
	if spyText(h2) != "alive" {
		t.Fatal("runtime unusable after a handler panic")
	}
}

func TestResolveUnknownTokenIgnored(t *testing.T) {
	h := start(t, `local gvim = require "gvim"`)
	h.Resolve(pluginapi.Completion{Token: 12345, Values: []any{"x"}})
	if r := plugin.RegistryOf(h.RT); r.Waits != 0 {
		t.Fatalf("registry = %+v", r)
	}
}

func TestParkedQuotaReturnsLimitError(t *testing.T) {
	// 64 parked tasks is the limit; the 65th await returns nil, "limit: ..." and the plugin lives.
	h := start(t, `
local gvim = require "gvim"
T.command("park", function() T.host() T.spy("resumed") end)
T.command("probe", function()
  local v, e = T.host()
  T.spy(tostring(v), e)
end)`)
	for i := 0; i < plugin.QuotaParkedTasks; i++ {
		if err := h.Run("t.park", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Run("t.probe", nil); err != nil {
		t.Fatal(err)
	}
	got := spy(h)
	if len(got) == 0 || !strings.Contains(got[len(got)-1], "limit: parked task quota (64)") {
		t.Fatalf("last spy = %q", got)
	}
	if status(h, "t") != "loaded" {
		t.Fatalf("plugin status = %q", status(h, "t"))
	}
	if !logHas(h, "t", "limit: parked task quota (64)") {
		t.Fatalf("no warning logged: %v", h.LogText("t"))
	}
	// The abandoned host waits were cancelled (CancelWait semantics) when the quota refused.
	if n := plugin.RegistryOf(h.RT).Parked; n != plugin.QuotaParkedTasks {
		t.Fatalf("parked = %d", n)
	}
}

// Awaiting inside a function that gopher-lua calls from Go (sort comparator, gsub callback,
// metamethod, for-in iterator function) cannot work. It must fail the task with a clear
// message, leave the state usable and leak nothing.
func TestAwaitInGoCallbackFailsCleanly(t *testing.T) {
	rows := map[string]string{
		"sort comparator":         `table.sort({3, 2, 1}, function(a, b) T.echo(1) return a < b end)`,
		"for-in iterator":         `for v in coroutine.wrap(function() coroutine.yield(T.echo(1)) end) do end`,
		"__index function":        `local t = setmetatable({}, {__index = function(_, k) return T.echo(k) end}) T.spy(t.x)`,
		"__tostring via tostring": `tostring(setmetatable({}, {__tostring = function() T.echo(1) return "x" end}))`,
		"__tostring via print":    `print(setmetatable({}, {__tostring = function() T.echo(1) return "x" end}))`,
		"__tostring via log":      `gvim.log.info("%s", setmetatable({}, {__tostring = function() T.echo(1) return "x" end}))`,
		"__tostring via format":   `string.format("%s", setmetatable({}, {__tostring = function() T.echo(1) return "x" end}))`,
		"__lt via table.sort": `local mt = {__lt = function() T.echo(1) return true end}
			table.sort({setmetatable({}, mt), setmetatable({}, mt)})`,
		"gsub table replacement": `("abc"):gsub(".", setmetatable({}, {__index = function(_, k) return T.echo(k) end}))`,
		"gsub function":          `("abc"):gsub(".", function(c) return T.echo(c) end)`,
	}
	for name, body := range rows {
		t.Run(name, func(t *testing.T) {
			h := cmd(t, "", body)
			if !logHas(h, "t", "async call inside a Go callback") {
				t.Fatalf("log = %v", h.LogText(""))
			}
			if r := plugin.RegistryOf(h.RT); r.Tasks != 0 || r.Waits != 0 {
				t.Fatalf("leaked: %+v", r)
			}
			if plugin.Strikes(h.RT, "t") != 0 {
				t.Fatalf("a plugin mistake must be an ordinary error: strikes=%d log=%v", plugin.Strikes(h.RT, "t"), h.LogText(""))
			}
			// the state is still usable
			h2 := cmd(t, "", `T.spy(T.echo("alive"))`)
			if spyText(h2) != "alive" {
				t.Fatal("runtime unusable afterwards")
			}
			plugin.ResetSpy(h.RT)
			if err := h.Run("t.go", nil); err != nil {
				t.Fatal(err)
			}
			if status(h, "t") != "loaded" {
				t.Fatalf("status = %q", status(h, "t"))
			}
		})
	}
}

// The error of an await inside a Go callback is an ordinary Lua error: pcall gets the
// documented message, not a Go nil dereference.
func TestAwaitInGoCallbackIsCatchable(t *testing.T) {
	rows := map[string]string{
		"__tostring": `local a = setmetatable({}, {__tostring = function() T.echo(1) return "x" end}) T.spy(pcall(tostring, a))`,
		"__lt": `local mt = {__lt = function() T.echo(1) return true end}
			T.spy(pcall(table.sort, {setmetatable({}, mt), setmetatable({}, mt)}))`,
	}
	for name, body := range rows {
		t.Run(name, func(t *testing.T) {
			h := cmd(t, "", body)
			if got := spyText(h); !strings.HasPrefix(got, "false ") || !strings.Contains(got, "async call inside a Go callback") || strings.Contains(got, "nil pointer") {
				t.Fatalf("spy = %q", got)
			}
			if plugin.Strikes(h.RT, "t") != 0 {
				t.Fatal("a caught error is no strike")
			}
		})
	}
}

// A __tostring that recurses forever, reached through gvim.log, is an ordinary error like it
// is through print.
func TestRecursiveToStringInLogIsNoStrike(t *testing.T) {
	h := cmd(t, "", `local t t = setmetatable({}, {__tostring = function(s) return tostring(s) end}) gvim.log.info("%s", t)`)
	if plugin.Strikes(h.RT, "t") != 0 || logHas(h, "t", "panic:") {
		t.Fatalf("strikes = %d log=%v", plugin.Strikes(h.RT, "t"), h.LogText(""))
	}
	if !logHas(h, "t", "overflow") {
		t.Fatalf("log = %v", h.LogText(""))
	}
}

func TestGoPanicInAnAPIFunctionIsAStrike(t *testing.T) {
	h := cmd(t, "", `
local ok, e = pcall(T.gopanic)
T.spy("pcall caught it", ok)
T.gopanic()
T.spy("not reached")`)
	if got := spyText(h); got != "pcall caught it false" {
		t.Fatalf("spy = %q", got)
	}
	if n := plugin.Strikes(h.RT, "t"); n != 1 {
		t.Fatalf("strikes = %d, want 1 (a Go panic is a strike, an ordinary Lua error is not)", n)
	}
	if !logHas(h, "t", "panic:") || !noticeHas(h, "strike 1/3") {
		t.Fatalf("log = %v notices = %v", h.LogText(""), h.NoticeTexts())
	}
	// the plugin keeps working
	plugin.ResetSpy(h.RT)
	h2 := cmd(t, "", `T.spy("alive")`)
	if spyText(h2) != "alive" {
		t.Fatal("runtime unusable after a Go panic")
	}
}

func TestWorkPanicBecomesAnErrorCompletion(t *testing.T) {
	// A panic in a Work closure would crash a tea.Cmd goroutine, that is the editor.
	h := cmd(t, "", `local v, e = T.work_panic() T.spy(tostring(v), e)`)
	if got := spyText(h); !strings.HasPrefix(got, "nil io: internal error: work boom") {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestWorkQuota(t *testing.T) {
	h := start(t, `
T.command("go", function() T.spy("woke", T.work("v", 10)) end)
T.command("probe", function() local v, e = T.work("v", 10) T.spy(tostring(v), e) end)`)
	// Run without settling: the Work stays in flight.
	for i := 0; i < plugin.QuotaWork; i++ {
		if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.RT.Run("t.probe", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	if got := spy(h); len(got) != 1 || got[0] != "nil limit: in-flight work quota (32)" {
		t.Fatalf("spy = %v", got)
	}
	if r := plugin.RegistryOf(h.RT); r.Inflight != plugin.QuotaWork {
		t.Fatalf("in-flight = %d", r.Inflight)
	}
	// Running the Work frees the quota.
	h.Settle()
	if r := plugin.RegistryOf(h.RT); r.Inflight != 0 || r.Waits != 0 {
		t.Fatalf("registry after settling = %+v", r)
	}
	if got := spy(h); len(got) != 1+plugin.QuotaWork {
		t.Fatalf("not every task resumed: %d lines", len(got))
	}
}

func TestNestedCallbacks(t *testing.T) {
	rows := []struct {
		name, body, want string
		strike           bool
	}{
		{"returns its values", `T.spy(T.nested(function() return 1, "two" end))`, "true 1 two", false},
		{"error comes back", `local ok, e = T.nested(function() error("inner", 0) end) T.spy(tostring(ok), e)`, "nil inner", false},
		{"a yield is refused", `local ok, e = T.nested(function() T.echo(1) end) T.spy(tostring(ok), e)`, "nil async call inside a Go callback", false},
		{"a user yield is refused", `local ok, e = T.nested(function() coroutine.yield(1) end) T.spy(tostring(ok), e)`, "nil", false},
		{"runaway recursion is bounded", `
local function f() local ok = T.nested(f) return ok end
T.spy(tostring(f()))`, "", false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			if got := spyText(h); !strings.HasPrefix(got, r.want) {
				t.Fatalf("spy = %q, want prefix %q (log %v)", got, r.want, h.LogText(""))
			}
			if plugin.Coroutines(h.RT, "t") != 0 {
				t.Fatal("coroutine depth not restored")
			}
		})
	}
	t.Run("inherits the slice deadline", func(t *testing.T) {
		h := cmd(t, "", `T.nested(function() while true do end end) T.spy("not reached")`, plugintest.Options{Timeout: 40 * time.Millisecond})
		if spyText(h) != "" || !noticeHas(h, "timed out (40 ms)") {
			t.Fatalf("spy = %q notices = %v", spyText(h), h.NoticeTexts())
		}
	})
}

// ctx.count follows args.count whatever numeric type the Go caller used.
func TestCommandCountNumericTypes(t *testing.T) {
	rows := []struct {
		name  string
		count any
		want  string
	}{
		{"int64", int64(3), "3 3"},
		{"int", 3, "3 3"},
		{"float64", float64(3), "3 3"},
		{"zero is 1", 0, "1 0"},
		{"negative is 1", -2, "1 -2"},
		{"fraction is 1", 2.5, "1 2.5"},
		{"string ignored", "3", "1 3"},
		{"absent", nil, "1 nil"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			h := start(t, `T.command("go", function(ctx) T.spy(ctx.count, tostring(ctx.args.count)) end)`)
			args := map[string]any{}
			if row.count != nil {
				args["count"] = row.count
			}
			if err := h.Run("t.go", args); err != nil {
				t.Fatal(err)
			}
			if got := spyText(h); got != row.want {
				t.Fatalf("ctx.count / args.count = %q, want %q", got, row.want)
			}
		})
	}
}
