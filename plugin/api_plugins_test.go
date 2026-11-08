package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
)

const exporterSrc = `
local gvim = require "gvim"
gvim.plugins.export{
  add = function(x, y) return x + y end,
  echo = function(...) return ... end,
  boom = function() error("boom", 0) end,
  name = "alpha",
  info = {n = 1, list = {1, 2}},
  kind = function(buf) return tostring(buf) end,
  handle = function() return T.buf(1) end,
  nested = function() return {T.buf(1)} end,
  slow = function() T.work("x", 30) return "slow" end,
  wait = function() T.host() return "never" end,
  mutate = function(t) t.x = 99 return t end,
  version = function() return "v1" end,
}`

// twoPlugins loads the exporter "a" and a plugin "b" whose command "go" runs body with the
// proxy of a in pa.
func twoPlugins(t *testing.T, body string) *plugintest.Harness {
	t.Helper()
	h := startFiles(t, plugintest.Files{
		"a.lua": exporterSrc,
		"b.lua": `local gvim = require "gvim" T.command("go", function() local pa = gvim.plugins.get("a") ` + body + ` end)`,
	}, plugintest.Options{Timeout: 2 * time.Second})
	if err := h.Run("b.go", nil); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestPluginsProxy(t *testing.T) {
	rows := []row{
		{"call", `T.spy(pa.add(2, 3))`, "5"},
		{"data", `T.spy(pa.name, pa.info.n, #pa.info.list)`, "alpha 1 2"},
		{"data is copied on every access", `local i = pa.info i.n = 9 T.spy(pa.info.n, i.n)`, "1 9"},
		{"several results", `T.spy(pa.echo(1, "x", true))`, "1 x true"},
		{"arguments are copied", `local t = {1, {k = "v"}} local r = pa.echo(t) T.spy(r ~= t, r[2].k, r[2] ~= t[2])`, "true v true"},
		{"the callee cannot change the caller's table", `local t = {x = 1} local r = pa.mutate(t) T.spy(t.x, r.x)`, "1 99"},
		// FINAL 5.11: no userdata crosses between plugins; a buffer travels as its id.
		{"a handle argument is refused", `T.spy(pa.kind(T.buf(7)))`, "~nil invalid: argument #1"},
		{"a nested handle argument is refused", `T.spy(pa.echo({{h = T.buf(7)}}))`, "~nil invalid: argument #1"},
		{"a handle result is refused", `T.spy(pa.handle())`, "~nil invalid: result"},
		{"a nested handle result is refused", `T.spy(pa.nested())`, "~nil invalid: result"},
		{"ids are plain data", `T.spy(pa.kind(7))`, "7"},
		{"a call that awaits", `T.spy(pa.slow())`, "slow"},
		{"error in the exported function", `T.spy(pa.boom())`, "nil plugin 'a': boom"},
		{"functions are not copied", `T.spy(pa.echo(print))`, "~nil invalid: argument #1"},
		{"unknown member", `T.spy(pa.nothing)`, "nil"},
		{"unknown plugin", `T.spy(gvim.plugins.get("zzz"))`, "nil"},
		{"tostring", `T.spy(tostring(pa))`, "Plugin(a)"},
		{"read only", `T.spy((pcall(function() pa.x = 1 end)), getmetatable(pa))`, "false locked"},
		{"list", `local l = gvim.plugins.list() T.spy(#l, l[1].name, l[1].status, l[1].source, l[2].name)`, "2 a loaded user b"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := twoPlugins(t, r.body)
			got := spyText(h)
			if len(r.want) > 0 && r.want[0] == '~' {
				if !strings.Contains(got, r.want[1:]) {
					t.Fatalf("spy = %q, want it to contain %q\nlog: %v", got, r.want[1:], h.LogText(""))
				}
				return
			}
			if got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestPluginsExportValidation(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"functions and data", `T.spy(gvim.plugins.export{f = function() end, d = {1, {x = "y"}}, n = 5, s = "s", b = true})`, "true"},
		{"a handle in data is refused", `T.spy(pcall(gvim.plugins.export, {x = {T.buf(1)}}))`, "~export 'x'"},
		{"a nested function is refused", `T.spy(pcall(gvim.plugins.export, {x = {f = print}}))`, "~export 'x'"},
		{"a non string key is refused", `T.spy(pcall(gvim.plugins.export, {1, 2}))`, "~export keys must be strings"},
		{"exporting again replaces", `gvim.plugins.export{a = function() end} gvim.plugins.export{b = function() end}
			local p = gvim.plugins.get("t") T.spy(type(p.b), type(p.a))`, "function nil"},
	}))
}

func TestProxyResolvesExporterAtCallTime(t *testing.T) {
	// The proxy holds only the plugin name: after the exporter is reloaded the same proxy
	// reaches the new version, and while it is not loaded a call reports not_found.
	h := startFiles(t, plugintest.Files{
		"a.lua": exporterSrc,
		"b.lua": `local gvim = require "gvim"
local pa
T.command("get", function() pa = gvim.plugins.get("a") end)
T.command("call", function() T.spy(pa.version()) end)
T.command("late", function() T.spy(pa.version()) end)`,
	}, plugintest.Options{Timeout: 2 * time.Second})
	for _, c := range []string{"b.get", "b.call"} {
		if err := h.Run(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	h.Unload("a")
	if err := h.Run("b.call", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Load("a", `require("gvim").plugins.export{version = function() return "v2" end}`); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("b.call", nil); err != nil {
		t.Fatal(err)
	}
	want := "v1|nil not_found: plugin 'a' is not loaded|v2"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q; log=%v", got, want, h.LogText(""))
	}
	// the function object fetched earlier is just as good
	if st := status(h, "b"); st != "loaded" {
		t.Fatalf("b is %s", st)
	}
}

func TestProxyCallResolvedOnUnload(t *testing.T) {
	h := startFiles(t, plugintest.Files{
		"a.lua": exporterSrc,
		"b.lua": `local gvim = require "gvim"
T.command("go", function()
  local pa = gvim.plugins.get("a")
  local f = pa.wait
  T.spy("calling")
  T.spy("got", f())
  T.spy("again", pa.add(1, 2))
end)`,
	}, plugintest.Options{Timeout: 2 * time.Second})
	if err := h.Run("b.go", nil); err != nil {
		t.Fatal(err)
	}
	// the exporter's task parks on a host token (the "token:N" line); the caller waits for it
	if sp := spy(h); len(sp) != 2 || sp[0] != "calling" {
		t.Fatalf("spy = %v", sp)
	}
	h.Unload("a")
	sp := spy(h)
	if len(sp) != 5 || sp[3] != "got nil closed: plugin reloaded" || sp[4] != "again nil not_found: plugin 'a' is not loaded" {
		t.Fatalf("spy = %v; log=%v", sp, h.LogText(""))
	}
}

func TestProxyCallDepthLimit(t *testing.T) {
	// a.f calls b.g calls a.f ...: the fifth nested call is refused, and the answer travels
	// back through the whole chain.
	h := startFiles(t, plugintest.Files{
		"a.lua": `local gvim = require "gvim"
gvim.plugins.export{f = function(n)
  local r, e = gvim.plugins.get("b").g(n + 1)
  return r or e
end}`,
		"b.lua": `local gvim = require "gvim"
gvim.plugins.export{g = function(n)
  local r, e = gvim.plugins.get("a").f(n + 1)
  return r or e
end}
T.command("go", function() T.spy(gvim.plugins.get("a").f(0)) end)`,
	}, plugintest.Options{Timeout: 2 * time.Second})
	if err := h.Run("b.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "limit: plugin calls nested deeper than 4" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}
