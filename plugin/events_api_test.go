package plugin_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func ev(name string, data map[string]any) pluginapi.Event {
	return pluginapi.Event{Name: name, Data: data}
}

func TestEvents(t *testing.T) {
	t.Run("on delivers, once fires once, off stops", func(t *testing.T) {
		h := start(t, gv+`
local n = 0
local sub = gvim.on("after_file_save", function(e) n = n + 1 T.spy("on", e.path, n) end)
gvim.once("after_file_save", function(e) T.spy("once", e.path) end)
local gone = gvim.on("after_file_save", function() T.spy("never") end)
T.spy(gone:active(), tostring(gone))
gvim.off(gone)
T.spy(gone:active(), gvim.off(gone), gone:off())
gvim.command.register{ name = "off", run = function() T.spy("off", sub:off(), sub:active()) end }`)
		if got := spyText(h); got != "true Subscription(after_file_save)|false true true" {
			t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
		}
		plugin.ResetSpy(h.RT)
		h.Emit(ev("after_file_save", map[string]any{"path": "/a"}))
		h.Emit(ev("after_file_save", map[string]any{"path": "/b"}))
		if got := spyText(h); got != "on /a 1|once /a|on /b 2" {
			t.Fatalf("spy = %q", got)
		}
		_ = h.Run("t.off", nil)
		plugin.ResetSpy(h.RT)
		h.Emit(ev("after_file_save", map[string]any{"path": "/c"}))
		if got := spyText(h); got != "" {
			t.Fatalf("an unsubscribed handler ran: %q", got)
		}
		if h.RT.Wants("after_file_save") {
			t.Fatal("Wants still true after every subscription is gone")
		}
	})

	t.Run("handlers run in subscription order and see their own payload table", func(t *testing.T) {
		h := start(t, gv+`
gvim.on("theme_changed", function(e) e.mine = 1 T.spy("one", e.name) end)
gvim.on("theme_changed", function(e) T.spy("two", e.mine) end)`)
		h.Emit(ev("theme_changed", map[string]any{"name": "dark"}))
		if got := spyText(h); got != "one dark|two nil" {
			t.Fatalf("spy = %q", got)
		}
	})

	rows := []struct {
		name string
		lua  string
		want string
	}{
		{"unknown event raises", `local ok, err = pcall(gvim.on, "no_such_event", function() end) T.spy(ok, tostring(err):match("unknown event") or err)`, "false unknown event"},
		{"unknown event raises for once", `T.spy((pcall(gvim.once, "cursor_moveed", function() end)))`, "false"},
		{"plugin events are accepted", `T.spy(gvim.on("git:status", function() end) ~= nil)`, "true"},
		{"bad plugin event name raises", `T.spy((pcall(gvim.on, "Git:Status", function() end)))`, "false"},
		{"handler must be a function", `T.spy((pcall(gvim.on, "ready", "x")))`, "false"},
		{"opts.buf must be a Buffer", `T.spy((pcall(gvim.on, "ready", function() end, { buf = 1 })))`, "false"},
		{"debounce range", `
for _, ms in ipairs{ 0, -1, 60001, 0/0, 1/0 } do T.spy((pcall(gvim.on, "after_file_save", function() end, { debounce = ms }))) end
T.spy((pcall(gvim.on, "after_file_save", function() end, { debounce = "x" })))
T.spy(gvim.on("after_file_save", function() end, { debounce = 60000 }) ~= nil)`, "false|false|false|false|false|false|true"},
		{"veto handlers cannot be debounced", `T.spy((pcall(gvim.on, "before_file_save", function() end, { debounce = 10 })))`, "false"},
		{"after_ms is for idle only", `
T.spy((pcall(gvim.on, "ready", function() end, { after_ms = 10 })))
T.spy((pcall(gvim.on, "idle", function() end, { after_ms = 0 })))
T.spy((pcall(gvim.on, "idle", function() end, { after_ms = 0/0 })))
T.spy((pcall(gvim.on, "idle", function() end, { after_ms = 1/0 })))
T.spy(gvim.on("idle", function() end, { after_ms = 10 }) ~= nil)`, "false|false|false|false|true"},
		{"subscription quota", `
local n = 0
for i = 1, 300 do
  local s, err = gvim.on("ready", function() end)
  if not s then T.spy(n, gvim.err_code(err)) break end
  n = n + 1
end`, fmt.Sprintf("%d limit", plugin.QuotaSubscriptions)},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, gv+r.lua)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}

	t.Run("buf filter", func(t *testing.T) {
		host := plugintest.NewFakeHost()
		host.AddBuffer("a", "")
		host.AddBuffer("b", "")
		h := start(t, gv+`
local b2 = T.buf(2)
gvim.on("after_file_save", function(e) T.spy("save", tostring(e.buf)) end, { buf = b2 })
gvim.on("buffer_closed", function(e) T.spy("closed", e.id) end, { buf = b2 })`, plugintest.Options{Host: host})
		h.Emit(ev("after_file_save", map[string]any{"buf": pluginapi.BufferID(1)}))
		h.Emit(ev("after_file_save", map[string]any{"buf": pluginapi.BufferID(2)}))
		h.Emit(ev("after_file_save", nil))
		h.Emit(ev("buffer_closed", map[string]any{"id": 1}))
		h.Emit(ev("buffer_closed", map[string]any{"id": 2}))
		if got := spyText(h); got != "save Buffer(2)|closed 2" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("a handler error is logged and does not stop others", func(t *testing.T) {
		h := start(t, gv+`
gvim.on("ready", function() error("bug") end)
gvim.on("ready", function() T.spy("second") end)`)
		h.Emit(ev("ready", nil))
		if got := spyText(h); got != "second" || !logHas(h, "t", "bug") {
			t.Fatalf("spy = %q log = %v", got, h.LogText(""))
		}
	})

	t.Run("handlers may await", func(t *testing.T) {
		h := start(t, gv+`gvim.on("ready", function() T.spy("before") T.work("w", 5) T.spy("after") end)`)
		h.Emit(ev("ready", nil))
		if got := spyText(h); got != "before|after" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("unload drops subscriptions", func(t *testing.T) {
		h := startFiles(t, plugintest.Files{"base.lua": ``})
		before := plugin.RegistryOf(h.RT)
		if err := h.Load("p", gv+`gvim.on("ready", function() end) gvim.once("resize", function() end) gvim.on("after_file_save", function() end, { debounce = 10 })`); err != nil {
			t.Fatal(err)
		}
		h.Emit(ev("after_file_save", map[string]any{})) // arms a debounce timer
		if h.PendingTimers() == 0 {
			t.Fatal("no debounce timer is out")
		}
		h.Unload("p")
		if got := plugin.RegistryOf(h.RT); got != before {
			t.Fatalf("registry after unload = %+v, before = %+v", got, before)
		}
		h.Advance(time.Second) // the late timer is ignored
		if h.RT.Wants("ready") {
			t.Fatal("subscription outlived its plugin")
		}
	})
}

func TestEmit(t *testing.T) {
	files := plugintest.Files{
		"a.lua": gv + `
gvim.on("a:ping", function(e) T.spy("a got", e.n, #e.list, e.nested.x, tostring(e.buf)) end)
gvim.command.register{ name = "go", run = function()
  T.spy(gvim.emit("a:ping", { n = 1, list = { 7, 8 }, nested = { x = "y" }, buf = T.buf(3) }))
  T.spy(gvim.emit("a:nobody"))
end }
gvim.command.register{ name = "bad", run = function()
  for _, name in ipairs{ "b:ping", "ping", "a:", ":ping", "a:Ping", "A:ping", "a:ping:x", "ready" } do
    local ok, err = gvim.emit(name)
    T.spy(name, ok, gvim.err_code(err))
  end
  T.spy((pcall(gvim.emit, {})))
  T.spy((pcall(gvim.emit, "a:ping", "text")))
  T.spy((pcall(gvim.emit, "a:ping", { 1, 2 })))
  local ok, err = gvim.emit("a:ping", { f = function() end })
  T.spy(ok, gvim.err_code(err))
end }
gvim.command.register{ name = "big", run = function()
  T.spy(gvim.emit("a:ping", { n = 1, list = {}, nested = { x = "y" }, s = ("x"):rep(60000) }))
  local ok, err = gvim.emit("a:ping", { s = ("x"):rep(70000) })
  T.spy(ok, gvim.err_code(err))
end }
gvim.command.register{ name = "flood", run = function()
  local n = 0
  for i = 1, 1500 do
    if gvim.emit("a:ping", { n = i, list = {}, nested = { x = "" } }) then n = n + 1 end
  end
  T.spy("accepted", n)
end }`,
		"b.lua": gv + `
gvim.on("a:ping", function(e) T.spy("b got", e.n) end)`,
	}
	t.Run("delivered to every subscriber of the name, payload converted", func(t *testing.T) {
		host := plugintest.NewFakeHost()
		host.AddBuffer("x", "")
		host.AddBuffer("y", "")
		host.AddBuffer("z", "")
		h := startFiles(t, files, plugintest.Options{Host: host})
		_ = h.Run("a.go", nil)
		if got := spyText(h); got != "true|true|a got 1 2 y Buffer(3)|b got 1" {
			t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
		}
	})
	t.Run("name and payload rules", func(t *testing.T) {
		h := startFiles(t, files)
		_ = h.Run("a.bad", nil)
		var want []string
		for _, name := range []string{"b:ping", "ping", "a:", ":ping", "a:Ping", "A:ping", "a:ping:x", "ready"} {
			want = append(want, name+" nil invalid")
		}
		want = append(want, "false", "false", "false", "nil invalid")
		if got := spy(h); !reflect.DeepEqual(got, want) {
			t.Fatalf("spy =\n%q\nwant\n%q", got, want)
		}
	})
	t.Run("payload size", func(t *testing.T) {
		h := startFiles(t, files)
		_ = h.Run("a.big", nil)
		got := spy(h)
		if len(got) < 3 || got[0] != "true" || got[1] != "nil limit" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("a plugin cannot flood the queue", func(t *testing.T) {
		h := startFiles(t, files)
		_ = h.Run("a.flood", nil)
		if got := spy(h); len(got) == 0 || got[0] != "accepted 1000" {
			t.Fatalf("first spy = %q", got[:1])
		}
	})
}

func TestEventDebounce(t *testing.T) {
	const src = gv + `
gvim.on("after_file_save", function(e) T.spy("saved", e.path) end, { debounce = 100 })
gvim.on("buffer_changed", function(e)
  local texts = {}
  for i, r in ipairs(e.edits or {}) do texts[i] = r.text end
  T.spy("changed", tostring(e.buf), table.concat(texts), e.version, e.undo == true, e.overflow == true)
end, { debounce = 100 })
gvim.once("resize", function(e) T.spy("resize once", e.width) end, { debounce = 50 })`
	save := func(p string) pluginapi.Event {
		return ev("after_file_save", map[string]any{"path": p})
	}
	t.Run("trailing edge with the latest payload, re-armed while events keep coming", func(t *testing.T) {
		h := start(t, src)
		h.Emit(save("/1"))
		h.Advance(50 * time.Millisecond)
		h.Emit(save("/2"))
		h.Advance(60 * time.Millisecond) // 110 ms after the first, 60 after the last
		if got := spyText(h); got != "" {
			t.Fatalf("fired too early: %q", got)
		}
		h.Advance(30 * time.Millisecond)
		if got := spyText(h); got != "" {
			t.Fatalf("fired too early: %q", got)
		}
		h.Advance(10 * time.Millisecond)
		if got := spyText(h); got != "saved /2" {
			t.Fatalf("spy = %q", got)
		}
		h.Advance(time.Second)
		if got := spyText(h); got != "saved /2" {
			t.Fatalf("a quiet period fired again: %q", got)
		}
		h.Emit(save("/3"))
		h.Advance(100 * time.Millisecond)
		if got := spyText(h); got != "saved /2|saved /3" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("buffer_changed edits are concatenated across the window, per buffer", func(t *testing.T) {
		h := start(t, src)
		change := func(buf int, text string, ver uint64, undo bool) pluginapi.Event {
			return ev("buffer_changed", map[string]any{
				"buf": pluginapi.BufferID(buf), "version": pluginapi.Version{Gen: 1, Ver: ver}, "undo": undo,
				"edits": []pluginapi.ChangeRec{{Text: text}},
			})
		}
		h.Emit(change(1, "a", 1, false))
		h.Emit(change(2, "x", 7, false))
		h.Advance(40 * time.Millisecond)
		h.Emit(change(1, "b", 2, true))
		h.Advance(60 * time.Millisecond) // buffer 2 is quiet for 100 ms now
		if got := spyText(h); got != "changed Buffer(2) x 1.7 false false" {
			t.Fatalf("spy = %q", got)
		}
		h.Advance(40 * time.Millisecond)
		h.Advance(time.Millisecond)
		if got := spyText(h); got != "changed Buffer(2) x 1.7 false false|changed Buffer(1) ab 1.2 true false" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("once", func(t *testing.T) {
		h := start(t, src)
		h.Emit(ev("resize", map[string]any{"width": 80}))
		h.Emit(ev("resize", map[string]any{"width": 90}))
		h.Advance(time.Second)
		if got := spyText(h); got != "resize once 90" {
			t.Fatalf("spy = %q", got)
		}
		h.Emit(ev("resize", map[string]any{"width": 100}))
		h.Advance(time.Second)
		if len(spy(h)) != 1 {
			t.Fatalf("a once subscription ran again: %q", spy(h))
		}
	})
	t.Run("a suspended plugin gets nothing", func(t *testing.T) {
		h := start(t, src+`
gvim.command.register{ name = "hang", run = function() while true do end end }`, plugintest.Options{Timeout: 15 * time.Millisecond})
		h.Emit(save("/1"))
		for i := 0; i < 3; i++ {
			_ = h.Run("t.hang", nil)
		}
		h.Advance(time.Second)
		if got := spyText(h); got != "" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("more waiting buffers than the limit are delivered at once", func(t *testing.T) {
		h := start(t, gv+`gvim.on("buffer_modified", function(e) T.spy("m") end, { debounce = 100 })`)
		for i := 1; i <= 70; i++ {
			h.RT.Emit(ev("buffer_modified", map[string]any{"buf": pluginapi.BufferID(i), "modified": true}))
		}
		h.Settle()
		if n := len(spy(h)); n != 6 { // 64 are held, the 6 beyond the limit run now
			t.Fatalf("%d immediate deliveries, want 6", n)
		}
		h.Advance(150 * time.Millisecond)
		if n := len(spy(h)); n != 70 {
			t.Fatalf("%d deliveries in the end, want 70", n)
		}
	})
}

// Debounce timers are in-flight Work: a plugin cannot hold more than QuotaWork of them, and
// the events beyond the quota are delivered at once (S7-2).
func TestDebounceTimersAreBoundedByWorkQuota(t *testing.T) {
	t.Run("many subscriptions", func(t *testing.T) {
		h := start(t, gv+`
for i = 1, 200 do gvim.on("cursor_moved", function() T.spy("c") end, { debounce = 60000 }) end`)
		h.RT.Emit(ev("cursor_moved", map[string]any{"buf": pluginapi.BufferID(1)}))
		h.Settle()
		if n := h.PendingTimers(); n != plugin.QuotaWork {
			t.Fatalf("%d timers, want the quota %d", n, plugin.QuotaWork)
		}
		if n := len(spy(h)); n != 200-plugin.QuotaWork {
			t.Fatalf("%d immediate deliveries, want %d", n, 200-plugin.QuotaWork)
		}
	})
	t.Run("one timer per subscription however many buffers", func(t *testing.T) {
		h := start(t, gv+`gvim.on("cursor_moved", function() T.spy("c") end, { debounce = 100 })`)
		for i := 1; i <= 64; i++ {
			h.RT.Emit(ev("cursor_moved", map[string]any{"buf": pluginapi.BufferID(i)}))
		}
		h.Settle()
		if n := h.PendingTimers(); n != 1 {
			t.Fatalf("%d timers, want 1", n)
		}
		h.Advance(150 * time.Millisecond)
		if n := len(spy(h)); n != 64 {
			t.Fatalf("%d deliveries, want 64", n)
		}
		if n := h.PendingTimers(); n != 0 {
			t.Fatalf("%d timers left", n)
		}
	})
}

func TestEventIdleAfterMS(t *testing.T) {
	idle := func(ms int) pluginapi.Event { return ev("idle", map[string]any{"ms": ms}) }
	rows := []struct {
		name   string
		opts   string
		events []int
		want   int
	}{
		{"default 1000 fires once per period", ``, []int{250, 500, 1000, 1500, 2000}, 1},
		{"a new period fires again", ``, []int{250, 1000, 1500, 250, 1000}, 2},
		{"the same value again is a new period", ``, []int{1000, 1000, 1000}, 3},
		{"after_ms 300", `, { after_ms = 300 }`, []int{250, 500, 750}, 1},
		{"below the threshold never", `, { after_ms = 5000 }`, []int{250, 1000, 2000}, 0},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, gv+`gvim.on("idle", function(e) T.spy("idle", e.ms) end`+r.opts+`)`)
			for _, ms := range r.events {
				h.Emit(idle(ms))
			}
			if n := len(spy(h)); n != r.want {
				t.Fatalf("handler ran %d times (%q), want %d", n, spy(h), r.want)
			}
		})
	}
	t.Run("the period number, not ms, starts a new period (S14-3)", func(t *testing.T) {
		h := start(t, gv+`gvim.on("idle", function(e) T.spy("idle", e.ms) end, { after_ms = 100 })`)
		// Jitter: the first ms of each short pause is larger than the last of the one before.
		for _, e := range []struct{ ms, period int }{{105, 1}, {107, 2}, {100, 3}, {103, 4}, {203, 4}, {304, 4}} {
			h.Emit(ev("idle", map[string]any{"ms": e.ms, "period": e.period}))
		}
		if n := len(spy(h)); n != 4 {
			t.Fatalf("handler ran %d times (%q), want once per period (4)", n, spy(h))
		}
	})
	t.Run("an idle event without ms passes", func(t *testing.T) {
		h := start(t, gv+`gvim.on("idle", function() T.spy("idle") end)`)
		h.Emit(ev("idle", nil))
		if len(spy(h)) != 1 {
			t.Fatalf("spy = %q", spy(h))
		}
	})
}

func TestVetoThroughTheAPI(t *testing.T) {
	rows := []struct {
		name    string
		lua     string
		cancel  bool
		reason  string
		strikes int
	}{
		{"false cancels", `gvim.on("before_file_save", function() return false end)`, true, "", 0},
		{"false with reason", `gvim.on("before_file_save", function() return false, "read-only mount" end)`, true, "read-only mount", 0},
		{"no return keeps the save", `gvim.on("before_file_save", function() end)`, false, "", 0},
		{"a yield is refused", `gvim.on("before_file_save", function() T.echo(1) return false end)`, false, "", 0},
		{"buf filter hit", `gvim.on("before_file_save", function() return false end, { buf = T.buf(1) })`, true, "", 0},
		{"buf filter miss", `gvim.on("before_file_save", function() return false end, { buf = T.buf(2) })`, false, "", 0},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, gv+r.lua)
			cancel, reason := h.RT.Veto(ev("before_file_save", map[string]any{"buf": pluginapi.BufferID(1), "path": "/x"}))
			if cancel != r.cancel || reason != r.reason {
				t.Fatalf("Veto = %v %q, want %v %q (log %v)", cancel, reason, r.cancel, r.reason, h.LogText(""))
			}
		})
	}
	t.Run("once runs for one save only", func(t *testing.T) {
		h := start(t, gv+`
local sub = gvim.once("before_file_save", function() T.spy("veto once") return false, "first only" end)
gvim.command.register{ name = "active", run = function() T.spy("active", sub:active()) end }`)
		c1, r1 := h.RT.Veto(ev("before_file_save", nil))
		c2, _ := h.RT.Veto(ev("before_file_save", nil))
		if !c1 || r1 != "first only" || c2 {
			t.Fatalf("first = %v %q, second = %v", c1, r1, c2)
		}
		_ = h.Run("t.active", nil)
		if got := spyText(h); got != "veto once|active false" {
			t.Fatalf("spy = %q", got)
		}
		if h.RT.Wants("before_file_save") {
			t.Fatal("a spent once subscription is still wanted")
		}
	})
}

// AMENDMENTS C25: before_file_save carries source = "manual" | "autosave" | "api".
func TestBeforeFileSaveSourceField(t *testing.T) {
	h := start(t, gv+`
gvim.on("before_file_save", function(e)
  T.spy(e.source, e.path, tostring(e.buf))
  if e.source == "autosave" then return false, "no autosave" end
end)`)
	rows := []struct {
		source string
		cancel bool
		reason string
	}{
		{"manual", false, ""},
		{"autosave", true, "no autosave"},
		{"api", false, ""},
	}
	for _, r := range rows {
		cancel, reason := h.RT.Veto(ev("before_file_save", map[string]any{"buf": pluginapi.BufferID(4), "path": "/p", "source": r.source}))
		if cancel != r.cancel || reason != r.reason {
			t.Errorf("source %q: Veto = %v %q, want %v %q", r.source, cancel, reason, r.cancel, r.reason)
		}
	}
	want := []string{"manual /p Buffer(4)", "autosave /p Buffer(4)", "api /p Buffer(4)"}
	if got := spy(h); !reflect.DeepEqual(got, want) {
		t.Fatalf("spy = %q, want %q", got, want)
	}
}

// AMENDMENTS C25: only hot events are droppable under the cascade cap; lifecycle events go
// to the next Drain.
func TestHotEventsDroppedLifecycleKept(t *testing.T) {
	const n = 1500
	emitAll := func(h *plugintest.Harness, name string) {
		for i := 1; i <= n; i++ {
			h.RT.Emit(ev(name, map[string]any{"buf": pluginapi.BufferID(i)}))
		}
	}
	t.Run("hot events beyond the invocation cap are dropped", func(t *testing.T) {
		h := start(t, gv+`gvim.on("cursor_moved", function() T.spy("c") end)`, plugintest.Options{Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
		emitAll(h, "cursor_moved")
		h.RT.Drain(time.Hour)
		h.Settle()
		if got := len(spy(h)); got != 1000 {
			t.Fatalf("handler ran %d times, want 1000 (the cap)", got)
		}
		if !logHas(h, "", "event storm: dropped 500 hot events") {
			t.Fatalf("log = %v", h.LogText(""))
		}
	})
	t.Run("lifecycle events are never dropped", func(t *testing.T) {
		h := start(t, gv+`gvim.on("after_file_open", function() T.spy("o") end)`, plugintest.Options{Timeout: 5 * time.Second})
		emitAll(h, "after_file_open")
		more := true
		for i := 0; more && i < 10; i++ {
			_, more = h.RT.Drain(time.Hour)
		}
		h.Settle()
		if got := len(spy(h)); got != n {
			t.Fatalf("handler ran %d times, want %d", got, n)
		}
		if logHas(h, "", "event storm") {
			t.Fatal("lifecycle events were dropped as a storm")
		}
	})
	t.Run("plugin events are lifecycle", func(t *testing.T) {
		h := start(t, gv+`
local n = 0
gvim.on("t:again", function() n = n + 1 if n < 30 then gvim.emit("t:again") end end)
gvim.command.register{ name = "go", run = function() gvim.emit("t:again") end }
gvim.command.register{ name = "count", run = function() T.spy(n) end }`, plugintest.Options{Timeout: 500 * time.Millisecond})
		_ = h.Run("t.go", nil)
		_ = h.Run("t.count", nil)
		if got := spyText(h); got != "30" || logHas(h, "", "event storm") {
			t.Fatalf("spy = %q log = %v", got, h.LogText(""))
		}
	})
}

// A plugin event handler that keeps re-emitting its own event is never dropped (C25), but
// the runaway plugin is struck and finally suspended (S7-3).
func TestSelfFeedingPluginEventIsStruck(t *testing.T) {
	h := start(t, gv+`
gvim.on("t:a", function() gvim.emit("t:a") end)
gvim.command.register{ name = "go", run = function() gvim.emit("t:a") end }`, plugintest.Options{Timeout: 500 * time.Millisecond})
	h.RT.Start(h.Host)
	if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	var more bool
	for i := 0; i < 50 && status(h, "t") == "loaded"; i++ {
		_, more = h.RT.Drain(100 * time.Millisecond)
	}
	if !logHas(h, "", "event storm") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	if got := status(h, "t"); got != "suspended" {
		t.Fatalf("status = %q (more=%v strikes=%d), want suspended", got, more, plugin.Strikes(h.RT, "t"))
	}
}

// changed builds a buffer_changed event with one record per text.
func changed(buf int, ver uint64, undo bool, texts ...string) pluginapi.Event {
	recs := make([]pluginapi.ChangeRec, len(texts))
	for i, s := range texts {
		recs[i] = pluginapi.ChangeRec{Text: s, Range: pluginapi.Range{From: pluginapi.Pos{Line: i}, To: pluginapi.Pos{Line: i}}}
	}
	return ev("buffer_changed", map[string]any{
		"buf": pluginapi.BufferID(buf), "version": pluginapi.Version{Gen: 1, Ver: ver}, "undo": undo, "edits": recs,
	})
}

const changedProbe = gv + `
gvim.on("buffer_changed", function(e)
  local texts = {}
  for i, r in ipairs(e.edits or {}) do texts[i] = r.text end
  T.spy(tostring(e.buf), #texts, table.concat(texts, ","), e.version, tostring(e.undo == true), tostring(e.overflow == true), e.edits == nil)
end)`

// AMENDMENTS C25: buffer_changed coalesces by concatenation for one subscription and buffer.
func TestBufferChangedCoalescesByConcatenation(t *testing.T) {
	t.Run("one invocation with the edits in arrival order, undo OR-ed, latest version", func(t *testing.T) {
		h := start(t, changedProbe)
		h.RT.Emit(changed(1, 5, false, "a", "b"))
		h.RT.Emit(changed(1, 6, true, "c"))
		h.RT.Emit(changed(1, 7, false, "d"))
		h.Settle()
		if got := spyText(h); got != "Buffer(1) 4 a,b,c,d 1.7 true false false" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("buffers are kept apart, in order of first event", func(t *testing.T) {
		h := start(t, changedProbe)
		h.RT.Emit(changed(1, 1, false, "a"))
		h.RT.Emit(changed(2, 9, false, "x"))
		h.RT.Emit(changed(1, 2, false, "b"))
		h.RT.Emit(changed(2, 10, false, "y"))
		h.Settle()
		if got := spyText(h); got != "Buffer(1) 2 a,b 1.2 false false false|Buffer(2) 2 x,y 1.10 false false false" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("each subscription is merged on its own", func(t *testing.T) {
		h := start(t, changedProbe+`
gvim.on("buffer_changed", function(e) T.spy("second", #e.edits) end)`)
		h.RT.Emit(changed(1, 1, false, "a"))
		h.RT.Emit(changed(1, 2, false, "b"))
		h.Settle()
		if got := spyText(h); got != "Buffer(1) 2 a,b 1.2 false false false|second 2" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("a handler that already started is not merged into", func(t *testing.T) {
		h := start(t, gv+`gvim.on("buffer_changed", function(e) T.spy("n", #e.edits) T.host() end)`)
		h.Emit(changed(1, 1, false, "a"))
		h.Emit(changed(1, 2, false, "b", "c"))
		if got := spyText(h); got != "n 1|token:1|n 2|token:2" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("the merged edits are sequential: applying them reproduces the text", func(t *testing.T) {
		// Two records in the coordinates of the document before each: insert "X" at col 0, then
		// insert "Y" at col 1 (after the X).
		h := start(t, gv+`
gvim.on("buffer_changed", function(e)
  local text = "abc"
  for _, r in ipairs(e.edits) do
    local c = r.range.from.col
    text = text:sub(1, c) .. r.text .. text:sub(c + 1)
  end
  T.spy(text)
end)`)
		rec := func(col int, text string) pluginapi.ChangeRec {
			return pluginapi.ChangeRec{Range: pluginapi.Range{From: pluginapi.Pos{Col: col}, To: pluginapi.Pos{Col: col}}, Text: text}
		}
		h.RT.Emit(ev("buffer_changed", map[string]any{"buf": pluginapi.BufferID(1), "edits": []pluginapi.ChangeRec{rec(0, "X")}}))
		h.RT.Emit(ev("buffer_changed", map[string]any{"buf": pluginapi.BufferID(1), "edits": []pluginapi.ChangeRec{rec(1, "Y")}}))
		h.Settle()
		if got := spyText(h); got != "XYabc" {
			t.Fatalf("spy = %q", got)
		}
	})
}

// AMENDMENTS C25: more than 256 records or 256 KiB of text makes the merged event overflow.
func TestBufferChangedOverflowOnLargePayload(t *testing.T) {
	many := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "x"
		}
		return out
	}
	rows := []struct {
		name   string
		events []pluginapi.Event
		want   string
	}{
		{"exactly 256 records do not overflow",
			[]pluginapi.Event{changed(1, 1, false, many(128)...), changed(1, 2, false, many(128)...)},
			"Buffer(1) 256 " + strings.TrimSuffix(strings.Repeat("x,", 256), ",") + " 1.2 false false false"},
		{"257 records overflow",
			[]pluginapi.Event{changed(1, 1, false, many(128)...), changed(1, 2, false, many(129)...)},
			"Buffer(1) 0  1.2 false true true"},
		{"text beyond 256 KiB overflows",
			[]pluginapi.Event{changed(1, 1, false, strings.Repeat("a", 200<<10)), changed(1, 2, false, strings.Repeat("b", 100<<10))},
			"Buffer(1) 0  1.2 false true true"},
		{"text just below 256 KiB does not",
			[]pluginapi.Event{changed(1, 1, false, strings.Repeat("a", 100<<10)), changed(1, 2, false, strings.Repeat("b", 100<<10))},
			"-"},
		{"an overflowed event makes the result overflow",
			[]pluginapi.Event{changed(1, 1, false, "a"), ev("buffer_changed", map[string]any{
				"buf": pluginapi.BufferID(1), "version": pluginapi.Version{Gen: 1, Ver: 2}, "overflow": true})},
			"Buffer(1) 0  1.2 false true true"},
		{"overflow sticks",
			[]pluginapi.Event{ev("buffer_changed", map[string]any{"buf": pluginapi.BufferID(1), "overflow": true}), changed(1, 3, true, "a")},
			"Buffer(1) 0  1.3 true true true"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, changedProbe, plugintest.Options{Timeout: 5 * time.Second})
			for _, e := range r.events {
				h.RT.Emit(e)
			}
			h.Settle()
			got := spy(h)
			if len(got) != 1 {
				t.Fatalf("handler ran %d times", len(got))
			}
			if r.want == "-" {
				if !strings.Contains(got[0], " false false false") || strings.Contains(got[0], "true") {
					t.Fatalf("spy = %.80q", got[0])
				}
				return
			}
			if got[0] != r.want {
				t.Fatalf("spy = %.200q, want %.200q", got[0], r.want)
			}
		})
	}
}

func TestHotEventCoalescing(t *testing.T) {
	cursor := func(buf, line, prev int) pluginapi.Event {
		return ev("cursor_moved", map[string]any{
			"buf": pluginapi.BufferID(buf), "pos": pluginapi.Pos{Line: line}, "previous": pluginapi.Pos{Line: prev},
		})
	}
	t.Run("cursor_moved keeps the earliest previous and the latest position", func(t *testing.T) {
		h := start(t, gv+`gvim.on("cursor_moved", function(e) T.spy(tostring(e.buf), e.previous.line, e.pos.line) end)`)
		h.RT.Emit(cursor(1, 1, 0))
		h.RT.Emit(cursor(2, 8, 7))
		h.RT.Emit(cursor(1, 2, 1))
		h.RT.Emit(cursor(1, 3, 2))
		h.Settle()
		if got := spyText(h); got != "Buffer(1) 0 3|Buffer(2) 7 8" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("a field the latest event lacks disappears", func(t *testing.T) {
		h := start(t, gv+`gvim.on("selection_changed", function(e) T.spy(tostring(e.range)) end)`)
		h.RT.Emit(ev("selection_changed", map[string]any{"buf": pluginapi.BufferID(1), "range": pluginapi.Range{To: pluginapi.Pos{Col: 3}}}))
		h.RT.Emit(ev("selection_changed", map[string]any{"buf": pluginapi.BufferID(1)}))
		h.Settle()
		if got := spyText(h); got != "nil" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("buffer_modified and viewport_changed deliver the latest state", func(t *testing.T) {
		h := start(t, gv+`
gvim.on("buffer_modified", function(e) T.spy("mod", e.modified, e.version) end)
gvim.on("viewport_changed", function(e) T.spy("view", e.top_line) end)`)
		for i := 1; i <= 3; i++ {
			h.RT.Emit(ev("buffer_modified", map[string]any{"buf": pluginapi.BufferID(1), "modified": i%2 == 1, "version": pluginapi.Version{Gen: 1, Ver: uint64(i)}}))
			h.RT.Emit(ev("viewport_changed", map[string]any{"buf": pluginapi.BufferID(1), "top_line": i * 10}))
		}
		h.Settle()
		if got := spyText(h); got != "mod true 1.3|view 30" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("events that are not merged: lifecycle, commands, idle", func(t *testing.T) {
		h := start(t, gv+`
gvim.on("after_file_save", function() T.spy("save") end)
gvim.on("pre_command", function(e) T.spy("pre", e.action) end)
gvim.on("post_command", function(e) T.spy("post", e.action) end)`)
		for i := 0; i < 3; i++ {
			h.RT.Emit(ev("after_file_save", map[string]any{"buf": pluginapi.BufferID(1)}))
			h.RT.Emit(ev("pre_command", map[string]any{"action": fmt.Sprint("a", i)}))
			h.RT.Emit(ev("post_command", map[string]any{"action": fmt.Sprint("a", i)}))
		}
		h.Settle()
		if n := len(spy(h)); n != 9 {
			t.Fatalf("%d invocations, want 9: %q", n, spy(h))
		}
	})
	t.Run("an event without a buffer is not merged", func(t *testing.T) {
		h := start(t, gv+`gvim.on("cursor_moved", function() T.spy("c") end)`)
		h.RT.Emit(ev("cursor_moved", nil))
		h.RT.Emit(ev("cursor_moved", nil))
		h.Settle()
		if n := len(spy(h)); n != 2 {
			t.Fatalf("%d invocations, want 2", n)
		}
	})
	t.Run("once takes the first event", func(t *testing.T) {
		h := start(t, gv+`gvim.once("cursor_moved", function(e) T.spy("c", e.pos.line) end)`)
		h.RT.Emit(cursor(1, 1, 0))
		h.RT.Emit(cursor(1, 2, 1))
		h.Settle()
		h.Emit(cursor(1, 3, 2))
		if got := spyText(h); got != "c 1" {
			t.Fatalf("spy = %q", got)
		}
	})
}

// AMENDMENTS C25: shutdown handlers run in Stop, 200 ms for all plugins, min(50 ms,
// remaining) each, without yielding.
func TestShutdownHandlersRunInStopWithin200msTotal(t *testing.T) {
	t.Run("a hanging handler does not stop the others", func(t *testing.T) {
		h := startFiles(t, plugintest.Files{
			"a.lua": gv + `gvim.on("shutdown", function() while true do end end)`,
			"b.lua": gv + `gvim.on("shutdown", function() T.spy("b shutdown") end)`,
			"c.lua": gv + `gvim.on("shutdown", function() error("c fails") end)`,
			"d.lua": gv + `gvim.on("shutdown", function() T.spy("d start") T.echo(1) T.spy("d yielded") end)`,
		}, plugintest.Options{Timeout: 400 * time.Millisecond})
		t0 := time.Now()
		h.RT.Stop()
		if d := time.Since(t0); d > 400*time.Millisecond {
			t.Fatalf("Stop took %v", d)
		}
		if got := spyText(h); got != "b shutdown|d start" {
			t.Fatalf("spy = %q", got)
		}
	})
	t.Run("the total is bounded however many plugins hang", func(t *testing.T) {
		files := plugintest.Files{}
		for i := 0; i < 8; i++ {
			files[fmt.Sprintf("p%d.lua", i)] = gv + `gvim.on("shutdown", function() while true do end end)`
		}
		h := startFiles(t, files, plugintest.Options{Timeout: 400 * time.Millisecond})
		t0 := time.Now()
		h.RT.Stop()
		if d := time.Since(t0); d > 500*time.Millisecond {
			t.Fatalf("Stop took %v with eight hanging plugins; the budget is 200 ms in total", d)
		}
	})
}
