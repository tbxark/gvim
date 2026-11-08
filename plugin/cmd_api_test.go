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

// cmdHost is a fake host that knows some editor actions.
func cmdHost() *plugintest.FakeHost {
	h := plugintest.NewFakeHost()
	h.ActionList = []string{"move_down", "delete_line", "insert_char", "undo"}
	return h
}

const gv = `local gvim = require "gvim"` + "\n"

func TestCommandsKeys(t *testing.T) {
	t.Run("register publishes the command", func(t *testing.T) {
		h := start(t, gv+`
local c = gvim.command.register{
  name = "hello", title = "Say Hello", description = "Greets\nthe user",
  when = { "has_buffer", "!read_only", "language:go" }, record = false, hidden = true,
  keys = { "ctrl+alt+b", "g   g" },
  run = function() end,
}
T.spy(tostring(c), type(c.unregister))`)
		if got := spyText(h); got != "Command(t.hello) function" {
			t.Fatalf("spy = %q", got)
		}
		want := []pluginapi.CommandInfo{{
			ID: "t.hello", Title: "Say Hello", Description: "Greets the user", Plugin: "t",
			When: []string{"has_buffer", "!read_only", "language:go"}, Hidden: true, Record: false,
		}}
		if got := h.Host.Commands["t"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("commands = %+v, want %+v", got, want)
		}
		wantB := []pluginapi.Binding{
			{Chord: "ctrl+alt+b", Action: "t.hello", Plugin: "t"},
			{Chord: "g g", Action: "t.hello", Plugin: "t"},
		}
		if got := h.Host.Bindings["t"]; !reflect.DeepEqual(got, wantB) {
			t.Fatalf("bindings = %+v, want %+v", got, wantB)
		}
		if !h.RT.Owns("t.hello") {
			t.Fatal("Owns(t.hello) = false")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		h := start(t, gv+`gvim.command.register{ name = "plain", run = function() end }`)
		got := h.Host.Commands["t"]
		if len(got) != 1 || got[0].Title != "plain" || !got[0].Record || got[0].Hidden || len(got[0].When) != 0 {
			t.Fatalf("commands = %+v", got)
		}
		if len(h.Host.Bindings["t"]) != 0 {
			t.Fatalf("bindings = %+v", h.Host.Bindings["t"])
		}
	})

	// Registration problems: wrong types and duplicates raise, bad values are `nil, invalid`.
	rows := []struct {
		name string
		lua  string
		want string // spy line
	}{
		{"duplicate raises", `
gvim.command.register{ name = "a", run = function() end }
local ok, err = pcall(gvim.command.register, { name = "a", run = function() end })
T.spy(ok, tostring(err):match("already registered") or err)`, "false already registered"},
		{"missing run raises", `
local ok, err = pcall(gvim.command.register, { name = "a" })
T.spy(ok, tostring(err):match('"run"') or err)`, `false "run"`},
		{"missing name raises", `
local ok, err = pcall(gvim.command.register, { run = function() end })
T.spy(ok, tostring(err):match('"name"') or err)`, `false "name"`},
		{"wrong title type raises", `
local ok = pcall(gvim.command.register, { name = "a", title = 5, run = function() end })
T.spy(ok)`, "false"},
		{"keys must be strings", `
local ok = pcall(gvim.command.register, { name = "a", keys = { 1 }, run = function() end })
T.spy(ok)`, "false"},
		{"not a table raises", `T.spy((pcall(gvim.command.register, "a")))`, "false"},
		{"bad name", `
for _, n in ipairs{ "Bad", "1x", "", "has space", "a.b", ("x"):rep(41), "__fn1" } do
  local c, err = gvim.command.register{ name = n, run = function() end }
  T.spy(c == nil, gvim.err_code(err))
end`, strings.Repeat("true invalid|", 6) + "true invalid"},
		{"bad when", `
local c, err = gvim.command.register{ name = "a", when = { "has buffer" }, run = function() end }
T.spy(c == nil, gvim.err_code(err))`, "true invalid"},
		{"bad key leaves nothing registered", `
local c, err = gvim.command.register{ name = "a", keys = { "ctrl+a", "" }, run = function() end }
T.spy(c == nil, gvim.err_code(err), #gvim.command.list{ plugin = "t" })`, "true invalid 0"},
		{"key sequence too long", `
local c, err = gvim.command.register{ name = "a", keys = { "a b c d e" }, run = function() end }
T.spy(c == nil, gvim.err_code(err))`, "true invalid"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, gv+r.lua)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}

	// The quota rows register hundreds of commands and thousands of bindings in one main
	// chunk; what they test is the quota, not the time budget, so the budget is generous
	// and a loaded machine cannot turn a quota row into a timeout.
	quotaOpts := plugintest.Options{Timeout: 30 * time.Second}

	t.Run("command quota", func(t *testing.T) {
		h := start(t, gv+`
local n = 0
for i = 1, 600 do
  local c, err = gvim.command.register{ name = "c" .. i, run = function() end }
  if c then n = n + 1 else T.spy(i, gvim.err_code(err)) break end
end
T.spy(n)`, quotaOpts)
		if got := spyText(h); got != fmt.Sprintf("%d limit|%d", plugin.QuotaCommands+1, plugin.QuotaCommands) {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("binding quota is checked before the command is added", func(t *testing.T) {
		h := start(t, gv+`
local function ch(n) return string.char(97 + n % 26) .. " " .. string.char(97 + math.floor(n / 26) % 26) .. " " .. string.char(97 + math.floor(n / 676) % 26) end
local n = 0
for i = 1, 300 do
  local keys = {}
  for k = 1, 16 do keys[k] = ch(i * 16 + k) end
  local c, err = gvim.command.register{ name = "c" .. i, keys = keys, run = function() end }
  if not c then T.spy(gvim.err_code(err), #gvim.command.list{ plugin = "t" } == n) break end
  n = n + 1
end`, quotaOpts)
		if got := spyText(h); got != "limit true" {
			t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
		}
		if n := len(h.Host.Bindings["t"]); n > plugin.QuotaBindings || n < plugin.QuotaBindings-16 {
			t.Fatalf("%d bindings", n)
		}
	})

	t.Run("a later command takes over a chord", func(t *testing.T) {
		h := start(t, gv+`
gvim.command.register{ name = "a", keys = { "ctrl+k" }, run = function() end }
gvim.command.register{ name = "b", keys = { "Ctrl+K" }, run = function() end }`)
		want := []pluginapi.Binding{{Chord: "Ctrl+K", Action: "t.b", Plugin: "t"}}
		if got := h.Host.Bindings["t"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("bindings = %+v", got)
		}
	})

	t.Run("unregister", func(t *testing.T) {
		h := start(t, gv+`
local a = gvim.command.register{ name = "a", keys = { "ctrl+a" }, run = function() end }
gvim.command.register{ name = "b", keys = { "ctrl+b" }, run = function() end }
T.spy(gvim.command.unregister("b"))
local _, err = gvim.command.unregister("b")
T.spy(gvim.err_code(err))
T.spy(a:unregister())
local _, err2 = a:unregister()
T.spy(gvim.err_code(err2))
-- a handle of an unregistered command does not touch a new command of the same name
local old = gvim.command.register{ name = "c", run = function() end }
gvim.command.unregister("c")
gvim.command.register{ name = "c", run = function() end }
local _, err3 = old:unregister()
T.spy(gvim.err_code(err3), #gvim.command.list{ plugin = "t" })`)
		if got := spyText(h); got != "true|not_found|true|not_found|not_found 1" {
			t.Fatalf("spy = %q", got)
		}
		if got := h.Host.Commands["t"]; len(got) != 1 || got[0].ID != "t.c" {
			t.Fatalf("commands = %+v", got)
		}
		if got := h.Host.Bindings["t"]; len(got) != 0 {
			t.Fatalf("bindings left: %+v", got)
		}
		if h.RT.Owns("t.a") || !h.RT.Owns("t.c") {
			t.Fatal("Owns does not follow unregister")
		}
	})

	t.Run("unload releases commands and bindings", func(t *testing.T) {
		h := startFiles(t, plugintest.Files{"base.lua": ``})
		base := h.Host.Resources()
		if err := h.Load("p", gv+`gvim.command.register{ name = "a", keys = { "ctrl+a" }, run = function() end }`); err != nil {
			t.Fatal(err)
		}
		if got := h.Host.Resources(); !reflect.DeepEqual(got, []string{"binding:p/ctrl+a", "command:p/p.a"}) {
			t.Fatalf("resources = %v", got)
		}
		h.Unload("p")
		if got := h.Host.Resources(); !reflect.DeepEqual(got, base) {
			t.Fatalf("resources after unload = %v", got)
		}
	})
}

func TestCommandRunPluginCommand(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"a.lua": gv + `
gvim.command.register{ name = "go", run = function()
  T.spy("a.go start")
  T.spy(gvim.command.run("b.hi", { x = 1, count = 3 }))
  T.spy("a.go end")
end }`,
		"b.lua": gv + `
gvim.command.register{ name = "hi", run = function(ctx)
  T.spy("b.hi", ctx.source, ctx.args.x, ctx.count)
end }`,
	}, plugintest.Options{Host: cmdHost()})
	if err := h.Run("a.go", nil); err != nil {
		t.Fatal(err)
	}
	// The target runs after the calling slice ended (the caller is in a slice: queued FIFO).
	if got := spyText(h); got != "a.go start|true|a.go end|b.hi api 1 3" {
		t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
	}
	if n := h.Host.CallCount("Do"); n != 0 {
		t.Fatalf("a plugin command went to Host.Do (%d calls)", n)
	}
}

func TestCommandRunEditorAction(t *testing.T) {
	rows := []struct {
		name   string
		lua    string
		spy    string
		calls  []plugintest.DoCall
		errors map[string]error
	}{
		{"plain", `T.spy(gvim.command.run("move_down"))`, "true",
			[]plugintest.DoCall{{Action: "move_down", Count: 1}}, nil},
		{"args and count", `T.spy(gvim.command.run("insert_char", { text = "x", count = 3 }))`, "true",
			[]plugintest.DoCall{{Action: "insert_char", Args: map[string]any{"text": "x"}, Count: 3}}, nil},
		{"empty args", `T.spy(gvim.command.run("undo", {}))`, "true",
			[]plugintest.DoCall{{Action: "undo", Args: map[string]any{}, Count: 1}}, nil},
		{"count too large", `local ok, err = gvim.command.run("move_down", { count = 10001 }) T.spy(ok, gvim.err_code(err))`,
			"nil limit", nil, nil},
		{"count max", `T.spy(gvim.command.run("move_down", { count = 10000 }))`, "true",
			[]plugintest.DoCall{{Action: "move_down", Args: map[string]any{}, Count: 10000}}, nil},
		{"count zero", `local ok, err = gvim.command.run("move_down", { count = 0 }) T.spy(ok, gvim.err_code(err))`, "nil invalid", nil, nil},
		{"count fraction", `local ok, err = gvim.command.run("move_down", { count = 1.5 }) T.spy(ok, gvim.err_code(err))`, "nil invalid", nil, nil},
		{"count string", `local ok, err = gvim.command.run("move_down", { count = "2" }) T.spy(ok, gvim.err_code(err))`, "nil invalid", nil, nil},
		{"unknown action", `local ok, err = gvim.command.run("no_such_action") T.spy(ok, gvim.err_code(err))`,
			"nil unsupported", nil, nil},
		{"unknown plugin command", `local ok, err = gvim.command.run("t.nothing") T.spy(ok, gvim.err_code(err))`,
			"nil unsupported", nil, nil},
		{"host error comes back", `local ok, err = gvim.command.run("undo") T.spy(ok, err)`,
			"nil read_only: nope", []plugintest.DoCall{{Action: "undo", Count: 1}},
			map[string]error{"undo": pluginapi.Errf(pluginapi.CodeReadOnly, "nope")}},
		{"args with a function", `local ok, err = gvim.command.run("undo", { f = function() end }) T.spy(ok, gvim.err_code(err))`,
			"nil invalid", nil, nil},
		{"args not a table raises", `T.spy((pcall(gvim.command.run, "undo", 5)))`, "false", nil, nil},
		{"args an array raises", `T.spy((pcall(gvim.command.run, "undo", { 1, 2 })))`, "false", nil, nil},
		{"empty name raises", `T.spy((pcall(gvim.command.run, "")))`, "false", nil, nil},
		{"name not a string raises", `T.spy((pcall(gvim.command.run, nil)))`, "false", nil, nil},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			host := cmdHost()
			host.Cmd().Errors = r.errors
			h := start(t, gv+r.lua, plugintest.Options{Host: host})
			if got := spyText(h); got != r.spy {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.spy, h.LogText(""))
			}
			var calls []plugintest.DoCall
			calls = append(calls, host.Cmd().Calls...)
			if !reflect.DeepEqual(calls, r.calls) {
				t.Fatalf("Do calls = %+v, want %+v", calls, r.calls)
			}
		})
	}
}

func TestCommandRunAll(t *testing.T) {
	rows := []struct {
		name  string
		lua   string
		spy   string
		calls []plugintest.DoCall
		errs  map[string]error
	}{
		{"all step forms", `T.spy(gvim.command.run_all{
  "move_down",
  { "insert_char", { text = "x" } },
  { action = "delete_line", count = 3 },
  { action = "insert_char", args = { text = "y", count = 2 } },
})`, "true", []plugintest.DoCall{
			{Action: "move_down", Count: 1, Group: 1},
			{Action: "insert_char", Args: map[string]any{"text": "x"}, Count: 1, Group: 1},
			{Action: "delete_line", Args: map[string]any{}, Count: 3, Group: 1},
			{Action: "insert_char", Args: map[string]any{"text": "y"}, Count: 2, Group: 1},
		}, nil},
		{"empty never reaches the host", `T.spy(gvim.command.run_all{})`, "true", nil, nil},
		{"stops at the first error", `local ok, err = gvim.command.run_all{ "move_down", "undo", "delete_line" } T.spy(ok, err)`,
			"nil io: boom", []plugintest.DoCall{
				{Action: "move_down", Count: 1, Group: 1}, {Action: "undo", Count: 1, Group: 1},
			}, map[string]error{"undo": fmt.Errorf("boom")}},
		{"bad count names the step", `local ok, err = gvim.command.run_all{ "move_down", { action = "undo", count = 0 } } T.spy(ok, err)`,
			"nil invalid: step 2: count must be a positive integer", nil, nil},
		{"too many repeats", `local ok, err = gvim.command.run_all{ { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 10000 }, { action = "undo", count = 1 } }
T.spy(ok, gvim.err_code(err))`, "nil limit", nil, nil},
		{"a number step raises", `T.spy((pcall(gvim.command.run_all, { 5 })))`, "false", nil, nil},
		{"a table without an action raises", `T.spy((pcall(gvim.command.run_all, { { count = 1 } })))`, "false", nil, nil},
		{"steps must be a table", `T.spy((pcall(gvim.command.run_all, "undo")))`, "false", nil, nil},
		{"args that are not a table raise", `T.spy((pcall(gvim.command.run_all, { { "undo", 3 } })))`, "false", nil, nil},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			host := cmdHost()
			host.Cmd().Errors = r.errs
			h := start(t, gv+r.lua, plugintest.Options{Host: host})
			if got := spyText(h); got != r.spy {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.spy, h.LogText(""))
			}
			if got := host.Cmd().Calls; !reflect.DeepEqual(got, r.calls) && (len(got) != 0 || len(r.calls) != 0) {
				t.Fatalf("calls = %+v, want %+v", host.Cmd().Calls, r.calls)
			}
		})
	}
	t.Run("too many steps", func(t *testing.T) {
		host := cmdHost()
		h := start(t, gv+`
local steps = {}
for i = 1, 10001 do steps[i] = "undo" end
local ok, err = gvim.command.run_all(steps)
T.spy(ok, gvim.err_code(err))`, plugintest.Options{Host: host})
		if got := spyText(h); got != "nil limit" || len(host.Cmd().Calls) != 0 {
			t.Fatalf("spy = %q, %d calls", got, len(host.Cmd().Calls))
		}
	})
}

func TestCommandListActionsPalette(t *testing.T) {
	host := cmdHost()
	h := startFiles(t, plugintest.Files{
		"a.lua": gv + `
gvim.command.register{ name = "show", title = "A: Show It", run = function() end }
gvim.command.register{ name = "secret", title = "A: Secret", hidden = true, run = function() end }
local function names(list) local out = {} for i, c in ipairs(list) do out[i] = c.id .. (c.builtin and "*" or "") end return table.concat(out, ",") end
gvim.command.register{ name = "probe", run = function()
  T.spy("all", names(gvim.command.list()))
  T.spy("query", names(gvim.command.list("DELETE")))
  T.spy("title", names(gvim.command.list("show it")))
  T.spy("plugin", names(gvim.command.list{ plugin = "b" }))
  T.spy("builtin", names(gvim.command.list{ builtin = true, query = "_" }))
  T.spy("not builtin", names(gvim.command.list{ builtin = false }))
  local row = gvim.command.list{ plugin = "a", query = "show" }[1]
  T.spy("row", row.id, row.title, row.plugin, tostring(row.builtin))
  T.spy("actions", table.concat(gvim.command.actions(), ","))
  T.spy(gvim.command.palette("> split"), gvim.command.palette())
  T.spy((pcall(gvim.command.list, 5)))
end }`,
		"b.lua": gv + `gvim.command.register{ name = "other", run = function() end }`,
	}, plugintest.Options{Host: host})
	if err := h.Run("a.probe", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"all delete_line*,insert_char*,move_down*,undo*,a.probe,a.show,b.other",
		"query delete_line*",
		"title a.show",
		"plugin b.other",
		"builtin delete_line*,insert_char*,move_down*",
		"not builtin a.probe,a.show,b.other",
		"row a.show A: Show It a false",
		"actions delete_line,insert_char,move_down,undo",
		"true true",
		"false",
	}
	if got := spy(h); !reflect.DeepEqual(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q", got, want)
	}
	if got := host.Cmd().Palette; !reflect.DeepEqual(got, []string{"> split", ""}) {
		t.Fatalf("palette queries = %q", got)
	}
}

// TestCommandRunTargetsActiveDocument: run and run_all hand the action to the host with no
// buffer argument; the host runs it on its active document (AMENDMENTS C15).
func TestCommandRunTargetsActiveDocument(t *testing.T) {
	host := cmdHost()
	host.AddBuffer("a.go", "x")
	host.AddBuffer("b.go", "y")
	h := start(t, gv+`
T.spy(gvim.command.run("undo"))
T.spy(gvim.command.run_all{ "move_down" })`, plugintest.Options{Host: host})
	if got := spyText(h); got != "true|true" {
		t.Fatalf("spy = %q", got)
	}
	// The host gets the action and its args and nothing that names a buffer: it runs the
	// action on its active document, and the runtime never switches documents on its own.
	want := []plugintest.DoCall{{Action: "undo", Count: 1}, {Action: "move_down", Count: 1, Group: 1}}
	if got := host.Cmd().Calls; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
	if host.ActiveBuffer() != 1 {
		t.Fatal("running an action changed the active buffer")
	}
}

func TestCommandContextFields(t *testing.T) {
	// ctx.source, ctx.args, ctx.count, ctx.buf as the commands see them.
	host := cmdHost()
	host.AddBuffer("a.go", "x")
	h := start(t, gv+`
gvim.command.register{ name = "c", run = function(ctx)
  T.spy(ctx.source, tostring(ctx.buf), ctx.count, ctx.args.k)
end }`, plugintest.Options{Host: host})
	if err := h.RT.Run("t.c", map[string]any{"k": "v", "count": 4}, pluginapi.SrcKey); err != nil {
		t.Fatal(err)
	}
	h.Settle()
	if got := spyText(h); got != "key Buffer(1) 4 v" {
		t.Fatalf("spy = %q", got)
	}
}

// A command that raises is reported, and a command that registers more commands while it
// runs republishes the set.
func TestCommandRegisteredFromACommand(t *testing.T) {
	h := start(t, gv+`
gvim.command.register{ name = "make", run = function()
  gvim.command.register{ name = "made", keys = { "ctrl+m" }, run = function() T.spy("made ran") end }
end }`)
	if len(h.Host.Commands["t"]) != 1 {
		t.Fatalf("commands = %+v", h.Host.Commands["t"])
	}
	_ = h.Run("t.make", nil)
	if len(h.Host.Commands["t"]) != 2 || len(h.Host.Bindings["t"]) != 1 {
		t.Fatalf("commands = %+v bindings = %+v", h.Host.Commands["t"], h.Host.Bindings["t"])
	}
	_ = h.Run("t.made", nil)
	if got := spyText(h); got != "made ran" {
		t.Fatalf("spy = %q", got)
	}
}
