package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
)

const ms = time.Millisecond

func TestTimerAfter(t *testing.T) {
	h := cmd(t, "", `
gvim.timer.after(100, function() T.spy("fired") end)
local c = gvim.timer.after(50, function() T.spy("cancelled timer fired") end)
T.spy("cancel", c:cancel(), c:cancel(), c:active())`)
	if got := spyText(h); got != "cancel true false false" {
		t.Fatalf("spy = %q", got)
	}
	h.Advance(99 * ms)
	if got := spyText(h); strings.Contains(got, "fired") {
		t.Fatalf("fired early: %q", got)
	}
	h.Advance(1 * ms)
	if got := spyText(h); got != "cancel true false false|fired" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	h.Advance(time.Second)
	if got := spyText(h); got != "cancel true false false|fired" {
		t.Fatalf("a one-shot timer fired twice: %q", got)
	}
}

func TestTimerActive(t *testing.T) {
	h := cmd(t, "local t", `
t = gvim.timer.after(10, function() T.spy("fired", t:active()) end)
T.spy("before", t:active(), tostring(t):match("^Timer%(") ~= nil)`)
	h.Advance(10 * ms)
	if got := spyText(h); got != "before true true|fired false" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestTimerEveryRearmsAfterTheCallbackFinished(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local n = 0
local tm
T.command("quick", function()
  tm = gvim.timer.every(10, function() n = n + 1 T.spy("tick", n) end)
end)
T.command("slow", function()
  tm = gvim.timer.every(10, function() T.spy("start") gvim.timer.sleep(100) T.spy("end") end)
end)
T.command("stop", function() tm:cancel() end)`)
	if err := h.Run("t.quick", nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		h.Advance(10 * ms)
	}
	if got := spyText(h); got != "tick 1|tick 2|tick 3" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if err := h.Run("t.stop", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(10 * ms)
	if got := spyText(h); got != "tick 1|tick 2|tick 3" {
		t.Fatalf("a cancelled timer fired: %q", got)
	}

	plugin.ResetSpy(h.RT)
	if err := h.Run("t.slow", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(10 * ms) // the callback starts and sleeps
	if got, pend := spyText(h), h.PendingTimers(); got != "start" || pend < 1 {
		t.Fatalf("spy = %q pending timers = %d", got, pend)
	}
	// While the callback sleeps, the periodic timer is not armed: nothing piles up.
	h.Advance(99 * ms)
	if got := spyText(h); got != "start" {
		t.Fatalf("callbacks overlapped: %q", got)
	}
	h.Advance(1 * ms) // the callback ends, the timer re-arms
	h.Advance(10 * ms)
	if got := spyText(h); got != "start|end|start" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestTimerDebounce(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local d = gvim.timer.debounce(50, function(a, b) T.spy("deb", a, b) end)
T.command("call", function(ctx) d(ctx.args.a, ctx.args.b) end)`)
	call := func(a, b int) {
		t.Helper()
		if err := h.Run("t.call", map[string]any{"a": a, "b": b}); err != nil {
			t.Fatal(err)
		}
	}
	call(1, 1)
	h.Advance(30 * ms)
	call(2, 2)
	h.Advance(30 * ms) // 60 ms after the first call, 30 after the second
	if got := spyText(h); got != "" {
		t.Fatalf("not debounced: %q", got)
	}
	call(3, 4)
	h.Advance(49 * ms)
	if got := spyText(h); got != "" {
		t.Fatalf("fired early: %q", got)
	}
	h.Advance(1 * ms)
	if got := spyText(h); got != "deb 3 4" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	h.Advance(time.Second)
	if got := spyText(h); got != "deb 3 4" {
		t.Fatalf("fired again: %q", got)
	}
}

func TestTimerSleep(t *testing.T) {
	h := cmd(t, "", `
T.spy("a", gvim.timer.sleep(100))
T.spy("b", gvim.timer.sleep(0))`)
	if got := spyText(h); got != "" {
		t.Fatalf("sleep did not park: %q", got)
	}
	h.Advance(99 * ms)
	if got := spyText(h); got != "" {
		t.Fatalf("woke early: %q", got)
	}
	h.Advance(1 * ms)
	if got := spyText(h); got != "a true" {
		t.Fatalf("spy = %q", got)
	}
	h.Advance(1 * ms) // sleep(0) still yields to the event loop
	if got := spyText(h); got != "a true|b true" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestTimerArguments(t *testing.T) {
	rows := []struct{ name, body, want string }{
		{"after needs ms >= 1", `local ok, e = pcall(function() return gvim.timer.after(0, function() end) end) T.spy(ok, e:match("bad argument #1 to after"))`, "false bad argument #1 to after"},
		{"every needs ms >= 10", `local ok, e = pcall(function() return gvim.timer.every(9, function() end) end) T.spy(ok, e:match("bad argument #1 to every"))`, "false bad argument #1 to every"},
		{"ms is capped", `local ok, e = pcall(function() return gvim.timer.after(1e12, function() end) end) T.spy(ok, e:match("bad argument #1"))`, "false bad argument #1"},
		{"a function is needed", `local ok, e = pcall(function() return gvim.timer.after(10, "f") end) T.spy(ok, e:match("bad argument #2"))`, "false bad argument #2"},
		{"sleep rejects negative", `local ok, e = pcall(function() return gvim.timer.sleep(-1) end) T.spy(ok, e:match("bad argument #1 to 'sleep'"))`, "false bad argument #1 to 'sleep'"},
		{"debounce needs ms >= 1", `local ok = pcall(function() return gvim.timer.debounce(0, function() end) end) T.spy(ok)`, "false"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestTimerQuota(t *testing.T) {
	h := cmd(t, "", `
local last
for i = 1, 65 do
  local tm, e = gvim.timer.after(1000, function() end)
  if not tm then last = e end
end
T.spy(last)
local d = gvim.timer.debounce(10, function() end)
local ok, e = pcall(d)
T.spy(ok, e:match("limit: timer quota %(64%)"))`)
	if got := spyText(h); got != "limit: timer quota (64)|false limit: timer quota (64)" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestTimersDieWithThePlugin(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
gvim.timer.after(100, function() T.spy("after") end)
gvim.timer.every(20, function() T.spy("every") end)
gvim.timer.debounce(30, function() T.spy("debounce") end)()
gvim.async.spawn(function() gvim.timer.sleep(100) T.spy("slept") end)`)
	h.Unload("t")
	if rg := plugin.RegistryOf(h.RT); rg.Waits != 0 || rg.Tasks != 0 {
		t.Fatalf("registry after unload = %+v", rg)
	}
	h.Advance(time.Second)
	if got := spyText(h); got != "" {
		t.Fatalf("timers ran after the unload: %q", got)
	}
}

// TestSuspendTimeout: suspend and Task:wait take timeout_ms and return nil, "timeout: ...".
func TestSuspendTimeout(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local saved
T.command("never", function()
  local v, e = gvim.async.suspend(function(resume) saved = resume end, {timeout_ms = 100})
  T.spy("never", tostring(v), e)
end)
T.command("late", function() saved("too late") T.spy("resumed late") end)
T.command("in_time", function()
  local a, b = gvim.async.suspend(function(resume) gvim.timer.after(20, function() resume("x", "y") end) end, {timeout_ms = 100})
  T.spy("in_time", a, b)
end)
T.command("sync", function()
  T.spy("sync", gvim.async.suspend(function(resume) resume("now") end, {timeout_ms = 100}))
end)
T.command("wait", function()
  local task = gvim.async.spawn(function() gvim.timer.sleep(1000) return "done" end)
  local v, e = task:wait({timeout_ms = 50})
  T.spy("wait", tostring(v), e)
  T.spy("task still runs", not task:done())
  T.spy("wait again", task:wait({timeout_ms = 2000}))
end)
T.command("wait_fast", function()
  local task = gvim.async.spawn(function() gvim.timer.sleep(10) return "fast" end)
  T.spy("wait_fast", task:wait({timeout_ms = 500}))
end)
T.command("bad", function()
  T.spy(pcall(gvim.async.suspend, function() end, {timeout_ms = -5}))
end)`)

	run := func(name string) {
		t.Helper()
		if err := h.Run("t."+name, nil); err != nil {
			t.Fatal(err)
		}
	}
	reset := func() { plugin.ResetSpy(h.RT) }

	run("never")
	h.Advance(99 * ms)
	if got := spyText(h); got != "" {
		t.Fatalf("timed out early: %q", got)
	}
	h.Advance(1 * ms)
	if got := spyText(h); got != "never nil timeout: suspend timed out after 100 ms" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	run("late") // a resume after the timeout is ignored
	if got := spyText(h); !strings.HasSuffix(got, "resumed late") || strings.Count(got, "never") != 1 {
		t.Fatalf("spy = %q", got)
	}
	if rg := plugin.RegistryOf(h.RT); rg.Parked != 0 {
		t.Fatalf("registry = %+v", rg)
	}

	reset()
	run("in_time")
	h.Advance(20 * ms)
	if got := spyText(h); got != "in_time x y" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	h.Advance(time.Second) // the stopped timeout must not fire
	if got := spyText(h); got != "in_time x y" {
		t.Fatalf("spy = %q", got)
	}

	reset()
	run("sync")
	if got := spyText(h); got != "sync now" {
		t.Fatalf("spy = %q", got)
	}

	reset()
	run("wait")
	h.Advance(50 * ms)
	if got := spyText(h); got != "wait nil timeout: wait timed out after 50 ms|task still runs true" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	h.Advance(950 * ms)
	if got := spyText(h); !strings.HasSuffix(got, "wait again done") {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}

	reset()
	run("wait_fast")
	h.Advance(10 * ms)
	h.Advance(time.Second)
	if got := spyText(h); got != "wait_fast fast" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}

	reset()
	run("bad")
	if got := spyText(h); !strings.HasPrefix(got, "false") || !strings.Contains(got, "timeout_ms must be a positive number") {
		t.Fatalf("spy = %q", got)
	}
}
