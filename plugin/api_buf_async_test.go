package plugin_test

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// doHost is a FakeHost whose Do records the action and creates an untitled buffer for "new".
type doHost struct {
	*plugintest.FakeHost
	actions []string
}

func (d *doHost) Do(action string, args map[string]any, count int) error {
	d.actions = append(d.actions, action)
	if action != "new" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown action %q", action)
	}
	d.SetActive(d.AddBuffer("", ""))
	return nil
}

func TestBufNew(t *testing.T) {
	fake := plugintest.NewFakeHost()
	fake.AddBuffer("a.go", "x")
	host := &doHost{FakeHost: fake}
	h := plugintest.New(t, nil, plugintest.Options{Timeout: 100 * time.Millisecond})
	h.RT.Start(host)
	if err := h.Load("t", bufShow+`
local gvim = require "gvim"
T.command("go", function()
  local b = gvim.buf.new()
  T.spy(tostring(b), b:path() == nil, b:is_modified(), tostring(gvim.buf.current() == b))
end)`); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "Buffer(2) true false true" || !slices.Equal(host.actions, []string{"new"}) {
		t.Fatalf("spy = %q actions = %v log %v", got, host.actions, h.LogText(""))
	}
}

func TestBufNewUnsupportedHost(t *testing.T) {
	// The FakeHost implements Do; a host that refuses the "new" action stands for one without it.
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	host.Cmd().Errors["new"] = pluginapi.Errf(pluginapi.CodeUnsupported, "no new action")
	h, _ := bufRun(t, "x", `local b, e = gvim.buf.new() T.spy(tostring(b), gvim.err_code(e))`, plugintest.Options{Host: host})
	if got := spyText(h); got != "nil unsupported" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

// settleBuffers completes the fake host's pending async buffer calls until none is left.
func settleBuffers(h *plugintest.Harness, host *plugintest.FakeHost) {
	for i := 0; i < 20 && host.PendingBufferCalls() > 0; i++ {
		h.CompleteBuffers()
	}
}

func TestBufOpenSaveRevert(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "old")
	host.SetFile("/w/new.go", "new text")
	host.SetFile("/w/other.go", "other")
	h := cmd(t, bufShow, `
local a = gvim.buf.current()
local n, e = gvim.buf.open("/w/new.go", {pos = gvim.pos(0, 3), focus = false, preview = true, background = true})
T.spy(tostring(n), tostring(e), n:text(), tostring(gvim.buf.current()), show(n:cursor()))
local again = gvim.buf.open("/w/a.go")
T.spy(again == a, tostring(gvim.buf.current()))
local o = gvim.buf.open("/w/other.go")
T.spy(tostring(gvim.buf.current() == o))
local m, me = gvim.buf.open("/w/missing.go")
T.spy(tostring(m), tostring(me))
a:insert(gvim.pos(0, 0), "mod ")
T.spy(a:is_modified(), tostring(a:save()), a:is_modified())
T.spy(a:insert(gvim.pos(0, 0), "again "), a:is_modified(), tostring(a:revert()), a:is_modified(), a:text())
T.spy(tostring(a:save("/w/renamed.go")), a:path(), a:name())`, plugintest.Options{Host: host})
	if got := spy(h); len(got) != 0 {
		t.Fatalf("the awaits returned before the host completed them: %q", got)
	}
	settleBuffers(h, host)
	want := []string{
		"Buffer(2) nil new text Buffer(1) {col=3,line=0}",
		"true Buffer(1)",
		"true",
		"nil not_found: /w/missing.go",
		"true true false",
		"true true true false mod old",
		"true /w/renamed.go renamed.go",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
	var open pluginapi.OpenRequest
	for _, c := range host.Calls() {
		if c.Method == "Open" {
			open = c.Args[1].(pluginapi.OpenRequest)
			break
		}
	}
	if open.Path != "/w/new.go" || open.Focus || !open.Preview || !open.Background || open.Pos == nil || *open.Pos != (pluginapi.Pos{Col: 3}) {
		t.Fatalf("OpenRequest = %+v", open)
	}
}

func TestBufOpenDefaults(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.SetFile("/w/x.go", "x")
	h := cmd(t, "", `gvim.buf.open("/w/x.go")`, plugintest.Options{Host: host})
	settleBuffers(h, host)
	for _, c := range host.Calls() {
		if c.Method == "Open" {
			r := c.Args[1].(pluginapi.OpenRequest)
			if r != (pluginapi.OpenRequest{Path: "/w/x.go", Focus: true}) {
				t.Fatalf("OpenRequest = %+v", r)
			}
			return
		}
	}
	t.Fatal("Host.Open was not called")
}

func TestBufAsyncErrors(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "a")
	host.AddBuffer("", "untitled")
	host.SetFile("/w/x.go", "x")
	host.FailBuf("open:/w/x.go", "io: disk on fire")
	host.FailBuf("save", "io: read-only file system")
	h := cmd(t, "", `
local a, u = gvim.buf.get(1), gvim.buf.get(2)
local function code(v, e) return tostring(v) .. " " .. tostring(e) end
T.spy(code(gvim.buf.open("")))
T.spy(code(gvim.buf.open("/w/x.go")))
T.spy(code(a:save()))
T.spy(code(u:save()))
T.spy(code(u:revert()))
T.spy(code(a:save()))
T.spy((pcall(gvim.buf.open, 5)))
T.spy((pcall(gvim.buf.open, "/w/a.go", 5)))
T.spy((pcall(gvim.buf.open, "/w/a.go", {focus = "yes"})))
T.spy((pcall(gvim.buf.open, "/w/a.go", {pos = 5})))
T.spy((pcall(a.save, a, 5)))
T.spy((pcall(a.save, 5)))
T.spy((pcall(a.revert, u.id)))`, plugintest.Options{Host: host})
	settleBuffers(h, host)
	want := []string{
		"nil invalid: empty path",
		"nil io: disk on fire",
		"nil io: read-only file system",
		"nil invalid: the buffer has no path",
		"nil invalid: buffer 2 has no file to revert to",
		"true nil",
		"false", "false", "false", "false", "false", "false", "false",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestBufAsyncOnClosedBuffer(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "a")
	host.AddBuffer("/w/b.go", "b")
	h := cmd(t, bufShow+`
local b2
T.command("prep", function() b2 = gvim.buf.get(2) end)
T.command("after", function()
  local ok, e = b2:save()
  T.spy(tostring(ok), gvim.err_code(e))
  local ok2, e2 = b2:revert()
  T.spy(tostring(ok2), gvim.err_code(e2))
end)`, ``, plugintest.Options{Host: host})
	if err := h.Run("t.prep", nil); err != nil {
		t.Fatal(err)
	}
	host.RemoveBuffer(2)
	if err := h.Run("t.after", nil); err != nil {
		t.Fatal(err)
	}
	if got := spy(h); !slices.Equal(got, []string{"nil closed", "nil closed"}) {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestBufAwaitOutsideTask(t *testing.T) {
	// main chunk is a task, but the state's main thread is not: open from a plain callback
	// registered with T.callback still runs as a task, so only the wrapper's own rule is
	// checked here: an await in a coroutine of a task works.
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "a")
	host.SetFile("/w/n.go", "n")
	h := cmd(t, "", `
local co = coroutine.wrap(function() local b = gvim.buf.open("/w/n.go") return tostring(b) end)
T.spy(co())`, plugintest.Options{Host: host})
	settleBuffers(h, host)
	if got := spyText(h); got != "Buffer(2)" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestBufDiffSaved(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "one\ntwo\nthree\nfour")
	host.AddBuffer("", "untitled")
	h := cmd(t, bufShow, `
local b, u = gvim.buf.get(1), gvim.buf.get(2)
local clean = b:diff_saved()
T.spy(#clean)
b:set_lines(1, 2, {"TWO", "extra"})
b:set_lines(4, 5, {})
local hunks, e = b:diff_saved()
T.spy(tostring(e), #hunks)
for _, hk in ipairs(hunks) do T.spy(hk.kind, hk.old_start, hk.old_count, hk.new_start, hk.new_count) end
local v, ue = u:diff_saved()
T.spy(tostring(v), gvim.err_code(ue))
T.spy((pcall(b.diff_saved, 5)))`, plugintest.Options{Host: host})
	want := []string{"0", "nil 2", "change 1 1 1 2", "delete 3 1 4 0", "nil not_found", "false"}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestBufDiffSavedWithoutService(t *testing.T) {
	// A runtime with no Services refuses diff_saved instead of reporting "no changes".
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "a")
	rt := plugin.New(plugin.Options{CheckGoroutine: true})
	t.Cleanup(rt.Stop)
	rt.Start(host)
	compiled := plugin.Compile("t", map[string]string{"init.lua": `T.command("go", function()
  local v, e = require("gvim").buf.current():diff_saved()
  T.spy(tostring(v), require("gvim").err_code(e))
end)`})
	if err := rt.LoadPlugin(plugin.Source{Name: "t", Kind: plugin.SourceUser, Compiled: compiled}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	rt.Drain(time.Second)
	if got := plugin.Spy(rt); !slices.Equal(got, []string{"nil unsupported"}) {
		t.Fatalf("spy = %q", got)
	}
}

func TestBufDiffSavedRunsInWork(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "a\nb")
	svc := &stackDiff{}
	h := plugintest.New(t, plugintest.Files{"t.lua": `
local gvim = require "gvim"
T.command("go", function()
  gvim.buf.current():set_lines(0, 1, {"A"})
  local hunks = gvim.buf.current():diff_saved()
  T.spy("done", #hunks)
end)`}, plugintest.Options{Host: host, Services: svc})
	h.Start()
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "done 1" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
	if !svc.called {
		t.Fatal("Diff never ran")
	}
	if svc.inLua {
		t.Fatal("Diff ran inside the Lua slice: it must run in Work")
	}
	if !slices.Equal(svc.a, []string{"a", "b"}) || !slices.Equal(svc.b, []string{"A", "b"}) {
		t.Fatalf("Diff(%q, %q): sides are the saved text and the current text", svc.a, svc.b)
	}
}

// stackDiff is a Services that notes whether its Diff was called with Lua on the stack.
type stackDiff struct {
	plugintest.FakeServices
	called, inLua bool
	a, b          []string
}

func (d *stackDiff) Diff(a, b []string) []pluginapi.Hunk {
	d.called, d.a, d.b = true, a, b
	buf := make([]byte, 1<<16)
	d.inLua = strings.Contains(string(buf[:runtime.Stack(buf, false)]), "gopher-lua")
	return []pluginapi.Hunk{{Kind: "change", OldStart: 0, OldCount: 1, NewStart: 0, NewCount: 1}}
}

func TestBufDiffSavedTooLarge(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/big.txt", strings.Repeat("x\n", 200_001))
	h := cmd(t, "", `
local v, e = gvim.buf.current():diff_saved()
T.spy(tostring(v), gvim.err_code(e))`, plugintest.Options{Host: host, Timeout: 5 * time.Second})
	if got := spyText(h); got != "nil too_large" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestRevertInvalidatesVersionTokens(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "old")
	h := cmd(t, "", `
local b = gvim.buf.current()
local tok0 = b:version()
b:insert(gvim.pos(0, 0), "mod ")
local tok1 = b:version()
T.spy(tostring(b:revert()), b:text())
local v = b:version()
T.spy(v ~= tok0, v ~= tok1)
for _, tok in ipairs({tok0, tok1}) do
  local ok, e = b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "Z"}}, {version = tok})
  T.spy(tostring(ok), gvim.err_code(e), b:text())
end
T.spy(tostring(b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "Z"}}, {version = v})), b:text())`, plugintest.Options{Host: host})
	settleBuffers(h, host)
	want := []string{"true old", "true true", "nil stale old", "nil stale old", "true Zld"}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}
