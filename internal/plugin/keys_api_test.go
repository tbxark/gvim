package plugin_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// keysRows runs each row as its own plugin and compares the spy line.
func keysRows(t *testing.T, rows []struct{ name, lua, want string }) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, gv+r.lua)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestKeymapSet(t *testing.T) {
	t.Run("a string action in the normal context", func(t *testing.T) {
		h := start(t, gv+`
local b = gvim.keymap.set("ctrl+alt+j", "move_down", { args = { n = 2 }, when = { "has_buffer", "!read_only" } })
T.spy(tostring(b), b:active())`)
		want := []pluginapi.Binding{{
			Chord: "ctrl+alt+j", Action: "move_down", Plugin: "t",
			Args: map[string]any{"n": int64(2)}, When: []string{"has_buffer", "!read_only"},
		}}
		if got := h.Host.Bindings["t"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("bindings = %+v, want %+v", got, want)
		}
		if got := spyText(h); got != "Binding(ctrl+alt+j) true" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("a function becomes a hidden command that runs it", func(t *testing.T) {
		h := start(t, gv+`gvim.keymap.set("ctrl+f", function(ctx) T.spy("ran", ctx.source) end, { desc = "Find things" })`)
		bs := h.Host.Bindings["t"]
		if len(bs) != 1 || !strings.HasPrefix(bs[0].Action, "t.__fn") {
			t.Fatalf("bindings = %+v", bs)
		}
		var info pluginapi.CommandInfo
		for _, c := range h.Host.Commands["t"] {
			if c.ID == bs[0].Action {
				info = c
			}
		}
		if !info.Hidden || info.Title != "Find things" {
			t.Fatalf("command = %+v, want hidden with the desc as title", info)
		}
		if err := h.Run(bs[0].Action, nil); err != nil {
			t.Fatal(err)
		}
		if got := spyText(h); got != "ran api" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("binding a chord again replaces it and drops the hidden command", func(t *testing.T) {
		h := start(t, gv+`
local a = gvim.keymap.set("ctrl+f", function() end)
local b = gvim.keymap.set("Ctrl+F", "save")
T.spy(a:active(), b:active(), select(2, a:del()) ~= nil)`)
		if got := spyText(h); got != "false true true" {
			t.Fatalf("spy = %q (an old handle must not delete the new binding)", got)
		}
		want := []pluginapi.Binding{{Chord: "Ctrl+F", Action: "save", Plugin: "t"}}
		if got := h.Host.Bindings["t"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("bindings = %+v, want %+v", got, want)
		}
		if n := len(h.Host.Commands["t"]); n != 0 {
			t.Fatalf("%d commands left: %+v", n, h.Host.Commands["t"])
		}
	})

	t.Run("del by chord and by handle", func(t *testing.T) {
		h := start(t, gv+`
local b = gvim.keymap.set("ctrl+f", "save")
gvim.keymap.set("ctrl+g", function() end)
T.spy(gvim.keymap.del("ctrl+g"))
T.spy(gvim.keymap.del("ctrl+g"))
T.spy(b:del(), b:active())`)
		if got := spyText(h); got != "true|true|true false" {
			t.Fatalf("spy = %q", got)
		}
		if len(h.Host.Bindings["t"]) != 0 || len(h.Host.Commands["t"]) != 0 {
			t.Fatalf("left over: %+v %+v", h.Host.Bindings["t"], h.Host.Commands["t"])
		}
	})

	t.Run("label asks the host", func(t *testing.T) {
		host := plugintest.NewFakeHost()
		host.Cmd().Labels["save"] = "Ctrl+S"
		hh := plugintest.New(t, plugintest.Files{"t.lua": gv + `T.spy(gvim.keymap.label("save"), gvim.keymap.label("nope"))`}, plugintest.Options{Host: host})
		hh.Start()
		if got := spyText(hh); got != "Ctrl+S nil" {
			t.Fatalf("spy = %q", got)
		}
	})

	keysRows(t, []struct{ name, lua, want string }{
		{"a sequence needs a mode", `T.spy(gvim.err_code(select(2, gvim.keymap.set("g g", "save"))))`, "invalid"},
		{"an empty chord is invalid", `T.spy(gvim.err_code(select(2, gvim.keymap.set("  ", "save"))))`, "invalid"},
		{"an undefined mode is not found", `T.spy(gvim.err_code(select(2, gvim.keymap.set("a", "save", { mode = "nope" }))))`, "not_found"},
		{"a bad mode name is invalid", `T.spy(gvim.err_code(select(2, gvim.keymap.set("a", "save", { mode = "No Way" }))))`, "invalid"},
		{"a bad when is invalid", `T.spy(gvim.err_code(select(2, gvim.keymap.set("a", "save", { when = { "no way" } }))))`, "invalid"},
		{"a wrong action type raises", `T.spy((pcall(gvim.keymap.set, "a", 5)))`, "false"},
		{"a wrong option type raises", `T.spy((pcall(gvim.keymap.set, "a", "save", { args = 5 })))`, "false"},
		{"del of an unbound chord is true (FINAL 5.4)", `T.spy(gvim.keymap.del("ctrl+q"))`, "true"},
		{"del in an undefined mode is not found", `T.spy(gvim.err_code(select(2, gvim.keymap.del("ctrl+q", { mode = "nope" }))))`, "not_found"},
	})
}

// A chord that names no key is "invalid" where the plugin can see it, from the keymap's own
// verdict (Services.ValidateChord), for set, del, mode bindings and command keys (S14-2).
func TestKeymapRejectsUnknownKeys(t *testing.T) {
	keysRows(t, []struct{ name, lua, want string }{
		{"set", `T.spy(gvim.err_code(select(2, gvim.keymap.set("ctrl+bogus", "save"))))`, "invalid"},
		{"set returns nil and a message", `local b, e = gvim.keymap.set("ctrl+bogus", "save")
T.spy(b, e)`, `nil invalid: keymap: "ctrl+bogus": unknown key name "bogus"`},
		{"a bad modifier", `T.spy(gvim.err_code(select(2, gvim.keymap.set("hyper+a", "save"))))`, "invalid"},
		{"a bad step of a sequence", `gvim.mode.define{ name = "m" }
T.spy(gvim.err_code(select(2, gvim.keymap.set("g bogus", "save", { mode = "m" }))))`, "invalid"},
		{"set in a mode", `gvim.mode.define{ name = "m" }
T.spy(gvim.err_code(select(2, gvim.keymap.set("ctrl+bogus", "save", { mode = "m" }))))`, "invalid"},
		{"del", `T.spy(gvim.err_code(select(2, gvim.keymap.del("ctrl+bogus"))))`, "invalid"},
		{"mode.define bindings", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "m", bindings = { ["ctrl+bogus"] = "x" } })))`, "invalid"},
		{"command keys", `T.spy(gvim.err_code(select(2, gvim.command.register{ name = "c", keys = { "ctrl+bogus" }, run = function() end })))`, "invalid"},
		{"real chords stay valid", `T.spy(gvim.keymap.set("ctrl+shift+f", "save") ~= nil, gvim.keymap.set("F10", "save") ~= nil, gvim.keymap.set("alt+pagedown", "save") ~= nil, gvim.keymap.set("ctrl++", "save") ~= nil)`, "true true true true"},
	})
	h := start(t, gv+`gvim.keymap.set("ctrl+bogus", "save")`)
	if len(h.Host.Bindings["t"]) != 0 {
		t.Fatalf("a rejected chord reached the host: %+v", h.Host.Bindings["t"])
	}
}

func TestKeymapModeBindings(t *testing.T) {
	h := start(t, gv+`
gvim.mode.define{ name = "nav", bindings = { j = "move_down" } }
gvim.keymap.set("k", "move_up", { mode = "nav", args = { n = 3 } })
gvim.keymap.set("g g", function() T.spy("top") end, { mode = "nav" })
T.spy(select(2, gvim.keymap.set("x", "save", { mode = "nav", when = { "has_buffer" } })) ~= nil)
T.spy(gvim.keymap.del("j", { mode = "nav" }))`)
	if len(h.Host.Bindings["t"]) != 0 {
		t.Fatalf("a mode binding reached the keymap: %+v", h.Host.Bindings["t"])
	}
	ms := h.Host.Modes["t"]
	if len(ms) != 1 {
		t.Fatalf("modes = %+v", ms)
	}
	m := ms[0]
	if m.Name != "nav" || m.Plugin != "t" || m.Bindings["k"] != "move_up" || m.Bindings["j"] != "" {
		t.Fatalf("mode = %+v", m)
	}
	if !reflect.DeepEqual(m.Args["k"], map[string]any{"n": int64(3)}) {
		t.Fatalf("args = %+v", m.Args)
	}
	if a := m.Bindings["g g"]; !strings.HasPrefix(a, "t.__fn") {
		t.Fatalf("g g -> %q, want a hidden command", a)
	}
	if got := spyText(h); got != "true|true" {
		t.Fatalf("spy = %q", got)
	}
	if err := h.Run(m.Bindings["g g"], nil); err != nil {
		t.Fatal(err)
	}
	if got := spy(h); got[len(got)-1] != "top" {
		t.Fatalf("spy = %q", got)
	}
}

func TestModeDefine(t *testing.T) {
	t.Run("everything the spec says", func(t *testing.T) {
		h := start(t, gv+`
local m = gvim.mode.define{
  name = "normal",
  bindings = { h = "move_left", ["Ctrl+D"] = function() end, ["g g"] = "goto_top" },
  text = function(ctx) end,
  paste = "text",
  fallthrough = false,
  status = "-- NORMAL --",
}
T.spy(tostring(m), m:name())`)
		if got := spyText(h); got != "Mode(normal) normal" {
			t.Fatalf("spy = %q", got)
		}
		ms := h.Host.Modes["t"]
		if len(ms) != 1 {
			t.Fatalf("modes = %+v", ms)
		}
		m := ms[0]
		if m.Name != "normal" || m.Plugin != "t" || m.Fallthrough || m.Status != "-- NORMAL --" ||
			m.Text != pluginapi.TextHandler || m.Paste != pluginapi.TextHandler || !strings.HasPrefix(m.TextAction, "t.__fn") {
			t.Fatalf("mode = %+v", m)
		}
		if m.Bindings["h"] != "move_left" || m.Bindings["g g"] != "goto_top" || !strings.HasPrefix(m.Bindings["ctrl+d"], "t.__fn") || len(m.Bindings) != 3 {
			t.Fatalf("bindings = %+v", m.Bindings)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		h := start(t, gv+`gvim.mode.define{ name = "plain" }`)
		m := h.Host.Modes["t"][0]
		if m.Text != pluginapi.TextPass || m.Paste != pluginapi.TextPass || !m.Fallthrough || m.Status != "" || len(m.Bindings) != 0 || m.TextAction != "" {
			t.Fatalf("mode = %+v", m)
		}
	})

	t.Run("uses_esc", func(t *testing.T) {
		h := start(t, gv+`gvim.mode.define{ name = "own", bindings = { escape = function() end } } gvim.mode.define{ name = "plain" }`)
		ms := h.Host.Modes["t"]
		if len(ms) != 2 || ms[0].Bindings["escape"] == "" || ms[1].Bindings["escape"] != "" {
			t.Fatalf("modes = %+v", ms)
		}
	})

	t.Run("swallow text and paste", func(t *testing.T) {
		h := start(t, gv+`gvim.mode.define{ name = "ro", text = "swallow", paste = "swallow" }`)
		m := h.Host.Modes["t"][0]
		if m.Text != pluginapi.TextSwallow || m.Paste != pluginapi.TextSwallow {
			t.Fatalf("mode = %+v", m)
		}
	})

	t.Run("a long status label is cut to its cells", func(t *testing.T) {
		h := start(t, gv+`gvim.mode.define{ name = "long", status = ("x"):rep(500) }`)
		if n := len([]rune(h.Host.Modes["t"][0].Status)); n != 80 {
			t.Fatalf("status has %d runes, want 80", n)
		}
	})

	keysRows(t, []struct{ name, lua, want string }{
		{"a name is required", `T.spy((pcall(gvim.mode.define, {})))`, "false"},
		{"a bad name is invalid", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "No Way" })))`, "invalid"},
		{"a duplicate is a conflict", `gvim.mode.define{ name = "a" } T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a" })))`, "conflict"},
		{"a bad text value is invalid", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a", text = "eat" })))`, "invalid"},
		{"a wrong text type raises", `T.spy((pcall(gvim.mode.define, { name = "a", text = 5 })))`, "false"},
		{"paste text needs a text function", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a", paste = "text" })))`, "invalid"},
		{"a bad paste value is invalid", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a", paste = "yes" })))`, "invalid"},
		{"a binding of the wrong type raises", `T.spy((pcall(gvim.mode.define, { name = "a", bindings = { a = 5 } })))`, "false"},
		{"two spellings of one chord are invalid", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a", bindings = { a = "x", A = "y" } })))`, "invalid"},
		{"an invalid chord is invalid", `T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a", bindings = { ["a b c d e"] = "x" } })))`, "invalid"},
		{"too many bindings is a limit", `
local function ch(n) return string.char(97 + n % 26) .. " " .. string.char(97 + math.floor(n / 26) % 26) .. " " .. string.char(97 + math.floor(n / 676) % 26) end
local b = {}
for i = 1, 1025 do b[ch(i)] = "x" end
T.spy(gvim.err_code(select(2, gvim.mode.define{ name = "a", bindings = b })))`, "limit"},
	})

	t.Run("a failed define registers no hidden command", func(t *testing.T) {
		h := start(t, gv+`
local ok = gvim.mode.define{ name = "a", bindings = { x = function() end, ["a b c d e"] = "y" } }
T.spy(ok)`)
		if n := len(h.Host.Commands["t"]); n != 0 || len(h.Host.Modes["t"]) != 0 {
			t.Fatalf("left over: %+v %+v", h.Host.Commands["t"], h.Host.Modes["t"])
		}
	})

	t.Run("the mode quota", func(t *testing.T) {
		h := start(t, gv+`
local codes = {}
for i = 1, 33 do
  local m, err = gvim.mode.define{ name = "m" .. i }
  if not m then codes[#codes + 1] = gvim.err_code(err) end
end
T.spy(table.concat(codes, ","))`)
		if got := spyText(h); got != "limit" {
			t.Fatalf("spy = %q", got)
		}
	})
}

func TestModeEnterLeaveCurrent(t *testing.T) {
	t.Run("enter, current and leave", func(t *testing.T) {
		h := start(t, gv+`
local m = gvim.mode.define{ name = "normal" }
gvim.mode.define{ name = "other" }
T.spy(gvim.mode.current())
T.spy(gvim.mode.enter("normal"))
T.spy(gvim.mode.current())
T.spy(m:enter())
T.spy(gvim.mode.enter("other"))
T.spy(gvim.mode.current())
T.spy(gvim.mode.leave())
T.spy(gvim.mode.current())`)
		want := "nil|true|normal t|true|true|other t|true|nil"
		if got := spyText(h); got != want {
			t.Fatalf("spy = %q, want %q", got, want)
		}
		var modes []string
		for _, c := range h.Host.Calls() {
			if c.Method == "SetInputMode" {
				modes = append(modes, c.Args[1].(string))
			}
		}
		if !slices.Equal(modes, []string{"normal", "normal", "other", ""}) {
			t.Fatalf("SetInputMode calls = %q", modes)
		}
	})

	t.Run("an undefined mode is not found", func(t *testing.T) {
		h := start(t, gv+`T.spy(gvim.err_code(select(2, gvim.mode.enter("nope"))))`)
		if got := spyText(h); got != "not_found" {
			t.Fatalf("spy = %q", got)
		}
		if h.Host.CallCount("SetInputMode") != 0 {
			t.Fatal("the host was asked to enter a mode that is not defined")
		}
	})

	t.Run("current follows the host when it clears the mode", func(t *testing.T) {
		h := start(t, gv+`
gvim.mode.define{ name = "normal" }
gvim.mode.enter("normal")
gvim.command.register{ name = "ask", run = function() T.spy(gvim.mode.current()) end }`)
		h.Host.InputMode["t"] = "" // the panic chord cleared it
		if err := h.Run("t.ask", nil); err != nil {
			t.Fatal(err)
		}
		if got := spyText(h); got != "nil" {
			t.Fatalf("spy = %q: the runtime kept its own copy of the mode", got)
		}
	})

	t.Run("a suspended plugin cannot enter a mode", func(t *testing.T) {
		h := start(t, gv+`
gvim.mode.define{ name = "normal" }
gvim.command.register{ name = "enter", run = function() T.spy(gvim.err_code(select(2, gvim.mode.enter("normal")))) end }
gvim.command.register{ name = "boom", run = function() while true do end end }`, plugintest.Options{Timeout: 20_000_000})
		for range 3 {
			_ = h.Run("t.boom", nil)
		}
		if status(h, "t") != "suspended" {
			t.Skipf("the plugin is %q, not suspended", status(h, "t"))
		}
		if err := h.Run("t.enter", nil); err != nil {
			t.Fatal(err)
		}
		if got := spy(h); got[len(got)-1] != "closed" {
			t.Fatalf("spy = %q", got)
		}
	})
}

func TestContext(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		h := start(t, gv+`
T.spy(gvim.context.get("active"))
T.spy(gvim.context.set("active", true), gvim.context.get("active"))
T.spy(gvim.context.set("active", false), gvim.context.get("active"))`)
		if got := spyText(h); got != "false|true true|true false" {
			t.Fatalf("spy = %q", got)
		}
		var calls []string
		for _, c := range h.Host.Calls() {
			if c.Method == "SetContext" {
				calls = append(calls, c.String())
			}
		}
		if want := []string{"SetContext[t active true]", "SetContext[t active false]"}; !slices.Equal(calls, want) {
			t.Fatalf("calls = %q, want %q", calls, want)
		}
	})

	t.Run("get falls back to the host", func(t *testing.T) {
		host := plugintest.NewFakeHost()
		host.HoldSet["read_only"] = true
		h := plugintest.New(t, plugintest.Files{"t.lua": gv + `T.spy(gvim.context.get("read_only"), gvim.context.get("modified"))`}, plugintest.Options{Host: host})
		h.Start()
		if got := spyText(h); got != "true false" {
			t.Fatalf("spy = %q", got)
		}
	})

	keysRows(t, []struct{ name, lua, want string }{
		{"a bad name is invalid", `T.spy(gvim.err_code(select(2, gvim.context.set("no way", true))))`, "invalid"},
		{"the host's names are reserved", `T.spy(gvim.err_code(select(2, gvim.context.set("mode:t:normal", true))))`, "invalid"},
		{"a negated name is invalid", `T.spy(gvim.err_code(select(2, gvim.context.set("!x", true))))`, "invalid"},
		{"a wrong type raises", `T.spy((pcall(gvim.context.set, "x", "yes")))`, "false"},
		{"the quota", `
local n
for i = 1, 65 do
  local ok, err = gvim.context.set("c" .. i, true)
  if not ok then n = gvim.err_code(err) end
end
T.spy(n)`, "limit"},
	})
}

func TestNextKey(t *testing.T) {
	press := func(h *plugintest.Harness, k pluginapi.Key) { h.PressKey(k) }

	t.Run("the next key press ends the wait", func(t *testing.T) {
		h := start(t, gv+`
gvim.command.register{ name = "ask", run = function()
  local k, err = gvim.input.next_key()
  T.spy(k.key, k.text, k.ctrl, k.alt, k.shift, k.super)
  local e = gvim.input.next_key()
  T.spy(e.key, e.text)
end }`)
		if err := h.Run("t.ask", nil); err != nil {
			t.Fatal(err)
		}
		w := h.Host.Keys().Pending
		if w == nil || w.TimeoutMS != 10000 {
			t.Fatalf("pending = %+v, want the default timeout 10000", w)
		}
		press(h, pluginapi.Key{Key: "a", Text: "A", Shift: true, Ctrl: true})
		press(h, pluginapi.Key{Key: "escape"})
		want := "a A true false true false|escape nil"
		if got := spyText(h); got != want {
			t.Fatalf("spy = %q, want %q", got, want)
		}
	})

	t.Run("the timeout option", func(t *testing.T) {
		h := start(t, gv+`gvim.command.register{ name = "ask", run = function() gvim.input.next_key{ timeout_ms = 250 } end }`)
		_ = h.Run("t.ask", nil)
		if w := h.Host.Keys().Pending; w == nil || w.TimeoutMS != 250 {
			t.Fatalf("pending = %+v", w)
		}
	})

	t.Run("a timeout and a cancel come back as errors", func(t *testing.T) {
		for _, tt := range []struct{ err, code string }{{"timeout: no key", "timeout"}, {"cancelled: plugin input cleared", "cancelled"}} {
			h := start(t, gv+`gvim.command.register{ name = "ask", run = function()
  local k, err = gvim.input.next_key()
  T.spy(k, gvim.err_code(err))
end }`)
			_ = h.Run("t.ask", nil)
			h.FailKeyWait(tt.err)
			if got := spyText(h); got != "nil "+tt.code {
				t.Fatalf("%s: spy = %q", tt.err, got)
			}
		}
	})

	t.Run("a second wait is busy", func(t *testing.T) {
		h := start(t, gv+`
gvim.command.register{ name = "ask", run = function()
  local k, err = gvim.input.next_key()
  T.spy(k and k.key, gvim.err_code(err))
end }`)
		_ = h.Run("t.ask", nil)
		_ = h.Run("t.ask", nil)
		if got := spyText(h); got != "nil busy" {
			t.Fatalf("spy = %q", got)
		}
		press(h, pluginapi.Key{Key: "x", Text: "x"})
		if got := spyText(h); got != "nil busy|x nil" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("a bad option raises", func(t *testing.T) {
		for _, opts := range []string{`{ timeout_ms = 60001 }`, `{ timeout_ms = 0 }`, `{ timeout_ms = "soon" }`, `5`} {
			h := start(t, gv+`gvim.command.register{ name = "ask", run = function() gvim.input.next_key(`+opts+`) end }`)
			_ = h.Run("t.ask", nil)
			if h.Host.Keys().Pending != nil {
				t.Errorf("%s: the host was asked to wait", opts)
			}
			if !logHas(h, "t", "bad argument") {
				t.Errorf("%s: log %v has no bad argument error", opts, h.LogText("t"))
			}
		}
	})

	t.Run("unloading the plugin cancels the capture", func(t *testing.T) {
		h := start(t, gv+`gvim.command.register{ name = "ask", run = function() gvim.input.next_key() end }`)
		base := h.Host.Resources()
		_ = h.Run("t.ask", nil)
		if h.Host.Keys().Pending == nil {
			t.Fatal("no capture pending")
		}
		h.Unload("t")
		if h.Host.Keys().Pending != nil || len(h.Host.Keys().Cancelled) != 1 {
			t.Fatalf("pending %+v, cancelled %v", h.Host.Keys().Pending, h.Host.Keys().Cancelled)
		}
		if got := h.Host.Resources(); len(got) != 0 && !reflect.DeepEqual(got, base) {
			t.Fatalf("resources = %v", got)
		}
	})

	t.Run("a host that refuses reports it", func(t *testing.T) {
		host := plugintest.NewFakeHost()
		host.Keys().Err = pluginapi.Errf(pluginapi.CodeUnsupported, "no keys here")
		h := plugintest.New(t, plugintest.Files{"t.lua": gv + `
gvim.command.register{ name = "ask", run = function()
  local k, err = gvim.input.next_key()
  T.spy(k, gvim.err_code(err))
end }`}, plugintest.Options{Host: host})
		h.Start()
		_ = h.Run("t.ask", nil)
		if got := spyText(h); got != "nil unsupported" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("has input", func(t *testing.T) {
		h := start(t, gv+`T.spy(gvim.has("input"))`)
		if got := spyText(h); got != "true" {
			t.Fatalf("spy = %q", got)
		}
	})
}

func TestStatusItems(t *testing.T) {
	last := func(h *plugintest.Harness) []pluginapi.StatusItem { return h.Host.Status["t"] }

	t.Run("create, set, hide, show, remove", func(t *testing.T) {
		h := start(t, gv+`
local it = gvim.ui.statusbar.item{ id = "clock", text = "12:00", align = "right", priority = 5 }
T.spy(tostring(it))
gvim.command.register{ name = "set", run = function() T.spy(it:set("12:01")) end }
gvim.command.register{ name = "hide", run = function() T.spy(it:hide()) end }
gvim.command.register{ name = "show", run = function() T.spy(it:show()) end }
gvim.command.register{ name = "remove", run = function() T.spy(it:remove(), select(2, it:set("x")) ~= nil) end }`)
		want := []pluginapi.StatusItem{{ID: "clock", Plugin: "t", Text: "12:00", Right: true, Priority: 5}}
		if got := last(h); !reflect.DeepEqual(got, want) {
			t.Fatalf("items = %+v, want %+v", got, want)
		}
		step := func(cmd string) {
			t.Helper()
			if err := h.Run("t."+cmd, nil); err != nil {
				t.Fatal(err)
			}
		}
		step("set")
		if got := last(h); len(got) != 1 || got[0].Text != "12:01" {
			t.Fatalf("after set: %+v", got)
		}
		step("hide")
		if got := last(h); len(got) != 0 {
			t.Fatalf("after hide: %+v", got)
		}
		step("show")
		if got := last(h); len(got) != 1 {
			t.Fatalf("after show: %+v", got)
		}
		step("remove")
		if got := last(h); len(got) != 0 {
			t.Fatalf("after remove: %+v", got)
		}
		if got := spyText(h); got != "StatusItem(clock)|true|true|true|true true" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("defaults and generated ids", func(t *testing.T) {
		h := start(t, gv+`gvim.ui.statusbar.item{ text = "a" } gvim.ui.statusbar.item{ text = "b" }`)
		got := last(h)
		if len(got) != 2 || got[0].ID == got[1].ID || got[0].Right || got[0].Priority != 0 || got[0].Clickable {
			t.Fatalf("items = %+v", got)
		}
	})

	t.Run("unchanged text publishes nothing", func(t *testing.T) {
		h := start(t, gv+`local it = gvim.ui.statusbar.item{ id = "a", text = "x" } it:set("x") it:set("x") it:hide() it:hide()`)
		if n := h.Host.CallCount("SetStatusItems"); n != 2 {
			t.Fatalf("SetStatusItems called %d times, want 2 (create, hide)", n)
		}
	})

	t.Run("text is one line of at most 80 cells", func(t *testing.T) {
		h := start(t, gv+`gvim.ui.statusbar.item{ id = "a", text = "one\ntwo" .. ("x"):rep(300) }`)
		got := last(h)[0].Text
		if strings.Contains(got, "\n") || len([]rune(got)) != 80 {
			t.Fatalf("text = %q (%d runes)", got, len([]rune(got)))
		}
	})

	t.Run("on_click is a hidden command named by the id", func(t *testing.T) {
		h := start(t, gv+`gvim.ui.statusbar.item{ id = "clock", text = "x", on_click = function(ctx) T.spy("clicked", ctx.source) end }`)
		if got := last(h); len(got) != 1 || !got[0].Clickable {
			t.Fatalf("items = %+v", got)
		}
		action := pluginapi.StatusClickAction("t", "clock")
		if !h.RT.Owns(action) {
			t.Fatalf("Owns(%q) = false", action)
		}
		var info pluginapi.CommandInfo
		for _, c := range h.Host.Commands["t"] {
			if c.ID == action {
				info = c
			}
		}
		if !info.Hidden || info.Record {
			t.Fatalf("command = %+v: want hidden and not recorded", info)
		}
		if err := h.Run(action, nil); err != nil {
			t.Fatal(err)
		}
		if got := spyText(h); got != "clicked api" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("removing an item removes its click command", func(t *testing.T) {
		h := start(t, gv+`gvim.ui.statusbar.item{ id = "clock", on_click = function() end }:remove()`)
		if len(h.Host.Commands["t"]) != 0 || h.RT.Owns(pluginapi.StatusClickAction("t", "clock")) {
			t.Fatalf("commands = %+v", h.Host.Commands["t"])
		}
	})

	keysRows(t, []struct{ name, lua, want string }{
		{"a bad id is invalid", `T.spy(gvim.err_code(select(2, gvim.ui.statusbar.item{ id = "No Way" })))`, "invalid"},
		{"a duplicate id is a conflict", `gvim.ui.statusbar.item{ id = "a" } T.spy(gvim.err_code(select(2, gvim.ui.statusbar.item{ id = "a" })))`, "conflict"},
		{"a bad align is invalid", `T.spy(gvim.err_code(select(2, gvim.ui.statusbar.item{ id = "a", align = "middle" })))`, "invalid"},
		{"a wrong field type raises", `T.spy((pcall(gvim.ui.statusbar.item, { id = "a", text = 5 })))`, "false"},
		{"a wrong on_click type raises", `T.spy((pcall(gvim.ui.statusbar.item, { id = "a", on_click = "x" })))`, "false"},
		{"the quota is eight", `
local code
for i = 1, 9 do
  local it, err = gvim.ui.statusbar.item{ id = "i" .. i }
  if not it then code = gvim.err_code(err) end
end
T.spy(code)`, "limit"},
	})
}

// TestKeysLeaveNothingBehind: everything the key APIs register goes with the plugin.
func TestKeysLeaveNothingBehind(t *testing.T) {
	h := start(t, gv+`T.spy("base")`)
	base := h.Host.Resources()
	if err := h.Load("greedy", gv+`
gvim.keymap.set("ctrl+1", function() end)
gvim.keymap.set("ctrl+2", "save")
gvim.mode.define{ name = "m", bindings = { a = function() end }, text = function() end }
gvim.mode.enter("m")
gvim.context.set("on", true)
gvim.ui.statusbar.item{ id = "s", text = "x", on_click = function() end }
gvim.command.register{ name = "ask", run = function() gvim.input.next_key() end }`); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("greedy.ask", nil); err != nil {
		t.Fatal(err)
	}
	res := h.Host.Resources()
	for _, want := range []string{"binding:greedy/ctrl+1", "binding:greedy/ctrl+2", "mode:greedy/m", "context:greedy/on", "status:greedy/s", "input_mode:greedy/m"} {
		if !slices.Contains(res, want) {
			t.Errorf("resource %q missing from %v", want, res)
		}
	}
	if h.Host.Keys().Pending == nil {
		t.Fatal("no key wait pending")
	}
	h.Unload("greedy")
	if got := h.Host.Resources(); !slices.Equal(got, base) {
		t.Fatalf("resources after unload = %v, want %v", got, base)
	}
}
