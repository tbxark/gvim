package plugin_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// timed runs the command and returns how long it took.
func timed(t *testing.T, h *plugintest.Harness) time.Duration {
	t.Helper()
	t0 := time.Now()
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	return time.Since(t0)
}

func TestSliceDeadline(t *testing.T) {
	rows := []struct {
		name, body string
	}{
		{"endless loop", `while true do end`},
		{"hostile pcall loop", `while true do pcall(function() while true do end end) end`},
		{"nested pcall loop", `pcall(function() while true do pcall(function() while true do end end) end end) T.spy("not reached")`},
		{"loop in a coroutine", `coroutine.wrap(function() while true do end end)()`},
		{"loop in a coroutine under pcall", `pcall(coroutine.wrap(function() while true do end end))`},
		{"tail call spin", `local function f() return f() end f()`},
		{"loop after an await", `T.echo(1) while true do end`},
		{"loop in a spawned task", `local k = require("gvim").async.spawn(function() while true do end end) k:wait()`},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, "T.command('go', function() "+r.body+" end)", plugintest.Options{Timeout: 50 * time.Millisecond})
			d := timed(t, h)
			if d < 40*time.Millisecond || d > time.Second {
				t.Fatalf("took %v, want about 50ms", d)
			}
			if got := spyText(h); got != "" {
				t.Fatalf("code ran past the deadline: %q", got)
			}
			if !noticeHas(h, "timed out (50 ms), strike 1/3") {
				t.Fatalf("notices = %v log = %v", h.NoticeTexts(), h.LogText(""))
			}
			if status(h, "t") != "loaded" {
				t.Fatalf("status = %q", status(h, "t"))
			}
		})
	}
}

func TestStateUsableAfterTimeout(t *testing.T) {
	h := start(t, `
local n = 0
T.command("go", function() n = n + 1 if n == 1 then while true do end end T.spy("second run", n) end)`,
		plugintest.Options{Timeout: 30 * time.Millisecond})
	_ = timed(t, h)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "second run 2" {
		t.Fatalf("spy = %q", got)
	}
}

func TestStrikesSuspendAndExpire(t *testing.T) {
	h := start(t, `
T.command("go", function() while true do end end)
T.command("ok", function() T.spy("ok ran") end)
T.on("tick", function() T.spy("tick") end)`, plugintest.Options{Timeout: 20 * time.Millisecond})

	for i := 1; i <= 2; i++ {
		_ = timed(t, h)
		if !noticeHas(h, fmt.Sprintf("strike %d/3", i)) {
			t.Fatalf("after %d: notices = %v", i, h.NoticeTexts())
		}
	}
	// strikes expire after 60 s
	h.Advance(61 * time.Second)
	_ = timed(t, h)
	if n := plugin.Strikes(h.RT, "t"); n != 1 || status(h, "t") != "loaded" {
		t.Fatalf("strikes = %d status = %q", n, status(h, "t"))
	}
	// three within the window suspend
	_ = timed(t, h)
	_ = timed(t, h)
	if status(h, "t") != "suspended" {
		t.Fatalf("status = %q notices = %v", status(h, "t"), h.NoticeTexts())
	}
	if !noticeHas(h, "suspended after 3 strikes") {
		t.Fatalf("notices = %v", h.NoticeTexts())
	}
	// suspended: events are muted, commands still run
	h.Emit(pluginapi.Event{Name: "tick"})
	if h.RT.Wants("tick") {
		t.Fatal("a suspended plugin still wants events")
	}
	if err := h.Run("t.ok", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "ok ran" {
		t.Fatalf("spy = %q", got)
	}
}

func TestEventAndCommandBudgets(t *testing.T) {
	// With plugin_timeout_ms = 400, a user command may run 400 ms but an event handler only 50 ms.
	h := start(t, `
T.on("slow", function() while true do end end)
T.command("go", function() T.busy(120) T.spy("command done") end)`, plugintest.Options{Timeout: 400 * time.Millisecond})
	t0 := time.Now()
	h.Emit(pluginapi.Event{Name: "slow"})
	d := time.Since(t0)
	if d < 40*time.Millisecond || d > 300*time.Millisecond {
		t.Fatalf("event handler ran %v, want about 50ms", d)
	}
	if !noticeHas(h, "timed out (50 ms)") {
		t.Fatalf("notices = %v", h.NoticeTexts())
	}
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "command done" {
		t.Fatalf("a 120 ms command must fit its 400 ms budget: spy = %q log=%v", got, h.LogText(""))
	}
}

func TestLoadBudgetAndRollback(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"t.lua": `
T.command("c", function() end)
T.register_all()
while true do end`}, plugintest.Options{Timeout: 25 * time.Millisecond})
	t0 := time.Now()
	h.Start()
	d := time.Since(t0)
	if d < 80*time.Millisecond || d > time.Second {
		t.Fatalf("load took %v, want about 4x25ms", d)
	}
	p, _ := h.Plugin("t")
	if p.Status != "failed" || !strings.Contains(p.Err, "timed out") {
		t.Fatalf("plugin = %+v", p)
	}
	if res := h.Host.Resources(); len(res) != 0 {
		t.Fatalf("registrations not rolled back: %v", res)
	}
	if r := plugin.RegistryOf(h.RT); r.Commands != 0 || r.Tasks != 0 {
		t.Fatalf("registry = %+v", r)
	}
	if len(h.Host.Gone) == 0 {
		t.Fatal("OwnerGone was not called")
	}
}

func TestParkedLoadFinishes(t *testing.T) {
	// A main chunk may await; the plugin is "loading" until it returns.
	h := plugintest.New(t, plugintest.Files{"t.lua": `T.work("x", 30) T.spy("main done")`}, plugintest.Options{Timeout: 100 * time.Millisecond})
	h.RT.Start(h.Host)
	if err := h.RT.LoadPlugin(h.Sources[0]); err != nil {
		t.Fatal(err)
	}
	if s := status(h, "t"); s != "loading" {
		t.Fatalf("status = %q, want loading", s)
	}
	h.Settle()
	if s := status(h, "t"); s != "loaded" {
		t.Fatalf("status = %q log=%v", s, h.LogText(""))
	}
	if got := spyText(h); got != "main done" {
		t.Fatalf("spy = %q", got)
	}
}

func TestPerUpdateCapDefersWithMore(t *testing.T) {
	// 10 handlers of 10 ms each; a Drain with a 25 ms cap runs a few and reports more.
	h := start(t, `for i = 1, 10 do T.on("x", function() T.busy(10) T.spy("h" .. i) end) end`, plugintest.Options{Timeout: 500 * time.Millisecond, EventBudget: 500 * time.Millisecond})
	h.RT.Emit(pluginapi.Event{Name: "x"})
	_, more := h.RT.Drain(25 * time.Millisecond)
	if !more {
		t.Fatal("Drain should report remaining work")
	}
	first := len(spy(h))
	if first < 2 || first > 5 {
		t.Fatalf("ran %d handlers in one capped Drain", first)
	}
	h.Settle()
	if got := len(spy(h)); got != 10 {
		t.Fatalf("after settling %d handlers ran, want 10: %v", got, spy(h))
	}
	if spy(h)[0] != "h1" || spy(h)[9] != "h10" {
		t.Fatalf("FIFO order lost: %v", spy(h))
	}
}

func TestEventFairnessRoundRobin(t *testing.T) {
	// Two plugins, each with a 5 ms handler and a queue of four events: they take turns
	// event by event instead of the first draining its queue.
	src := func(name string) string {
		return `local n = 0 T.on("x", function() n = n + 1 T.busy(5) T.spy("` + name + `" .. n) end)`
	}
	// The 50 ms slice of an event handler is real time: on a loaded machine a 5 ms busy wait
	// is descheduled for longer and the handler times out, so the test raises the cap.
	h := startFiles(t, plugintest.Files{"a.lua": src("a"), "b.lua": src("b")}, plugintest.Options{Timeout: 500 * time.Millisecond, EventBudget: 500 * time.Millisecond})
	for i := 0; i < 4; i++ {
		h.RT.Emit(pluginapi.Event{Name: "x"})
	}
	h.Settle()
	if got := strings.Join(spy(h), " "); got != "a1 b1 a2 b2 a3 b3 a4 b4" {
		t.Fatalf("order = %q", got)
	}
}

func TestHandlersOfOnePluginRunTogether(t *testing.T) {
	// All handlers of a plugin for one event run before the next plugin's, in subscription order.
	src := func(name string) string {
		return `for i = 1, 3 do T.on("x", function() T.spy("` + name + `" .. i) end) end`
	}
	h := startFiles(t, plugintest.Files{"a.lua": src("a"), "b.lua": src("b")})
	h.Emit(pluginapi.Event{Name: "x"})
	if got := strings.Join(spy(h), " "); got != "a1 a2 a3 b1 b2 b3" {
		t.Fatalf("order = %q", got)
	}
}

func TestCascadeCapDropsHotKeepsLifecycle(t *testing.T) {
	// A handler that re-emits its own event every time is a storm. Hot events are dropped
	// (with a log line and a strike); lifecycle events are not.
	storm := `
local n = 0
T.on("EV", function() n = n + 1 T.emit("EV") end)
T.command("go", function() T.emit("EV") end)
T.command("count", function() T.spy(n) end)`
	t.Run("hot", func(t *testing.T) {
		h := start(t, strings.ReplaceAll(storm, "EV", "cursor_moved"), plugintest.Options{Timeout: 500 * time.Millisecond})
		_ = h.Run("t.go", nil)
		if !logHas(h, "", "event storm") {
			t.Fatalf("log = %v", h.LogText(""))
		}
		if plugin.Strikes(h.RT, "t") != 1 {
			t.Fatalf("strikes = %d", plugin.Strikes(h.RT, "t"))
		}
		_ = h.Run("t.count", nil)
		if n := spy(h)[len(spy(h))-1]; n == "0" || n == "1" {
			t.Fatalf("handler ran %s times", n)
		}
	})
	t.Run("lifecycle", func(t *testing.T) {
		// 20 chained deliveries exceed the 8 rounds of one Drain but none is dropped.
		h := start(t, `
local n = 0
T.on("after_file_open", function() n = n + 1 if n < 20 then T.emit("after_file_open") end end)
T.command("go", function() T.emit("after_file_open") end)
T.command("count", function() T.spy(n) end)`, plugintest.Options{Timeout: 500 * time.Millisecond})
		h.RT.Start(h.Host)
		if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
			t.Fatal(err)
		}
		_, more := h.RT.Drain(time.Second)
		if !more {
			t.Fatal("the chain must continue in a later Drain")
		}
		h.Settle()
		_ = h.Run("t.count", nil)
		if got := spy(h); got[len(got)-1] != "20" {
			t.Fatalf("spy = %v", got)
		}
		if logHas(h, "", "event storm") {
			t.Fatal("lifecycle events must never be dropped")
		}
	})
}

func TestSubscriptionDroppedAfterFiveFailures(t *testing.T) {
	h := start(t, `T.on("x", function() error("nope") end)`)
	for i := 0; i < 5; i++ {
		h.Emit(pluginapi.Event{Name: "x"})
	}
	if h.RT.Wants("x") {
		t.Fatal("subscription still active after 5 consecutive failures")
	}
	if !logHas(h, "t", "unsubscribed x after 5 consecutive failures") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	// Ordinary errors are not strikes, and the burst produced a single notice.
	if n := plugin.Strikes(h.RT, "t"); n != 0 {
		t.Fatalf("strikes = %d", n)
	}
	count := 0
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, "nope") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("notices for the burst = %d: %v", count, h.NoticeTexts())
	}
}

func TestSubscriptionOnceAndFailureStreakReset(t *testing.T) {
	h := start(t, `
local calls = 0
T.on("once", function() T.spy("once") end, true)
T.on("flaky", function() calls = calls + 1 if calls % 2 == 1 then error("odd", 0) end end)`)
	h.Emit(pluginapi.Event{Name: "once"})
	h.Emit(pluginapi.Event{Name: "once"})
	if got := spyText(h); got != "once" {
		t.Fatalf("once handler ran: %q", got)
	}
	for i := 0; i < 20; i++ {
		h.Emit(pluginapi.Event{Name: "flaky"})
	}
	if !h.RT.Wants("flaky") {
		t.Fatal("alternating failures must not unsubscribe (streak resets on success)")
	}
}

// ---------- host time and memory ----------

func TestHostTimeNotCharged(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.txt", "x\ny")
	// 120 ms in the editor is more than twice the 50 ms budget, and below the 200 ms hard
	// deadline (4 x budget), which is never paused.
	host.LinesDelay = 120 * time.Millisecond
	h := start(t, `T.command("go", function() T.spy(T.lines(1)) end)`, plugintest.Options{Timeout: 50 * time.Millisecond, Host: host})
	d := timed(t, h)
	if d < 110*time.Millisecond {
		t.Fatalf("took %v: the fake delay did not happen", d)
	}
	if got := spyText(h); got != "2" {
		t.Fatalf("spy = %q log=%v notices=%v", got, h.LogText(""), h.NoticeTexts())
	}
	if plugin.Strikes(h.RT, "t") != 0 {
		t.Fatalf("strikes = %d", plugin.Strikes(h.RT, "t"))
	}
}

func TestHardDeadlineStillEnds(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.txt", "x")
	host.LinesDelay = 20 * time.Millisecond
	h := start(t, `T.command("go", function() while true do T.lines(1) end end)`, plugintest.Options{Timeout: 50 * time.Millisecond, Host: host})
	d := timed(t, h)
	if d < 150*time.Millisecond || d > 2*time.Second {
		t.Fatalf("a loop of host calls ended after %v, want about 4 x 50ms", d)
	}
	if !logHas(h, "t", "slow host call") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	// the host was slow, not the plugin: no strike
	if plugin.Strikes(h.RT, "t") != 0 {
		t.Fatalf("strikes = %d", plugin.Strikes(h.RT, "t"))
	}
}

func TestCancelCauseTexts(t *testing.T) {
	rows := []struct {
		cause string
		want  string
	}{
		{"deadline", "timed out (50 ms)"},
		{"hard", "timed out (50 ms)"},
		{"memory", "memory limit"},
		{"cancelled", "cancelled"},
		{"plain", "boom"},
	}
	for _, r := range rows {
		t.Run(r.cause, func(t *testing.T) {
			kind, msg := plugin.ClassifyCause(r.cause, 50*time.Millisecond)
			if !strings.Contains(msg, r.want) {
				t.Fatalf("msg = %q (kind %s), want %q", msg, kind, r.want)
			}
		})
	}
}

func heapMiB() int {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int(m.HeapAlloc >> 20)
}

func TestHeapGuardTripsOnRetainedGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates memory")
	}
	h := start(t, `T.command("go", function()
  local keep = {}
  while true do keep[#keep + 1] = string.rep("x", 1024 * 1024) .. #keep end
  T.spy("survived")
end)`, plugintest.Options{Timeout: 5 * time.Second, HeapLimit: 64 << 20})
	d := timed(t, h)
	if got := spyText(h); got != "" {
		t.Fatalf("the allocation loop was not stopped (took %v, heap %d MiB)", d, heapMiB())
	}
	if !noticeHas(h, "memory limit") || !logHas(h, "t", "memory limit") {
		t.Fatalf("notices = %v log = %v", h.NoticeTexts(), h.LogText(""))
	}
	if plugin.Strikes(h.RT, "t") != 1 {
		t.Fatalf("strikes = %d", plugin.Strikes(h.RT, "t"))
	}
}

func TestHeapGuardIgnoresGarbageWithLargeLiveHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates memory")
	}
	// The process holds a large live heap; a plugin churns garbage past the absolute limit.
	// The threshold is max(limit, live/2) and the guard confirms with a forced GC, so no
	// strike. (Scaled down from 600 MiB live / 256 MiB limit: 300 MiB live, 32 MiB limit.)
	live := make([]byte, 300<<20)
	for i := 0; i < len(live); i += 4096 {
		live[i] = 1
	}
	h := start(t, `T.command("go", function()
  for i = 1, 30 do local s = string.rep("y", 16 * 1024 * 1024) end
  T.spy("churned")
end)`, plugintest.Options{Timeout: 10 * time.Second, HeapLimit: 32 << 20})
	_ = timed(t, h)
	runtime.KeepAlive(live)
	if got := spyText(h); got != "churned" {
		t.Fatalf("spy = %q log=%v notices=%v", got, h.LogText(""), h.NoticeTexts())
	}
	if plugin.Strikes(h.RT, "t") != 0 {
		t.Fatalf("strikes = %d", plugin.Strikes(h.RT, "t"))
	}
}

// A task that re-queues itself is user-class work without a strike (every slice is inside
// its budget), so Drain itself must bound it: it stops after about twice the per-Update cap
// and reports more.
func TestSelfRequeueingChainLeavesDrain(t *testing.T) {
	const capacity = 100 * time.Millisecond
	rows := []struct{ name, body string }{
		{"spawn", `local function f() T.busy(5) T.spy("n") gvim.async.spawn(f) end f()`},
		{"command.run", `T.busy(5) T.spy("n") T.run("t.go")`},
		{"slow command.run", `T.busy(40) T.spy("n") T.run("t.go")`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			h := start(t, `
local gvim = require "gvim"
T.command("go", function() `+row.body+` end)`, plugintest.Options{Timeout: 500 * time.Millisecond})
			if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
				t.Fatal(err)
			}
			prev := len(spy(h))
			for i := 0; i < 3; i++ {
				t0 := time.Now()
				_, more := h.RT.Drain(capacity)
				d := time.Since(t0)
				if !more {
					t.Fatalf("Drain %d: a self-requeueing chain must report more", i)
				}
				// Drain stops starting slices after 2 x capacity of them, and a started slice
				// is never cut short; the margin covers a slice stretched by a loaded machine.
				// A chain that was not bounded would never return.
				if d > 6*capacity {
					t.Fatalf("Drain %d took %v, want about 2 x %v", i, d, capacity)
				}
				if n := len(spy(h)); n <= prev {
					t.Fatalf("Drain %d made no progress (%d lines)", i, n)
				} else {
					prev = n
				}
			}
			if status(h, "t") != "loaded" {
				t.Fatalf("status = %q, want loaded (no strike)", status(h, "t"))
			}
			h.RT.UnloadPlugin("t")
			if _, more := h.RT.Drain(capacity); more {
				t.Fatal("unloading the plugin must end the chain")
			}
		})
	}
}

// Event-class work still runs in a Drain whose user-class budget is spent.
func TestUserChainDoesNotStarveEvents(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.on("x", function() T.spy("event") end)
T.command("go", function() T.busy(5) gvim.async.spawn(function() T.run("t.go") end) end)`, plugintest.Options{Timeout: 500 * time.Millisecond})
	if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	h.RT.Emit(pluginapi.Event{Name: "x"})
	if _, more := h.RT.Drain(50 * time.Millisecond); !more {
		t.Fatal("chain should leave work")
	}
	if got := spyText(h); got != "event" {
		t.Fatalf("spy = %q, want the event handler to have run", got)
	}
	h.RT.UnloadPlugin("t")
}

// A chain of user-class tasks that never ends keeps every Drain at its cap (100% CPU, a few
// Updates a second). Like an event storm it earns strikes, and the third suspends the
// plugin and ends the chain.
func TestSelfRequeueingChainIsSuspended(t *testing.T) {
	rows := []struct{ name, body string }{
		{"spawn", `local function f() gvim.async.spawn(f) end f()`},
		{"two spawns", `local function f() gvim.async.spawn(f) gvim.async.spawn(f) end f()`},
		{"command.run", `T.run("t.go")`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			h := start(t, `
local gvim = require "gvim"
T.command("go", function() `+row.body+` end)`, plugintest.Options{Timeout: 500 * time.Millisecond})
			if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
				t.Fatal(err)
			}
			drains := 0
			for ; drains < 200 && status(h, "t") != "suspended"; drains++ {
				h.RT.Drain(2 * time.Millisecond)
			}
			if status(h, "t") != "suspended" {
				t.Fatalf("status = %q after %d Drains, want suspended; log=%v", status(h, "t"), drains, h.LogText(""))
			}
			if got := plugin.Strikes(h.RT, "t"); got < 3 {
				t.Fatalf("strikes = %d, want 3", got)
			}
			if !logHas(h, "t", "task storm") {
				t.Fatalf("log = %v", h.LogText(""))
			}
			if _, more := h.RT.Drain(2 * time.Millisecond); more {
				t.Fatal("suspending the plugin must end the chain")
			}
		})
	}
}

// A finite user-class chain that spans several capped Drains is not a storm.
func TestFiniteUserChainIsNotAStorm(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("go", function()
  local n = 0
  local function f() n = n + 1 if n < 20 then T.busy(1) gvim.async.spawn(f) else T.spy("done") end end
  f()
end)`, plugintest.Options{Timeout: 500 * time.Millisecond})
	if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if _, more := h.RT.Drain(2 * time.Millisecond); !more {
			break
		}
	}
	if got := spyText(h); got != "done" || status(h, "t") != "loaded" {
		t.Fatalf("spy = %q status = %q log=%v", got, status(h, "t"), h.LogText(""))
	}
}
