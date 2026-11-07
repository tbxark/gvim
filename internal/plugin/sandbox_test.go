package plugin_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

func TestSandboxGlobalsAreGone(t *testing.T) {
	names := []string{
		"dofile", "loadfile", "load", "loadstring", "module", "newproxy", "collectgarbage",
		"getfenv", "setfenv", "_printregs", "_GOPHER_LUA_VERSION", "io", "debug", "package", "channel",
		"__await", "__mk", "__tag", "__yield", "__resume", "__suspend", "__raw_await",
	}
	var body strings.Builder
	for _, n := range names {
		body.WriteString(`T.spy("` + n + `", tostring(rawget(_G, "` + n + `")))` + "\n")
	}
	body.WriteString(`T.spy("string.dump", tostring(string.dump))` + "\n")
	body.WriteString(`T.spy("string.gfind", tostring(string.gfind))` + "\n")
	body.WriteString(`T.spy("math.mod", tostring(math.mod))` + "\n")
	body.WriteString(`T.spy("math.pow", tostring(math.pow))` + "\n")
	h := start(t, body.String())
	for _, line := range spy(h) {
		if !strings.HasSuffix(line, " nil") {
			t.Errorf("%s is reachable", strings.TrimSuffix(line, " nil"))
		}
	}
	if len(spy(h)) != len(names)+4 {
		t.Fatalf("only %d rows ran: log=%v", len(spy(h)), h.LogText(""))
	}
}

func TestSandboxAttacks(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"require os", `T.spy((pcall(require, "os")))`, "false"},
		{"require io", `T.spy((pcall(require, "io")))`, "false"},
		{"require debug", `T.spy((pcall(require, "debug")))`, "false"},
		{"require parent", `T.spy((pcall(require, "../x")))`, "false"},
		{"require absolute", `T.spy((pcall(require, "/etc/passwd")))`, "false"},
		{"require slash", `T.spy((pcall(require, "a/b")))`, "false"},
		{"require dotdot", `T.spy((pcall(require, "a..b")))`, "false"},
		{"require leading dot", `T.spy((pcall(require, ".a")))`, "false"},
		{"require trailing dot", `T.spy((pcall(require, "a.")))`, "false"},
		{"require empty", `T.spy((pcall(require, "")))`, "false"},
		{"require backslash", `T.spy((pcall(require, "a\\b")))`, "false"},
		{"load string", `T.spy(pcall(function() return load("return 1")() end))`, "false"},
		{"string metatable is locked", `T.spy(getmetatable("").__metatable, (pcall(setmetatable, "x", {__index = function() return "HIJACK" end})))`, "locked false"},
		{"string methods survive", `pcall(setmetatable, "x", {}) T.spy(("abc"):upper())`, "ABC"},
		{"handle metatables are locked", `local b = T.buf(1) T.spy(getmetatable(b), (pcall(setmetatable, b, {})))`, "locked false"},
		{"handles are read-only", `local b = T.buf(1) T.spy((pcall(function() b.x = 1 end)))`, "false"},
		{"print lands in the log", `print("hello", 42, nil, true) T.spy("printed")`, "printed"},
		{"version string", `T.spy(_VERSION)`, "Lua 5.1 (gopher-lua)"},
		{"resume of the running root", `T.spy((pcall(coroutine.resume, coroutine.running())))`, "false"},
		{"running is nil in a root", `T.spy(tostring(coroutine.running()))`, "nil"},
		{"running is a thread in a coroutine", `T.spy(coroutine.wrap(function() return type(coroutine.running()) end)())`, "thread"},
		{"resume of an ancestor coroutine", `local a, b
a = coroutine.create(function() return coroutine.resume(b) end)
b = coroutine.create(function() return coroutine.resume(a) end)
T.spy(coroutine.resume(a))`, "true true false"},
		{"resume of an ancestor through wrap", `local me
coroutine.wrap(function() me = coroutine.running(); T.spy(pcall(coroutine.wrap(function() return coroutine.resume(me) end))) end)()`, "true false cannot resume non-suspended coroutine"},
		{"resume of itself", `local co
co = coroutine.create(function() return coroutine.resume(co) end)
T.spy(coroutine.resume(co))`, "true false"},
		{"forged yield at the root", `T.spy(pcall(coroutine.yield, "fs_write", "/x", "y"))`, "false"},
		{"forged yield returns to the wrapper's caller", `local f = coroutine.wrap(function() coroutine.yield("fs_write", "/x", "y") end) T.spy(f())`, "fs_write /x y"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			got := spyText(h)
			if !strings.HasPrefix(got, r.want) {
				t.Fatalf("spy = %q, want prefix %q\nlog: %v", got, r.want, h.LogText(""))
			}
			if r.name == "print lands in the log" && !logHas(h, "t", "hello\t42\tnil\ttrue") {
				t.Fatalf("log = %v", h.LogText(""))
			}
		})
	}
}

func TestForgedYieldNeverReachesTheHost(t *testing.T) {
	// A plugin must not be able to ask the host for an async operation by yielding a kind
	// name itself: at the root the yield is refused, in a coroutine it goes to its resumer.
	for name, body := range map[string]string{
		"root":      `coroutine.yield("t_echo", "x")`,
		"in pcall":  `pcall(coroutine.yield, "t_echo", "x")`,
		"tag guess": `coroutine.yield({}, "t_echo", "x")`,
	} {
		t.Run(name, func(t *testing.T) {
			h := cmd(t, "", body)
			if r := plugin.RegistryOf(h.RT); r.Waits != 0 || r.Parked != 0 {
				t.Fatalf("registry = %+v", r)
			}
			if plugin.Strikes(h.RT, "t") != 0 {
				t.Fatal("refusing a forged yield is an ordinary error, not a strike")
			}
		})
	}
}

func TestPreludeSurvivesTampering(t *testing.T) {
	// The prelude captured the originals in upvalues: replacing globals changes nothing for
	// async calls, pcall inside the runtime's own wrappers, or require.
	h := cmd(t, "", `
local real_pcall = pcall
pcall = nil
error = nil
select = nil
unpack = nil
type = nil
coroutine.create = nil
coroutine.status = nil
coroutine.resume = nil
local gvim = require "gvim"
local k = gvim.async.spawn(function() return T.echo("still works") end)
T.spy(k:wait())
local r = gvim.async.all({function() return 1 end, function() return 2 end})
T.spy(r[1][1] + r[2][1])
T.spy(real_pcall ~= nil)`)
	if got := spyText(h); got != "still works|3|true" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestPluginsAreIsolated(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"a.lua": `
T.command("go", function()
  leak = "from a"
  getmetatable("").__index.upper = function() return "PWNED" end
  string.len = nil
  T.spy("a", ("x"):upper())
end)`,
		"b.lua": `
T.command("go", function() T.spy("b", tostring(leak), ("x"):upper(), type(string.len)) end)`,
	})
	if err := h.Run("a.go", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("b.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "a PWNED|b nil X function" {
		t.Fatalf("spy = %q", got)
	}
}

func TestUnpack100kNestedVarargs(t *testing.T) {
	// The registry grows (RegistryMaxSize 1<<20): 100k values through unpack and vararg calls.
	h := cmd(t, "", `
local t = {}
for i = 1, 100000 do t[i] = i end
local function count(...) return select("#", ...) end
local function pass(...) return count(...) end
T.spy(select("#", unpack(t)), pass(unpack(t)), #table.concat(t, ","))`, plugintest.Options{Timeout: 10 * time.Second})
	if got := spyText(h); got != "100000 100000 588894" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestErrorTextNeverCallsTostring(t *testing.T) {
	// A thrown object with a hostile __tostring: the runtime formats the error without
	// calling it (it would run outside any budget).
	h := cmd(t, "", `
error(setmetatable({}, {__tostring = function() T.spy("tostring called") while true do end end}))`)
	if strings.Contains(spyText(h), "tostring called") {
		t.Fatal("the error formatter invoked __tostring")
	}
	if !logHas(h, "t", "error object (table)") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	for name, want := range map[string]string{
		`error({message = "from table"})`: "from table",
		`error(42)`:                       "42",
		`error(nil)`:                      "nil",
	} {
		h := cmd(t, "", name)
		if !noticeHas(h, want) {
			t.Errorf("%s: notices = %v", name, h.NoticeTexts())
		}
	}
}

func TestCompileHappensInWork(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{
		"a.lua":      `T.spy("a")`,
		"b/init.lua": `require("util") T.spy("b")`, "b/lua/util.lua": `return {}`,
	}, plugintest.Options{})
	h.Start()
	if h.CompileOnUIGoroutine {
		t.Fatal("plugins were compiled on the UI goroutine")
	}
	if got := spyText(h); got != "a|b" {
		t.Fatalf("spy = %q", got)
	}
	// The runtime never parses: a source without compiled files cannot load.
	err := h.RT.LoadPlugin(plugin.Source{Name: "raw"})
	if err == nil {
		t.Fatal("LoadPlugin accepted a plugin without compiled code")
	}
	if p, _ := h.Plugin("raw"); p.Status != "failed" {
		t.Fatalf("status = %q", p.Status)
	}
}

func TestCompileErrorsAndSizeCap(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{
		"bad.lua": "local x = = 1",
		"big.lua": "local x = 1 --" + strings.Repeat("y", plugin.MaxLuaFile),
		"ok.lua":  `T.spy("ok")`,
	}, plugintest.Options{})
	h.Start()
	for name, want := range map[string]string{"bad": "syntax error", "big": "source too large"} {
		p, ok := h.Plugin(name)
		if !ok || p.Status != "failed" || !strings.Contains(p.Err, want) {
			t.Errorf("%s: %+v, want failed with %q", name, p, want)
		}
	}
	if got := spyText(h); got != "ok" {
		t.Fatalf("spy = %q", got)
	}
}

func TestPcallOfGoFunctions(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"string.upper", `local r = table.pack(pcall(string.upper, "x")) T.spy(r.n, tostring(r[1]), tostring(r[2]))`, "2 true X"},
		{"math.floor", `local r = table.pack(pcall(math.floor, 1.5)) T.spy(r.n, tostring(r[1]), tostring(r[2]))`, "2 true 1"},
		{"no result", `local r = table.pack(pcall(print)) T.spy(r.n, tostring(r[1]))`, "1 true"},
		{"many results", `local r = table.pack(pcall(string.find, "abc", "b")) T.spy(r.n, tostring(r[1]), r[2], r[3])`, "3 true 2 2"},
		{"go error", `local ok, e = pcall(string.rep) T.spy(tostring(ok), type(e))`, "false string"},
		{"xpcall", `T.spy(xpcall(string.upper, print, "x"))`, "true X"},
		{"xpcall runs the handler of a Go error", `T.spy(xpcall(string.rep, function(e) return "H:" .. type(e) end))`, "false H:string"},
		{"xpcall handler not run on success", `T.spy(xpcall(tostring, function() return "H" end, 3))`, "true 3"},
		{"tostring", `T.spy(pcall(tostring, 3))`, "true 3"},
		{"string.rep", `T.spy(pcall(string.rep, "x", 3))`, "true xxx"},
		{"json.decode", `local ok, v = pcall(json.decode, "[1]") T.spy(ok, #v)`, "true 1"},
		{"re.find", `T.spy(pcall(re.find, "hello", "l+"))`, "true 3 4"},
		{"gvim.text.trim", `local gvim = require "gvim" T.spy(pcall(gvim.text.trim, "  x "))`, "true x"},
		{"coroutine.wrap of a Go function", `T.spy(coroutine.wrap(string.rep)("x", 3))`, "xxx"},
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

// A Go function used as a coroutine body, a command or an event handler must keep its results
// and not fail with a nil error (S7-1).
func TestGoFunctionsAsEntryPoints(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"pcall string.rep", `T.spy(pcall(string.rep, "x", 2))`, "true xx"},
		{"pcall gvim.emit", `T.spy(pcall(gvim.emit, "t:x", {}))`, "true"},
		{"pcall tostring", `T.spy(pcall(tostring, 5))`, "true 5"},
		{"pcall gvim.command.run", `
gvim.command.register{ name = "a", run = function() T.spy("ran") end }
T.spy(pcall(gvim.command.run, "t.a"))`, "true true|ran"},
		{"resume of a Go function", `T.spy(coroutine.resume(coroutine.create(string.rep), "x", 2))`, "true xx"},
		{"wrap of a Go function", `T.spy(coroutine.wrap(string.rep)("x", 2))`, "xx"},
		{"create of a non-function still raises", `T.spy(pcall(coroutine.create, 5))`, "false"},
		{"command run is a Go function", `
gvim.command.register{ name = "p", run = function() end }
gvim.command.register{ name = "q", run = T.spy }
T.spy(pcall(gvim.command.run, "t.q"))`, "true true|table: 0x"},
		{"event handler is a Go function", `gvim.on("t:x", T.spy) gvim.emit("t:x", { k = "v" })`, "table: 0x"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, gv+r.body)
			h.Settle()
			if logHas(h, "t", "error") {
				t.Fatalf("log: %v", h.LogText("t"))
			}
			if got := spyText(h); !strings.HasPrefix(got, r.want) {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText("t"))
			}
		})
	}
}

func TestSparseIntegerKeysDoNotPadTheArray(t *testing.T) {
	// A store at a sparse integer key below gopher-lua's default MaxArrayIndex would append
	// nils up to that index: a gigabyte in a single VM instruction.
	rows := []struct{ name, body string }{
		{"index", `local t = {} t[67108863] = 1 T.spy(t[67108863])`},
		{"constructor", `local t = {[67108863] = 1} T.spy(t[67108863])`},
		{"rawset", `local t = {} rawset(t, 67108863, 1) T.spy(t[67108863])`},
		{"insert", `local t = {} table.insert(t, 67108863, 1) T.spy(t[67108863])`},
		{"a line number of a large file", `local t = {} t[2833990] = 1 T.spy(t[2833990])`},
		{"thirty million", `local t = {} t[30000000] = 1 T.spy(t[30000000])`},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			h := cmd(t, "", r.body)
			runtime.ReadMemStats(&after)
			if got := spyText(h); got != "1" {
				t.Fatalf("spy = %q log=%v", got, h.LogText(""))
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 256<<20 {
				t.Fatalf("one store allocated %d MiB", grew>>20)
			}
		})
	}
}
