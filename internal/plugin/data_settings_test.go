package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

const settingsSchema = `
local gvim = require "gvim"
local s = gvim.settings.define{
  depth = {type = "integer", default = 3, minimum = 1, maximum = 10, project = true},
  label = {type = "string", default = "x", project = true},
  mode = {type = "enum", values = {"a", "b"}, default = "a"},
  on = {type = "boolean", default = true},
  ratio = {type = "number", default = 0.5, minimum = 0, maximum = 1},
  names = {type = "string_list", default = {"p", "q"}},
  cmd = {type = "string", default = "safe"},
}
T.command("show", function()
  T.spy(s.depth, s.label, s.mode, tostring(s.on), s.ratio, table.concat(s.names, ","), s.cmd)
end)
`

// settingsHarness loads the schema plugin "t" with the given config layers.
func settingsHarness(t *testing.T, user, project map[string]any) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, plugintest.Files{"t.lua": settingsSchema}, plugintest.Options{Timeout: 100 * time.Millisecond})
	h.Sources[0].Settings, h.Sources[0].ProjectSettings = user, project
	h.Start()
	return h
}

func show(t *testing.T, h *plugintest.Harness) string {
	t.Helper()
	before := len(spy(h))
	if err := h.Run("t.show", nil); err != nil {
		t.Fatal(err)
	}
	sp := spy(h)
	if len(sp) != before+1 {
		t.Fatalf("show reported %v; log=%v", sp[before:], h.LogText(""))
	}
	return sp[before]
}

func TestSettingsDefineAndUserLayer(t *testing.T) {
	rows := []struct {
		name string
		user map[string]any
		want string
		warn string // contained in some log line of the plugin, "" for none
	}{
		{"defaults", nil, "3 x a true 0.5 p,q safe", ""},
		{"user overrides", map[string]any{"depth": 7.0, "label": "u", "mode": "b", "on": false, "ratio": 0.25,
			"names": []any{"a", "b", "c"}, "cmd": "mine"}, "7 u b false 0.25 a,b,c mine", ""},
		{"integers of any width", map[string]any{"depth": int64(9)}, "9 x a true 0.5 p,q safe", ""},
		{"wrong type", map[string]any{"depth": "deep"}, "3 x a true 0.5 p,q safe", "setting 'depth': expected an integer; using the default"},
		{"fraction for an integer", map[string]any{"depth": 2.5}, "3 x a true 0.5 p,q safe", "setting 'depth': expected an integer"},
		{"below the minimum", map[string]any{"depth": 0.0}, "3 x a true 0.5 p,q safe", "setting 'depth': 0 is below the minimum 1"},
		{"above the maximum", map[string]any{"ratio": 1.5}, "3 x a true 0.5 p,q safe", "setting 'ratio': 1.5 is above the maximum 1"},
		{"not in the enum", map[string]any{"mode": "c"}, "3 x a true 0.5 p,q safe", "setting 'mode': expected one of [a b]"},
		{"list with a non-string", map[string]any{"names": []any{"a", 1.0}}, "3 x a true 0.5 p,q safe", "setting 'names': expected a list of strings"},
		{"unknown keys are ignored", map[string]any{"nope": 1.0}, "3 x a true 0.5 p,q safe", ""},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := settingsHarness(t, r.user, nil)
			if got := show(t, h); got != r.want {
				t.Fatalf("settings = %q, want %q", got, r.want)
			}
			if r.warn != "" && !logHas(h, "t", r.warn) {
				t.Fatalf("log = %v, want a line with %q", h.LogText("t"), r.warn)
			}
			if r.warn == "" {
				for _, l := range h.LogText("t") {
					if strings.HasPrefix(l, "warn:") {
						t.Fatalf("unexpected warning: %v", h.LogText("t"))
					}
				}
			}
		})
	}
}

// TestProjectSettingsOnlyForProjectKeys: the project layer is untrusted input. It overrides
// the user layer only for keys declared project = true, is type-checked like every value,
// and every other key is dropped with one warning (AMENDMENTS C11).
func TestProjectSettingsOnlyForProjectKeys(t *testing.T) {
	rows := []struct {
		name          string
		user, project map[string]any
		want          string
		warns         []string
	}{
		{"project key wins over the user", map[string]any{"depth": 7.0, "label": "u"}, map[string]any{"depth": 2.0, "label": "p"},
			"2 p a true 0.5 p,q safe", nil},
		{"project key wins over the default", nil, map[string]any{"label": "p"}, "3 p a true 0.5 p,q safe", nil},
		{"other keys are dropped", map[string]any{"cmd": "mine"}, map[string]any{"cmd": "rm -rf /", "mode": "b"},
			"3 x a true 0.5 p,q mine", []string{"project setting 'cmd' ignored (not project=true)", "project setting 'mode' ignored (not project=true)"}},
		{"unknown keys are dropped too", nil, map[string]any{"extra": "z"}, "3 x a true 0.5 p,q safe", []string{"project setting 'extra' ignored (not project=true)"}},
		{"a bad project value keeps the user value", map[string]any{"depth": 7.0}, map[string]any{"depth": 99.0},
			"7 x a true 0.5 p,q safe", []string{"project setting 'depth': 99 is above the maximum 10; ignored"}},
		{"a bad project type keeps the default", nil, map[string]any{"label": 5.0}, "3 x a true 0.5 p,q safe", []string{"project setting 'label': expected a string; ignored"}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := settingsHarness(t, r.user, r.project)
			if got := show(t, h); got != r.want {
				t.Fatalf("settings = %q, want %q", got, r.want)
			}
			var warns []string
			for _, l := range h.LogText("t") {
				if strings.HasPrefix(l, "warn: ") {
					warns = append(warns, strings.TrimPrefix(l, "warn: "))
				}
			}
			for _, want := range r.warns {
				n := 0
				for _, w := range warns {
					if strings.Contains(w, want) {
						n++
					}
				}
				if n != 1 {
					t.Errorf("warning %q logged %d times, want once; warnings = %v", want, n, warns)
				}
			}
			if len(warns) != len(r.warns) {
				t.Errorf("warnings = %v, want %d", warns, len(r.warns))
			}
		})
	}
}

func TestSettingsLiveTableAndGet(t *testing.T) {
	h := settingsHarness(t, map[string]any{"depth": 4.0}, nil)
	if got := show(t, h); got != "4 x a true 0.5 p,q safe" {
		t.Fatalf("settings = %q", got)
	}
	// a config reload recomputes the values; the live table follows
	h.RT.UpdateSettings("t", map[string]any{"depth": 8.0, "label": "z"}, map[string]any{"label": "proj"})
	if got := show(t, h); got != "8 proj a true 0.5 p,q safe" {
		t.Fatalf("after UpdateSettings: %q", got)
	}
	h.RT.UpdateSettings("nobody", nil, nil) // unknown plugins are ignored

	h2 := cmd(t, "", `
local s = gvim.settings.define{a = {type = "integer", default = 1}, l = {type = "string_list", default = {"x"}}}
local got = gvim.settings.get()
T.spy(got.a, got.l[1], getmetatable(s), (pcall(function() s.a = 2 end)), s.a, s.missing)
s.l[1] = "changed" -- a copy: the stored list is not affected
T.spy(s.l[1], gvim.settings.get().l[1])`)
	if got := spyText(h2); got != "1 x locked false 1 nil|x x" {
		t.Fatalf("spy = %q log=%v", got, h2.LogText(""))
	}
}

func TestSettingsDefineErrors(t *testing.T) {
	rows := []row{
		{"unknown type", `T.spy(pcall(gvim.settings.define, {a = {type = "float"}}))`, "~setting 'a': type must be one of"},
		{"missing type", `T.spy(pcall(gvim.settings.define, {a = {}}))`, "~setting 'a': type must be one of"},
		{"enum without values", `T.spy(pcall(gvim.settings.define, {a = {type = "enum"}}))`, "~an enum needs a non-empty values list"},
		{"enum value not a string", `T.spy(pcall(gvim.settings.define, {a = {type = "enum", values = {1}}}))`, "~enum values must be strings"},
		{"default of the wrong type", `T.spy(pcall(gvim.settings.define, {a = {type = "integer", default = "x"}}))`, "~setting 'a': default: expected an integer"},
		{"default out of range", `T.spy(pcall(gvim.settings.define, {a = {type = "integer", default = 5, maximum = 3}}))`, "~setting 'a': default: 5 is above the maximum 3"},
		{"default not in the enum", `T.spy(pcall(gvim.settings.define, {a = {type = "enum", values = {"x"}, default = "y"}}))`, "~setting 'a': default: expected one of"},
		{"minimum not a number", `T.spy(pcall(gvim.settings.define, {a = {type = "integer", minimum = "1"}}))`, "~minimum must be a number"},
		{"project not a boolean", `T.spy(pcall(gvim.settings.define, {a = {type = "string", project = "yes"}}))`, "~project must be a boolean"},
		{"schema entry not a table", `T.spy(pcall(gvim.settings.define, {a = 1}))`, "~the schema maps setting names to tables"},
		{"twice", `gvim.settings.define{} T.spy(pcall(gvim.settings.define, {}))`, "~was already called"},
		{"missing defaults are zero values", `local s = gvim.settings.define{b = {type = "boolean"}, i = {type = "integer", minimum = 2}, s = {type = "string"}, e = {type = "enum", values = {"z", "y"}}, l = {type = "string_list"}}
			T.spy(tostring(s.b), s.i, s.s == "", s.e, #s.l)`, "false 2 true z 0"},
	}
	luaRows(t, gvimRows(rows))
}
