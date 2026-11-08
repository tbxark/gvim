package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// reqPlugin builds a directory plugin "p" with the given files; init.lua is main.
func reqPlugin(t *testing.T, files map[string]string) *plugintest.Harness {
	t.Helper()
	f := plugintest.Files{}
	for name, src := range files {
		f["p/"+name] = src
	}
	return startFiles(t, f)
}

func TestRequire(t *testing.T) {
	rows := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"lua/ module", map[string]string{
			"init.lua": `local u = require "util" T.spy(u.name)`, "lua/util.lua": `return {name = "util"}`}, "util"},
		{"lua/a/b.lua", map[string]string{
			"init.lua": `T.spy(require("a.b").v)`, "lua/a/b.lua": `return {v = 7}`}, "7"},
		{"lua/a/b/init.lua", map[string]string{
			"init.lua": `T.spy(require("a.b").v)`, "lua/a/b/init.lua": `return {v = 8}`}, "8"},
		{"a/b.lua beside the entry", map[string]string{
			"init.lua": `T.spy(require("a.b").v)`, "a/b.lua": `return {v = 9}`}, "9"},
		{"lua/ wins over the plain path", map[string]string{
			"init.lua": `T.spy(require("m").where)`, "lua/m.lua": `return {where = "lua"}`, "m.lua": `return {where = "root"}`}, "lua"},
		{"cached", map[string]string{
			"init.lua": `T.spy(require("c") == require("c"), require("c").n)`, "lua/c.lua": `T.spy("loading") return {n = 1}`}, "loading|true 1"},
		{"cycle returns true", map[string]string{
			"init.lua": `T.spy(require("a"))`, "lua/a.lua": `local b = require("b") return "a:" .. tostring(b)`,
			"lua/b.lua": `T.spy("b sees", require("a")) return "b"`}, "b sees true|a:b"},
		{"module without a return value is true", map[string]string{
			"init.lua": `T.spy(require("n"))`, "lua/n.lua": `x = 1`}, "true"},
		{"module shares the plugin globals", map[string]string{
			"init.lua": `shared = "g" T.spy(require("s"))`, "lua/s.lua": `return shared .. "!"`}, "g!"},
		{"module may await while loading", map[string]string{
			"init.lua": `T.spy(require("w"))`, "lua/w.lua": `return T.echo("awaited")`}, "awaited"},
		{"module error propagates and is not cached", map[string]string{
			"init.lua": `local ok, e = pcall(require, "bad") T.spy(ok, e:match("boom"))
local ok2 = pcall(require, "bad") T.spy(ok2)`, "lua/bad.lua": `error("boom")`}, "false boom|false"},
		{"module syntax error", map[string]string{
			"init.lua": `local ok, e = pcall(require, "syn") T.spy(ok, e:match("failed to compile") ~= nil)`, "lua/syn.lua": `local = =`}, "false true"},
		{"not found", map[string]string{
			"init.lua": `local ok, e = pcall(require, "nothere") T.spy(ok, e:match("not found") ~= nil)`}, "false true"},
		{"host modules", map[string]string{
			"init.lua": `local gvim = require "gvim"
T.spy(require("gvim") == gvim, require("gvim.async") == gvim.async, gvim.async ~= nil)`}, "true true true"},
		{"gvim cannot be overridden", map[string]string{
			"init.lua": `T.spy(require("gvim").api_version)`, "lua/gvim.lua": `return {api_version = "fake"}`}, "2"},
		{"unknown gvim area", map[string]string{
			"init.lua": `T.spy((pcall(require, "gvim.nothing")))`}, "false"},
		{"areas of the stubs exist", map[string]string{
			"init.lua": `local gvim = require "gvim"
T.spy(type(require "gvim.fs"), type(gvim.buf), type(gvim.command), type(gvim.ui), type(gvim.timer))`}, "table table table table table"},
		{"no cross-plugin require", map[string]string{
			"init.lua": `T.spy((pcall(require, "q.init")))`}, "false"},
		{"depth limit", func() map[string]string {
			m := map[string]string{"init.lua": `local ok, e = pcall(require, "d1") T.spy(ok, e:match("nesting too deep") ~= nil)`}
			for i := 1; i <= 20; i++ {
				m["lua/d"+itoa(i)+".lua"] = `return require("d` + itoa(i+1) + `")`
			}
			m["lua/d21.lua"] = `return 1`
			return m
		}(), "false true"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := reqPlugin(t, r.files)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v\nstatus: %q", got, r.want, h.LogText(""), status(h, "p"))
			}
		})
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func TestRequireNameValidation(t *testing.T) {
	for _, name := range []string{"", ".a", "a.", "a..b", "a/b", `a\b`, "../x", "/abs", "a b", "a$", strings.Repeat("x", 129)} {
		h := start(t, `local ok, e = pcall(require, `+luaQuote(name)+`) T.spy(ok, tostring(e):match("invalid module name") ~= nil)`)
		if got := spyText(h); got != "false true" {
			t.Errorf("require(%q): %q", name, got)
		}
	}
}

func luaQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func TestRequireDoesNotTouchTheFilesystem(t *testing.T) {
	// Modules come from the compiled file map of the plugin: a file that is not in it does
	// not exist, wherever it lives.
	h := reqPlugin(t, map[string]string{"init.lua": `T.spy((pcall(require, "elsewhere")))`})
	if got := spyText(h); got != "false" {
		t.Fatalf("spy = %q", got)
	}
	if r := plugin.RegistryOf(h.RT); r.Tasks != 0 {
		t.Fatalf("registry = %+v", r)
	}
}
