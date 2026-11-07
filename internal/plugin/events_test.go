package plugin_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestEventDelivery(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	h := startFiles(t, plugintest.Files{
		"a.lua": `
T.on("e", function(ev) T.spy("a", tostring(ev.buf), ev.pos.line .. ":" .. ev.pos.col, ev.range.to.col, ev.name, ev.n) end)
T.on("e", function() T.spy("a second") end)
T.on("only_b", function() T.spy("a must not see only_b") end)`,
		"b.lua": `
T.on("e", function(ev) T.spy("b", ev.name) end)
T.on("only_b", function() T.spy("b only") end)`,
	}, plugintest.Options{Host: host})

	data := map[string]any{
		"buf": pluginapi.BufferID(1), "pos": pluginapi.Pos{Line: 3, Col: 4},
		"range": pluginapi.Range{To: pluginapi.Pos{Col: 9}}, "name": "n", "n": 5,
	}
	h.Emit(pluginapi.Event{Name: "e", Data: data})
	// Dispatch order: plugin load order, then subscription order.
	want := "a Buffer(1) 3:4 9 n 5|a second|b n"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q", got, want)
	}

	plugin.ResetSpy(h.RT)
	h.Emit(pluginapi.Event{Name: "only_b", Target: "b"})
	h.Emit(pluginapi.Event{Name: "nobody"})
	if got := spyText(h); got != "b only" {
		t.Fatalf("targeted event: spy = %q", got)
	}
	if r := plugin.RegistryOf(h.RT); r.Events != 0 {
		t.Fatalf("events nobody subscribes to are dropped at once: %+v", r)
	}
}

func TestWantsTracksSubscriptions(t *testing.T) {
	h := start(t, `T.on("one", function() end)`)
	if !h.RT.Wants("one") || h.RT.Wants("two") {
		t.Fatal("Wants does not follow the subscriptions")
	}
	h.Unload("t")
	if h.RT.Wants("one") {
		t.Fatal("an unloaded plugin is still wanted")
	}
}

// TestWantsMemoFollowsChanges asks Wants before and after every kind of change, so a stale
// memo would show: a later subscription, an unsubscription, a plugin that comes and goes,
// and more distinct event names than the memo holds.
func TestWantsMemoFollowsChanges(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local sub = gvim.on("resize", function() end)
T.command("drop", function() sub:off() end)
T.command("add", function() gvim.on("resize", function() end) end)`)
	steps := []struct {
		name string
		do   func()
		want bool
	}{
		{"subscribed at load", func() {}, true},
		{"asked again", func() {}, true},
		{"unsubscribed", func() { _ = h.Run("t.drop", nil) }, false},
		{"subscribed again", func() { _ = h.Run("t.add", nil) }, true},
		{"many other names do not break the answer", func() {
			for i := range 2000 {
				h.RT.Wants(fmt.Sprintf("x:%d", i))
			}
		}, true},
		{"unloaded", func() { h.Unload("t") }, false},
	}
	for _, st := range steps {
		st.do()
		if got := h.RT.Wants("resize"); got != st.want {
			t.Fatalf("%s: Wants(resize) = %v, want %v", st.name, got, st.want)
		}
	}
}

func TestEventHandlersAreTasks(t *testing.T) {
	// A handler may await, and keeps its event budget class when it resumes.
	h := start(t, `require("gvim").on("resize", function() T.spy("before") T.work("w", 5) T.spy("after") end)`)
	h.Emit(pluginapi.Event{Name: "resize"})
	if got := spyText(h); got != "before|after" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	h2 := start(t, `require("gvim").on("resize", function() T.work("w", 5) while true do end end)`, plugintest.Options{Timeout: 400 * time.Millisecond})
	t0 := time.Now()
	h2.Emit(pluginapi.Event{Name: "resize"})
	if d := time.Since(t0); d > 300*time.Millisecond {
		t.Fatalf("a resumed event handler got a user budget: %v", d)
	}
	if !noticeHas(h2, "timed out (50 ms)") {
		t.Fatalf("notices = %v", h2.NoticeTexts())
	}
}

func TestVeto(t *testing.T) {
	rows := []struct {
		name    string
		handler string
		cancel  bool
		reason  string
		strikes int
	}{
		{"no handler returns nothing", `function() end`, false, "", 0},
		{"false cancels", `function() return false end`, true, "", 0},
		{"false with a reason", `function() return false, "read-only mount" end`, true, "read-only mount", 0},
		{"true does not cancel", `function() return true end`, false, "", 0},
		{"nil does not cancel", `function() return nil end`, false, "", 0},
		{"an error is ignored", `function() error("bug") end`, false, "", 0},
		{"a yield is refused", `function() T.echo(1) return false end`, false, "", 0},
		{"a slow handler is ignored with a strike", `function() T.busy(5) while true do end end`, false, "", 1},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, `require("gvim").on("before_file_save", `+r.handler+`)`)
			cancel, reason := h.RT.Veto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{"path": "/x"}})
			if cancel != r.cancel || reason != r.reason {
				t.Fatalf("Veto = %v %q, want %v %q (log %v)", cancel, reason, r.cancel, r.reason, h.LogText(""))
			}
			if n := plugin.Strikes(h.RT, "t"); n != r.strikes {
				t.Fatalf("strikes = %d, want %d", n, r.strikes)
			}
		})
	}
	t.Run("the first veto wins", func(t *testing.T) {
		h := startFiles(t, plugintest.Files{
			"a.lua": `require("gvim").on("before_file_save", function() T.spy("a") return false, "a says no" end)`,
			"b.lua": `require("gvim").on("before_file_save", function() T.spy("b") return false, "b says no" end)`,
		})
		cancel, reason := h.RT.Veto(pluginapi.Event{Name: "before_file_save"})
		if !cancel || reason != "a says no" || spyText(h) != "a" {
			t.Fatalf("cancel=%v reason=%q spy=%q", cancel, reason, spyText(h))
		}
	})
	t.Run("sees the payload", func(t *testing.T) {
		h := start(t, `require("gvim").on("before_file_save", function(ev) T.spy(ev.path) end)`)
		h.RT.Veto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{"path": "/some/file"}})
		if spyText(h) != "/some/file" {
			t.Fatalf("spy = %q", spyText(h))
		}
	})
}

func TestRuntimeVetoInSliceSkipsHandlers(t *testing.T) {
	h := start(t, `
require("gvim").on("before_file_save", function() T.spy("handler ran") return false end)
T.command("go", function()
  local cancel, reason = T.veto("before_file_save")
  T.spy("veto in slice", cancel, reason)
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "veto in slice false " {
		t.Fatalf("spy = %q: a veto asked inside a slice must not run Lua", got)
	}
	if !logHas(h, "", "veto skipped in slice") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	// at depth 0 it works
	if cancel, _ := h.RT.Veto(pluginapi.Event{Name: "before_file_save"}); !cancel {
		t.Fatal("the same veto outside a slice must run the handler")
	}
}

func TestSuspendedPluginIsMuted(t *testing.T) {
	h := start(t, `
require("gvim").on("resize", function() T.spy("ran") end)
require("gvim").on("before_file_save", function() return false end)
T.command("hang", function() while true do end end)`, plugintest.Options{Timeout: 15 * time.Millisecond})
	for i := 0; i < 3; i++ {
		_ = h.Run("t.hang", nil)
	}
	if status(h, "t") != "suspended" {
		t.Fatalf("status = %q", status(h, "t"))
	}
	h.Emit(pluginapi.Event{Name: "resize"})
	if cancel, _ := h.RT.Veto(pluginapi.Event{Name: "before_file_save"}); cancel {
		t.Fatal("a suspended plugin vetoed")
	}
	if spyText(h) != "" {
		t.Fatalf("a suspended plugin received an event: %q", spyText(h))
	}
	if got := h.Host.InputMode["t"]; got != "" {
		t.Fatalf("input mode of a suspended plugin = %q", got)
	}
}

func TestServicesNilIsUnsupported(t *testing.T) {
	svc := plugin.NoServices()
	if got := svc.Width("héllo"); got != 5 {
		t.Fatalf("Width falls back to the rune count: %d", got)
	}
	if got := svc.Truncate("abcdef", 4, "…"); got != "abc…" {
		t.Fatalf("Truncate = %q", got)
	}
	if _, err := svc.Grep(context.Background(), pluginapi.GrepRequest{}); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Grep: %v", err)
	}
	if _, err := svc.Decode("latin1", nil); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Decode: %v", err)
	}
	if _, err := svc.GitBranch("/r"); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("GitBranch: %v", err)
	}
	if _, ok := svc.GitRoot("/r"); ok {
		t.Fatal("GitRoot found a root")
	}
	if svc.Diff([]string{"a"}, []string{"b"}) != nil || svc.Fuzzy(context.Background(), "a", []string{"a"}, 5) != nil {
		t.Fatal("Diff and Fuzzy must answer nothing")
	}
}

func TestOptionsClampTimeout(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{
		0: plugin.DefaultTimeout, -5: plugin.DefaultTimeout, time.Nanosecond: time.Millisecond,
		time.Hour: plugin.MaxTimeout, 2 * time.Second: 2 * time.Second,
	} {
		if got := plugin.TimeoutOf(plugin.New(plugin.Options{Timeout: in})); got != want {
			t.Errorf("Timeout %v -> %v, want %v", in, got, want)
		}
	}
}

func TestBudgetClasses(t *testing.T) {
	r := plugin.New(plugin.Options{Timeout: 500 * time.Millisecond})
	got := plugin.Budgets(r)
	want := map[string]time.Duration{"load": 2 * time.Second, "command": 500 * time.Millisecond, "callback": 500 * time.Millisecond, "event": 50 * time.Millisecond, "timer": 50 * time.Millisecond}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("budget[%s] = %v, want %v", k, got[k], w)
		}
	}
	// the load budget is capped at 5 s, the event budget follows a smaller plugin_timeout_ms
	big := plugin.Budgets(plugin.New(plugin.Options{Timeout: 60 * time.Second}))
	if big["load"] != 5*time.Second {
		t.Errorf("load budget with 60 s = %v", big["load"])
	}
	small := plugin.Budgets(plugin.New(plugin.Options{Timeout: 20 * time.Millisecond}))
	if small["event"] != 20*time.Millisecond {
		t.Errorf("event budget with 20 ms = %v", small["event"])
	}
	// Options.EventBudget raises the cap (tests that run real Lua on a busy machine); the
	// slice still gets no more than plugin_timeout_ms, and the other classes do not change.
	for _, row := range []struct {
		name            string
		timeout, budget time.Duration
		want            time.Duration
	}{
		{"cap raised", 5 * time.Second, 5 * time.Second, 5 * time.Second},
		{"timeout still wins", 200 * time.Millisecond, 5 * time.Second, 200 * time.Millisecond},
		{"zero is the spec's 50 ms", 5 * time.Second, 0, 50 * time.Millisecond},
	} {
		b := plugin.Budgets(plugin.New(plugin.Options{Timeout: row.timeout, EventBudget: row.budget}))
		if b["event"] != row.want || b["timer"] != row.want {
			t.Errorf("%s: event %v, timer %v, want %v", row.name, b["event"], b["timer"], row.want)
		}
		if b["command"] != row.timeout {
			t.Errorf("%s: the command budget = %v, want %v", row.name, b["command"], row.timeout)
		}
	}
}

func TestGoroutineCheck(t *testing.T) {
	h := start(t, ``)
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		h.RT.Emit(pluginapi.Event{Name: "x"})
	}()
	if rec := <-done; rec == nil {
		t.Fatal("a call from another goroutine must panic with checking on")
	}
}

// buffer_closed delivers the old id as a plain number whatever integer type the host used,
// and drops the handle from every cache.
func TestBufferClosedIDIsNumberAndPurgesCache(t *testing.T) {
	rows := []struct {
		name string
		id   any
	}{
		{"BufferID", pluginapi.BufferID(1)},
		{"int", 1},
		{"int64", int64(1)},
		{"uint64", uint64(1)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			host := plugintest.NewFakeHost()
			host.AddBuffer("f", "x")
			h := startFiles(t, plugintest.Files{"t.lua": `
T.command("make", function() T.buf(1) end)
require("gvim").on("buffer_closed", function(e) T.spy(type(e.id), e.id, e.path) end)`}, plugintest.Options{Host: host})
			if err := h.Run("t.make", nil); err != nil {
				t.Fatal(err)
			}
			if n := plugin.HandleCacheSize(h.RT, "t"); n != 1 {
				t.Fatalf("cache = %d before close", n)
			}
			h.Emit(pluginapi.Event{Name: "buffer_closed", Data: map[string]any{"id": row.id, "path": "f"}})
			if got := spyText(h); got != "number 1 f" {
				t.Fatalf("handler saw %q", got)
			}
			if n := plugin.HandleCacheSize(h.RT, "t"); n != 0 {
				t.Fatalf("cache = %d after close, want 0", n)
			}
		})
	}
}
