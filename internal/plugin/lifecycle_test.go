package plugin_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

const leaky = `
local gvim = require "gvim"
T.register_all()
T.on("x", function() end)
T.on_unload(function() T.spy("on_unload ran") end)
gvim.plugin.on_unload(function() T.spy("second on_unload ran") end)
T.command("park", function() T.host() T.spy("park resumed") end)
T.command("susp", function() gvim.async.suspend(function() end) T.spy("susp resumed") end)
T.command("spawn", function() gvim.async.spawn(function() T.host() T.spy("spawned resumed") end) end)
T.command("xcall", function() T.xcall("other") T.spy("xcall resumed") end)
`

func TestUnloadLeavesNothingBehind(t *testing.T) {
	h := startFiles(t, plugintest.Files{"base.lua": `T.spy("base loaded")`})
	baseRes := h.Host.Resources()
	baseReg := plugin.RegistryOf(h.RT)

	if err := h.Load("leaky", leaky); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"park", "park", "susp", "spawn", "xcall"} {
		if err := h.Run("leaky."+c, nil); err != nil {
			t.Fatal(err)
		}
	}
	res := h.Host.Resources()
	for _, want := range []string{"command:leaky/leaky.reg", "binding:leaky/ctrl+k", "mode:leaky/normal", "context:leaky/active", "status:leaky/s", "input_mode:leaky/normal"} {
		if !contains(res, want) {
			t.Fatalf("resource %q missing from %v", want, res)
		}
	}
	reg := plugin.RegistryOf(h.RT)
	if reg.Parked != 5 || reg.Waits != 5 || reg.Subs != 1 || reg.Commands != 5 {
		t.Fatalf("registry while loaded = %+v", reg)
	}
	tokens := plugin.ParkedTokens(h.RT)

	h.Unload("leaky")

	if got := h.Host.Resources(); !reflect.DeepEqual(got, baseRes) {
		t.Fatalf("host resources after unload = %v, before load = %v", got, baseRes)
	}
	if got := plugin.RegistryOf(h.RT); got != baseReg {
		t.Fatalf("registry after unload = %+v, before load = %+v", got, baseReg)
	}
	if _, ok := h.Plugin("leaky"); ok {
		t.Fatal("the plugin is still listed")
	}
	sp := spy(h)
	for _, want := range []string{"on_unload ran", "second on_unload ran", "cleanup:leaky"} {
		if !contains(sp, want) {
			t.Fatalf("%q missing from spy %v", want, sp)
		}
	}
	cancelled := 0
	for _, l := range sp {
		if strings.HasPrefix(l, "cancelled:") {
			cancelled++
		}
	}
	// park (twice) and the spawned task wait on a host token: unload abandons all three
	// (Host.CancelWait in the real ui); suspend and xcall have no host side.
	if cancelled != 3 {
		t.Fatalf("cancel hooks ran %d times: %v", cancelled, sp)
	}
	if n := h.Host.CallCount("OwnerGone"); n == 0 || h.Host.Gone[len(h.Host.Gone)-1].Plugin != "leaky" {
		t.Fatalf("OwnerGone calls = %v", h.Host.Gone)
	}
	// A completion that arrives late for the old generation is ignored.
	before := len(spy(h))
	for _, tok := range tokens {
		h.Resolve(pluginapi.Completion{Token: tok, Values: []any{"late"}})
	}
	if after := spy(h); len(after) != before {
		t.Fatalf("a late completion woke a dead task: %v", after[before:])
	}
	// The name is free again, and the new generation is a different one.
	if err := h.Load("leaky", `T.spy("reloaded")`); err != nil {
		t.Fatal(err)
	}
	h.Unload("leaky")
	gens := map[uint64]bool{}
	for _, g := range h.Host.Gone {
		if g.Plugin == "leaky" {
			if gens[g.Gen] {
				t.Fatalf("generation %d reused", g.Gen)
			}
			gens[g.Gen] = true
		}
	}
	if len(gens) != 2 {
		t.Fatalf("OwnerGone generations of leaky = %v", gens)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestUnloadCancelsWorkAndIgnoresItsCompletion(t *testing.T) {
	h := start(t, `T.command("go", function() T.spy("got", T.work("v", 60000)) end)`)
	if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	work, _ := h.RT.Drain(time.Second)
	if len(work) != 1 || work[0].Run == nil {
		t.Fatalf("work = %+v", work)
	}
	w := work[0]
	if w.Plugin != "t" || w.Gen == 0 || w.Ctx == nil || w.Token == 0 || w.Ctx.Err() != nil {
		t.Fatalf("work = %+v", w)
	}
	h.RT.UnloadPlugin("t")
	if w.Ctx.Err() == nil {
		t.Fatal("unload did not cancel the context of in-flight Work")
	}
	// Whatever the Work returns now is dropped.
	c := w.Run(w.Ctx)
	if c.Token != w.Token {
		t.Fatalf("the runtime must stamp its token on the completion: %+v", c)
	}
	h.Resolve(c)
	if got := spyText(h); got != "" {
		t.Fatalf("spy = %q", got)
	}
}

func TestFailedMainChunkRollsBack(t *testing.T) {
	h := startFiles(t, plugintest.Files{"base.lua": ``})
	baseRes := h.Host.Resources()
	baseReg := plugin.RegistryOf(h.RT)
	err := h.Load("bad", `
T.register_all()
T.on("x", function() end)
T.on_unload(function() T.spy("bad on_unload") end)
error("late failure")`)
	if err != nil {
		t.Fatal(err) // started fine; it fails while running
	}
	p, ok := h.Plugin("bad")
	if !ok || p.Status != "failed" || !strings.Contains(p.Err, "late failure") {
		t.Fatalf("plugin = %+v ok=%v", p, ok)
	}
	if got := h.Host.Resources(); !reflect.DeepEqual(got, baseRes) {
		t.Fatalf("registrations not rolled back: %v", got)
	}
	reg := plugin.RegistryOf(h.RT)
	baseReg.Plugins++ // the failed row stays for the manager
	if reg != baseReg {
		t.Fatalf("registry = %+v, want %+v", reg, baseReg)
	}
	if h.RT.Wants("x") {
		t.Fatal("the subscription of a failed plugin is still live")
	}
	// A failed row is replaced by a new load of the same name.
	if err := h.Load("bad", `T.spy("fixed")`); err != nil {
		t.Fatal(err)
	}
	if status(h, "bad") != "loaded" {
		t.Fatalf("status = %q", status(h, "bad"))
	}
}

func TestLoadPluginValidation(t *testing.T) {
	h := startFiles(t, plugintest.Files{"taken.lua": ``})
	for _, name := range []string{"", "Bad", "1x", "a-b", "a b", strings.Repeat("x", 41), "ünï"} {
		if err := h.Load(name, ``); err == nil {
			t.Errorf("name %q was accepted", name)
		}
	}
	err := h.Load("taken", ``)
	if pluginapi.CodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("loading a loaded name: %v", err)
	}
	if err := h.Load(strings.Repeat("x", 40), ``); err != nil {
		t.Fatalf("a 40 character name is allowed: %v", err)
	}
	if err := h.Load("init", ``); err != nil {
		t.Fatalf("init is a plugin name: %v", err)
	}
}

func TestParkedProxyCallResolvedOnUnload(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"a.lua": `T.command("call", function() local v, e = T.xcall("b") T.spy("a got", tostring(v), e) end)`,
		"b.lua": `T.spy("b loaded")`,
	})
	if err := h.Run("a.call", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "b loaded" {
		t.Fatalf("spy = %q", got)
	}
	h.Unload("b")
	if got := spyText(h); got != "b loaded|a got nil closed: plugin reloaded" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	// the caller is fine
	if status(h, "a") != "loaded" {
		t.Fatalf("status = %q", status(h, "a"))
	}
}

func TestReloadAllFromOwnCommandClosesStateAfterSlice(t *testing.T) {
	h := start(t, `T.command("go", function()
  local w, n = T.control("disable", "t")   -- asks to unload this very plugin, from inside its own command
  T.spy("queued", w, n, T.in_slice())
  T.spy("statement after the control call still ran")
end)`)
	closeDepth, closed := -1, 0
	plugin.SetCloseHook(h.RT, func(depth int, name string) {
		if name == "t" {
			closeDepth = depth
			closed++
		}
	})
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "queued 0 0 true|statement after the control call still ran" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if closed != 1 || closeDepth != 0 {
		t.Fatalf("state closed %d times, at depth %d (must be once, outside any slice)", closed, closeDepth)
	}
	if p, _ := h.Plugin("t"); p.Status != "disabled" {
		t.Fatalf("plugin = %+v", p)
	}
}

func TestRuntimeInSliceQueuesRunControlResolve(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"x.lua": ``,
		"t.lua": `
T.command("wait", function() T.host() T.spy("resumed") end)
T.command("b", function() T.spy("b ran") end)
T.command("a", function()
  T.spy("a1", T.in_slice())
  T.spy("run", T.run("t.b"))                       -- queued FIFO: does not run now
  T.spy("control", T.control("disable", "x"))      -- queued: nothing happens now
  T.resolve(T.last_token())                        -- queued: the parked task does not wake now
  T.spy("a2")
end)`,
	})
	if err := h.Run("t.wait", nil); err != nil {
		t.Fatal(err)
	}
	plugin.ResetSpy(h.RT)
	if err := h.Run("t.a", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"a1 true", "run true", "control 0 0", "a2", "resumed", "b ran"}
	if got := spy(h); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("spy = %v, want %v", got, want)
	}
	if p, _ := h.Plugin("x"); p.Status != "disabled" {
		t.Fatalf("the queued control did not run afterwards: %+v", p)
	}
}

func TestRunFirstSliceSynchronous(t *testing.T) {
	h := start(t, `T.command("go", function() T.spy("ran") end)`)
	// Direct call, no Drain: the first slice has run when Run returns.
	if err := h.RT.Run("t.go", nil, pluginapi.SrcKey); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "ran" {
		t.Fatalf("spy = %q: Run must execute the first slice before it returns", got)
	}
}

func TestRunAndOwns(t *testing.T) {
	h := start(t, `T.command("go", function() end)`)
	for id, want := range map[string]bool{"t.go": true, "t.nope": false, "u.go": false, "nodot": false, "": false, "t.": false, "go": false} {
		if got := h.RT.Owns(id); got != want {
			t.Errorf("Owns(%q) = %v", id, got)
		}
	}
	err := h.RT.Run("t.nope", nil, pluginapi.SrcAPI)
	if pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("Run of an unknown command: %v", err)
	}
	h.Unload("t")
	if h.RT.Owns("t.go") {
		t.Fatal("an unloaded plugin still owns its commands")
	}
}

func TestCommandContext(t *testing.T) {
	h := start(t, `T.command("go", function(ctx)
  T.spy(type(ctx.args), ctx.count, ctx.source, ctx.args.name, tostring(ctx.buf), T.to_host(ctx.args))
end)`)
	host := h.Host
	host.AddBuffer("a.go", "x")
	if err := h.RT.Run("t.go", map[string]any{"name": "n", "count": int64(3)}, pluginapi.SrcPalette); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != `table 3 palette n Buffer(1) {count=i3,name="n"}` {
		t.Fatalf("spy = %q", got)
	}
	plugin.ResetSpy(h.RT)
	if err := h.RT.Run("t.go", nil, pluginapi.SrcKey); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); !strings.HasPrefix(got, "table 1 key") {
		t.Fatalf("spy = %q", got)
	}
}

func TestPluginsInfo(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"a.lua": `T.command("one", function() end) T.command("two", function() end) T.on("e1", function() end) T.on("e2", function() end)`,
		"b.lua": `error("fails")`,
	})
	infos := h.RT.Plugins()
	if len(infos) != 2 || infos[0].Name != "a" || infos[1].Name != "b" {
		t.Fatalf("plugins = %+v", infos)
	}
	a, b := infos[0], infos[1]
	if a.Status != "loaded" || a.Commands != 2 || a.Subscriptions != 2 || a.Source != "user" || !a.Enabled || a.Err != "" {
		t.Fatalf("a = %+v", a)
	}
	if b.Status != "failed" || !strings.Contains(b.Err, "fails") || b.Commands != 0 {
		t.Fatalf("b = %+v", b)
	}
	h.Control(pluginapi.Control{Op: "disable", Name: "a"})
	if a, _ := h.Plugin("a"); a.Status != "disabled" || a.Enabled {
		t.Fatalf("a after disable = %+v", a)
	}
}

func TestControlStub(t *testing.T) {
	h := start(t, ``)
	notices := h.Control(pluginapi.Control{Op: "reload", Name: "t"})
	if len(notices) != 1 || !notices[0].Warn {
		t.Fatalf("notices = %+v", notices)
	}
	notices = h.Control(pluginapi.Control{Op: "disable", Name: "ghost"})
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "not loaded") {
		t.Fatalf("notices = %+v", notices)
	}
}

func TestStopRunsShutdownHandlersAndClosesStates(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"a.lua": `T.on("shutdown", function() while true do end end)`,
		"b.lua": `T.on("shutdown", function() T.spy("b shutdown") end)`,
		"c.lua": `T.on("shutdown", function() error("c fails") end)`,
	}, plugintest.Options{Timeout: 400 * time.Millisecond})
	closed := 0
	plugin.SetCloseHook(h.RT, func(int, string) { closed++ })
	t0 := time.Now()
	h.RT.Stop()
	if d := time.Since(t0); d > 400*time.Millisecond {
		t.Fatalf("Stop took %v: shutdown handlers share 200 ms", d)
	}
	if got := spyText(h); got != "b shutdown" {
		t.Fatalf("spy = %q", got)
	}
	if closed != 3 || len(h.RT.Plugins()) != 0 {
		t.Fatalf("closed %d states, %d plugins left", closed, len(h.RT.Plugins()))
	}
	h.RT.Stop() // idempotent
	if closed != 3 {
		t.Fatal("second Stop closed states again")
	}
	if err := h.Load("late", ``); err == nil {
		t.Fatal("a stopped runtime accepted a plugin")
	}
}

func TestStopHooksRunBeforeStatesClose(t *testing.T) {
	h := start(t, `T.on("shutdown", function() T.spy("shutdown") end)`)
	order := []string{}
	plugin.SetCloseHook(h.RT, func(int, string) { order = append(order, "close") })
	plugin.OnStop(h.RT, func() { order = append(order, "hook:"+strings.Join(spy(h), ",")) })
	h.RT.Stop()
	if strings.Join(order, " ") != "hook:shutdown close" {
		t.Fatalf("order = %v", order)
	}
}

func TestCommandRegistrySyncsTheHost(t *testing.T) {
	h := start(t, `
local cmd = require("gvim").command
cmd.register{ name = "one", run = function() end }
cmd.register{ name = "two", run = function() end }
cmd.register{ name = "go", run = function() cmd.unregister("one") end }`)
	ids := func() []string {
		var out []string
		for _, c := range h.Host.Commands["t"] {
			out = append(out, c.ID)
		}
		return out
	}
	if got := ids(); !reflect.DeepEqual(got, []string{"t.go", "t.one", "t.two"}) {
		t.Fatalf("host commands = %v", got)
	}
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !reflect.DeepEqual(got, []string{"t.go", "t.two"}) {
		t.Fatalf("host commands after unregister = %v", got)
	}
	if h.RT.Owns("t.one") {
		t.Fatal("an unregistered command is still owned")
	}
}

func TestCallbacksUseTheirBudgetClass(t *testing.T) {
	// A timer-class callback gets min(50 ms, timeout); a user-interaction callback the full timeout.
	h := start(t, `
T.command("timer", function() T.callback("timer", function() T.busy(120) T.spy("timer done") end) end)
T.command("user", function() T.callback("callback", function() T.busy(120) T.spy("user done") end) end)`, plugintest.Options{Timeout: 400 * time.Millisecond})
	_ = h.Run("t.timer", nil)
	if got := spyText(h); got != "" {
		t.Fatalf("a timer-class callback exceeded its budget: spy=%q", got)
	}
	h.Settle()
	_ = h.Run("t.user", nil)
	if got := spyText(h); got != "user done" {
		t.Fatalf("a user-class callback must get the full budget: spy=%q log=%v", got, h.LogText(""))
	}
}

func TestRuntimeLevelWaitsAndStreams(t *testing.T) {
	h := start(t, ``)
	var got []string
	plugin.ExpectRuntime(h.RT, 900, func(c pluginapi.Completion) {
		got = append(got, c.Kind+":"+map[bool]string{true: "more", false: "last"}[c.More])
	})
	h.Resolve(pluginapi.Completion{Token: 900, Kind: "a", More: true})
	h.Resolve(pluginapi.Completion{Token: 900, Kind: "b", More: true})
	h.Resolve(pluginapi.Completion{Token: 900, Kind: "c"})
	h.Resolve(pluginapi.Completion{Token: 900, Kind: "after the last is ignored"})
	if want := []string{"a:more", "b:more", "c:last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if r := plugin.RegistryOf(h.RT); r.Waits != 0 {
		t.Fatalf("registry = %+v", r)
	}
}

func TestUnloadHooksShareOneBudget(t *testing.T) {
	// Documented: on_unload gets 200 ms in all, not 200 ms per registered function.
	h := startFiles(t, plugintest.Files{"a.lua": `
local gvim = require "gvim"
for i = 1, 10 do gvim.plugin.on_unload(function() while true do end end) end`}, plugintest.Options{Timeout: 10 * time.Second})
	start := time.Now()
	h.Unload("a")
	if took := time.Since(start); took > 600*time.Millisecond {
		t.Fatalf("unload took %v with ten runaway hooks, want about 200 ms", took)
	}
}

// TestUnloadAndShutdownHooksCannotWait backs the docs: every gvim.fs call waits, so an
// on_unload hook or a shutdown handler cannot write or delete a file.
func TestUnloadAndShutdownHooksCannotWait(t *testing.T) {
	h := startFiles(t, plugintest.Files{"a.lua": `
local gvim = require "gvim"
gvim.plugin.on_unload(function() gvim.fs.write("/tmp/gvim-never", "x") end)`})
	h.Unload("a")
	if !logHas(h, "a", "on_unload must not yield") {
		t.Fatalf("log %v", h.LogText("a"))
	}
}
