package plugin_test

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// startOS starts the plugin "t" with the given main chunk on a harness around fos.
func startOS(t *testing.T, fos *plugintest.FakeOS, main string) *plugintest.Harness {
	t.Helper()
	return start(t, `local gvim = require "gvim"
`+main, plugintest.Options{OS: fos})
}

// sameLines compares two "|"-joined spy texts ignoring the order of the lines: callbacks
// and resumed tasks run in different classes, and their relative order is not a contract.
func sameLines(got, want string) bool {
	a, b := strings.Split(got, "|"), strings.Split(want, "|")
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "|") == strings.Join(b, "|")
}

// drainAll drains the runtime like the ui pump would and returns the Work, without running it.
func drainAll(rt *plugin.Runtime) []pluginapi.Work {
	var out []pluginapi.Work
	for range 100 {
		work, more := rt.Drain(100 * time.Millisecond)
		out = append(out, work...)
		if !more && len(work) == 0 {
			break
		}
	}
	return out
}

func TestProcRun(t *testing.T) {
	rows := []struct {
		name   string
		script plugintest.ProcScript
		body   string
		want   string
	}{
		{"result fields", plugintest.ProcScript{Stdout: "out", Stderr: "err"},
			`local r = gvim.proc.run({"tool", "x"}) T.spy(r.code, r.ok, r.stdout, r.stderr, r.timed_out, r.truncated, tostring(r.signal))`, "0 true out err false false nil"},
		{"a non-zero exit is a result", plugintest.ProcScript{Code: 2},
			`local r, e = gvim.proc.run({"tool"}) T.spy(r.code, r.ok, tostring(e))`, "2 false nil"},
		{"a signal", plugintest.ProcScript{Code: -1, Signal: "KILL"},
			`local r = gvim.proc.run({"tool"}) T.spy(r.code, r.ok, r.signal)`, "-1 false KILL"},
		{"timed out and truncated", plugintest.ProcScript{Code: -1, TimedOut: true, Truncated: true},
			`local r = gvim.proc.run({"tool"}) T.spy(r.ok, r.timed_out, r.truncated)`, "false true true"},
		{"could not start", plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "no such program")},
			`local r, e = gvim.proc.run({"tool"}) T.spy(tostring(r), e)`, "nil exec: no such program"},
		{"unknown program", plugintest.ProcScript{},
			`local r, e = gvim.proc.run({"other"}) T.spy(tostring(r), gvim.err_code(e))`, "nil exec"},
		{"argv must be a table", plugintest.ProcScript{},
			`local ok, e = pcall(gvim.proc.run, "tool x") T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #1 to 'run' (table (argv) expected)"},
		{"argv must name a program", plugintest.ProcScript{},
			`local ok, e = pcall(gvim.proc.run, {}) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #1 to 'run' (argv must name a program)"},
		{"argv of strings", plugintest.ProcScript{},
			`local ok, e = pcall(gvim.proc.run, {"tool", {}}) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #1 to 'run' (argv must be a list of strings)"},
		{"argv without NUL", plugintest.ProcScript{},
			`local ok, e = pcall(gvim.proc.run, {"to\0ol"}) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #1 to 'run' (argv contains a NUL byte)"},
		{"env names", plugintest.ProcScript{},
			`local ok, e = pcall(gvim.proc.run, {"tool"}, {env = {["A=B"] = "x"}}) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #2 to 'run' (env has an invalid name or value)"},
		{"stdin of run is a string", plugintest.ProcScript{},
			`local ok, e = pcall(gvim.proc.run, {"tool"}, {stdin = true}) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #2 to 'run' (option stdin must be a string)"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fos := plugintest.NewFakeOS()
			fos.Script([]string{"tool"}, r.script)
			h := osCmd(t, fos, "", r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestProcRunOptionsReachTheOS(t *testing.T) {
	rows := []struct {
		name string
		opts string
		want plugin.ProcOptions
	}{
		{"defaults", ``, plugin.ProcOptions{Timeout: 60 * time.Second, MaxOutput: 8 << 20}},
		{"all of them", `{cwd = "/x", env = {A = "1"}, stdin = "in", timeout_ms = 1500, max_output = 100, merge_stderr = true}`,
			plugin.ProcOptions{Cwd: "/x", Env: map[string]string{"A": "1"}, Stdin: []byte("in"), Timeout: 1500 * time.Millisecond, MaxOutput: 100, MergeStderr: true}},
		{"timeout is capped at ten minutes", `{timeout_ms = 99999999}`, plugin.ProcOptions{Timeout: 600 * time.Second, MaxOutput: 8 << 20}},
		{"timeout zero is the default, not unlimited", `{timeout_ms = 0}`, plugin.ProcOptions{Timeout: 60 * time.Second, MaxOutput: 8 << 20}},
		{"negative timeout is the default", `{timeout_ms = -1}`, plugin.ProcOptions{Timeout: 60 * time.Second, MaxOutput: 8 << 20}},
		{"a fraction of a millisecond is one millisecond", `{timeout_ms = 0.4}`, plugin.ProcOptions{Timeout: time.Millisecond, MaxOutput: 8 << 20}},
		{"output is capped at 8 MiB", `{max_output = 1e12}`, plugin.ProcOptions{Timeout: 60 * time.Second, MaxOutput: 8 << 20}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fos := plugintest.NewFakeOS()
			fos.Script([]string{"tool"}, plugintest.ProcScript{})
			arg := ""
			if r.opts != "" {
				arg = ", " + r.opts
			}
			osCmd(t, fos, "", `gvim.proc.run({"tool", "a b"}`+arg+`)`)
			runs := fos.Runs()
			if len(runs) != 1 {
				t.Fatalf("runs = %v", runs)
			}
			got := runs[0]
			if strings.Join(got.Argv, "|") != "tool|a b" || got.Started {
				t.Fatalf("run = %+v", got)
			}
			o := got.Opts
			if o.Cwd != r.want.Cwd || o.Timeout != r.want.Timeout || o.MaxOutput != r.want.MaxOutput || o.MergeStderr != r.want.MergeStderr ||
				string(o.Stdin) != string(r.want.Stdin) || len(o.Env) != len(r.want.Env) || o.Env["A"] != r.want.Env["A"] {
				t.Fatalf("options = %+v, want %+v", o, r.want)
			}
		})
	}
}

func TestProcWhich(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Bin["git"] = "/usr/bin/git"
	h := osCmd(t, fos, "", `T.spy(gvim.proc.which("git"), tostring(gvim.proc.which("nope")))`)
	if got := spyText(h); got != "/usr/bin/git nil" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

// TestSpawnPidNilUntilStarted: spawn returns at once, the OS is not touched on the UI
// goroutine, and pid appears when the child has started in its Work.
func TestSpawnPidNilUntilStarted(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"sleepy"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
local p
T.command("go", function()
  p = gvim.proc.spawn({"sleepy"}, {on_start = function(pid) T.spy("on_start", pid) end})
  T.spy("pid before", tostring(p.pid), p:is_running())
end)
T.command("after", function()
  T.spy("pid after", p.pid, tostring(p))
  T.spy("wait_started", p:wait_started())
end)`)
	if err := h.RT.Run("t.go", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	work := drainAll(h.RT)
	if got := spyText(h); got != "pid before nil true" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if n := len(fos.Runs()); n != 0 {
		t.Fatalf("Start ran on the UI goroutine (%d runs)", n)
	}
	if len(work) != 1 || work[0].Stream == nil {
		t.Fatalf("expected one stream Work, got %+v", work)
	}
	h.RunWork(work)
	h.Settle()
	if n := len(fos.Runs()); n != 1 || !fos.Runs()[0].Started {
		t.Fatalf("runs = %+v", fos.Runs())
	}
	if err := h.Run("t.after", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "pid before nil true|on_start 1000|pid after 1000 Process(1000)|wait_started 1000" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestSpawnCallbacks(t *testing.T) {
	chunks := []plugin.ProcChunk{{Data: []byte("a\nb\r\npa")}, {Stderr: true, Data: []byte("e1")}, {Data: []byte("rt\n")}}
	rows := []struct {
		name string
		opts string
		want string
	}{
		{"chunks", ``, "out[a\nb\r\npa]|err[e1]|out[rt\n]|exit 3 false false nil|wait 3 false"},
		{"lines", `lines = true,`, "out[a]|out[b]|out[part]|err[e1]|exit 3 false false nil|wait 3 false"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fos := plugintest.NewFakeOS()
			fos.Script([]string{"tool"}, plugintest.ProcScript{Chunks: chunks, Code: 3})
			h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"tool"}, {
    `+r.opts+`
    on_stdout = function(c) T.spy("out[" .. c .. "]") end,
    on_stderr = function(c) T.spy("err[" .. c .. "]") end,
    on_exit = function(r) T.spy("exit", r.code, r.ok, r.timed_out, tostring(r.signal)) end,
  })
  local r = p:wait()
  T.spy("wait", r.code, p:is_running())
end)`)
			if err := h.Run("t.go", nil); err != nil {
				t.Fatal(err)
			}
			h.Settle()
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
			if rg := plugin.RegistryOf(h.RT); rg.Waits != 0 || rg.Parked != 0 {
				t.Fatalf("leftovers: %+v", rg)
			}
		})
	}
}

func TestSpawnStartFailure(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"tool"}, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "boom")})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"tool"}, {on_exit = function(r) T.spy("exit", tostring(r.code), r.err) end})
  T.spy("wait_started", p:wait_started())
  T.spy("wait", p:wait())
  T.spy(p:is_running(), tostring(p.pid))
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	h.Settle()
	want := "wait_started nil exec: boom|wait nil exec: boom|false nil|exit nil exec: boom"
	if got := spyText(h); !sameLines(got, want) {
		t.Fatalf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
	}
}

// TestProcessWriteBusy: a child that never reads fills the bounded stdin queue; further
// writes fail with busy at once instead of blocking.
func TestProcessWriteBusy(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"cat"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"cat"}, {stdin = true})
  p:wait_started()
  local chunk = string.rep("x", 1048576)
  local ok, busy, first = 0, 0, nil
  for i = 1, 20 do
    local v, e = p:write(chunk)
    if v then ok = ok + 1 else busy = busy + 1 first = first or gvim.err_code(e) end
  end
  T.spy(ok, busy, first)
end)`)
	begin := time.Now()
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "8 12 busy" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if d := time.Since(begin); d > 3*time.Second {
		t.Fatalf("writes blocked: %v", d)
	}
}

func TestProcessWriteBeforeStartIsQueued(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"cat"}, plugintest.ProcScript{Echo: true})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"cat"}, {stdin = true,
    on_stdout = function(c) T.spy("out", c) end, on_exit = function(r) T.spy("exit", r.code) end})
  T.spy("queued", p:write("hel"), p:write("lo"))
  p:close_stdin()
  local v, e = p:write("late")
  T.spy("after close", tostring(v), gvim.err_code(e))
  p:wait()
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	want := "queued true true|after close nil closed|out hel|out lo|exit 0"
	if got := spyText(h); !sameLines(got, want) {
		t.Fatalf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
	}
	p := fos.Procs()[0]
	if string(p.Written()) != "hello" || !p.StdinClosed() {
		t.Fatalf("written = %q closed = %v", p.Written(), p.StdinClosed())
	}
}

func TestProcessWriteRefusedWithoutStdin(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"tool"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"tool"})
  p:wait_started()
  local v, e = p:write("x")
  T.spy(tostring(v), gvim.err_code(e))
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "nil closed" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestProcessKill(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"sleepy"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"sleepy"}, {on_exit = function(r) T.spy("on_exit", r.code, r.signal) end})
  p:wait_started()
  T.spy(p:kill("sigkill"))
  local r = p:wait()
  T.spy(r.signal, r.code, r.ok)
  local v, e = p:kill()
  T.spy(tostring(v), gvim.err_code(e))
  local ok, err = pcall(function() p:kill("USR1") end)
  T.spy(ok, err:match("bad argument.*$"))
end)
T.command("early", function()
  local p = gvim.proc.spawn({"sleepy"})
  T.spy(p:kill())
  T.spy(p:kill("KILL"))
  p:wait()
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	want := `true|KILL -1 false|nil closed|false bad argument #2 to kill (signal must be "TERM", "KILL", "INT" or "HUP")|on_exit -1 KILL`
	if got := spyText(h); !sameLines(got, want) {
		t.Fatalf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
	}
	if err := h.Run("t.early", nil); err != nil {
		t.Fatal(err)
	}
	// a kill requested before the child started is applied when it does; KILL beats TERM
	if sig := fos.Procs()[1].Signals(); len(sig) != 1 || sig[0] != "KILL" {
		t.Fatalf("signals = %v", sig)
	}
}

func TestProcessQuota(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"sleepy"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
T.command("go", function()
  local last
  for i = 1, 17 do
    local p, e = gvim.proc.spawn({"sleepy"})
    if not p then last = e end
  end
  T.spy(last)
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "limit: process quota (16)" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

// TestProcessesDieWithThePlugin: unloading terminates the children with the grace period,
// and nothing stays registered.
func TestProcessesDieWithThePlugin(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"sleepy"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"sleepy"})
  p:wait_started()
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	p := fos.Procs()[0]
	h.Unload("t")
	if p.Terminated() != 2*time.Second {
		t.Fatalf("Terminate grace = %v", p.Terminated())
	}
	if rg := plugin.RegistryOf(h.RT); rg.Waits != 0 || rg.Plugins != 0 {
		t.Fatalf("registry = %+v", rg)
	}
	if n := fos.BlockedWaiters(); n != 0 {
		t.Fatalf("%d goroutines still blocked", n)
	}
}

// TestStopKillsProcessesAtOnce: Runtime.Stop sends SIGKILL, it does not wait out a grace period.
func TestStopKillsProcessesAtOnce(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"sleepy"}, plugintest.ProcScript{Hold: true})
	h := startOS(t, fos, `
T.command("go", function()
  local p = gvim.proc.spawn({"sleepy"})
  p:wait_started()
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	p := fos.Procs()[0]
	h.RT.Stop()
	if sig := p.Signals(); len(sig) == 0 || sig[0] != "KILL" {
		t.Fatalf("signals = %v", sig)
	}
	if !fos.ShutdownCalled() {
		t.Fatal("OS.Shutdown was not called")
	}
}

func TestWatch(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.SetFile("/work/a.txt", "1")
	h := startOS(t, fos, `
local w
T.command("go", function()
  w = gvim.fs.watch("/work", function(ev) T.spy(ev.kind, ev.path) end, {debounce_ms = 5})
  T.spy("active", w:active())
end)
T.command("stop", function()
  T.spy("stop", w:stop(), w:active())
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	ws := fos.Watches()
	if len(ws) != 1 || ws[0].Path() != "/work" || ws[0].Options().Debounce != 10*time.Millisecond {
		t.Fatalf("watches = %+v", ws)
	}
	fos.Emit("/work/a.txt", "write")
	h.Settle()
	fos.Emit("/work/b.txt", "create")
	h.Settle()
	if err := h.Run("t.stop", nil); err != nil {
		t.Fatal(err)
	}
	fos.Emit("/work/c.txt", "write")
	h.Settle()
	want := "active true|write /work/a.txt|create /work/b.txt|stop true false"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
	}
	if !ws[0].Stopped() {
		t.Fatal("the OS watch is still running")
	}
}

// TestWatchErrorDelivered: a watch that cannot be added reports once through the callback.
func TestWatchErrorDelivered(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.FailWatch("/work/nope", pluginapi.Errf(pluginapi.CodeLimit, "directory too large to watch"))
	h := startOS(t, fos, `
local w
T.command("go", function()
  w = gvim.fs.watch("/work/nope", function(ev) T.spy(ev.kind, gvim.err_code(ev.err), tostring(ev.path)) end)
  T.spy("returned", tostring(w), w:active())
end)
T.command("late", function() T.spy("active now", w:active()) end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("t.late", nil); err != nil {
		t.Fatal(err)
	}
	want := "returned Watcher(1) true|error limit nil|active now false"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
	}
}

func TestWatchStopsWithThePlugin(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startOS(t, fos, `gvim.fs.watch("/work", function() T.spy("never") end)`)
	h.Settle()
	ws := fos.Watches()
	if len(ws) != 1 {
		t.Fatalf("watches = %d", len(ws))
	}
	h.Unload("t")
	// the Work goroutine notices the cancelled context a moment later
	for i := 0; i < 400 && !ws[0].Stopped(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !ws[0].Stopped() {
		t.Fatal("watch survived the unload")
	}
	if rg := plugin.RegistryOf(h.RT); rg.Waits != 0 {
		t.Fatalf("registry = %+v", rg)
	}
}

func TestWatchQuotaAndArguments(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startOS(t, fos, `
T.command("go", function()
  local last
  for i = 1, 33 do
    local w, e = gvim.fs.watch("/work", function() end)
    if not w then last = e end
  end
  T.spy(last)
  local ok, e = pcall(function() return gvim.fs.watch("/work", 5) end)
  T.spy(ok, e:match("bad argument.*$"))
  local v, err = gvim.fs.watch("", function() end)
  T.spy(tostring(v), gvim.err_code(err))
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	want := "limit: watcher quota (32)|false bad argument #2 to watch (function expected, got number)|nil invalid"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
	}
}
