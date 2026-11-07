package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/plugin"
)

const loaderSettingsPlugin = `
local gvim = require "gvim"
local s = gvim.settings.define{
  depth = {type = "integer", default = 3, project = true},
  label = {type = "string", default = "x"},
}
T.command("show", function() T.spy(s.depth, s.label) end)
`

// A plugin's config settings reach gvim.settings through the loader, from discovery, from a
// reload, and after UpdateSettings (a config reload); the project layer only counts for
// keys declared project = true.
func TestLoaderPassesConfigSettingsToPlugins(t *testing.T) {
	e := newEnv(t)
	e.user("p.lua", loaderSettingsPlugin)
	e.user("q.lua", loaderSettingsPlugin)
	e.opts.Settings = map[string]plugin.PluginSettings{
		"p": {User: map[string]any{"depth": 7.0, "label": "u"}, Project: map[string]any{"depth": 9.0, "label": "hostile"}},
	}
	e.start()

	show := func(name string) string {
		t.Helper()
		e.run(name + ".show")
		got := e.spyLines()
		if len(got) != 1 {
			t.Fatalf("%s.show spy = %v; log %v", name, got, e.h.LogText(""))
		}
		return got[0]
	}
	if got := show("p"); got != "9 u" {
		t.Errorf("p after discovery = %q, want %q", got, "9 u")
	}
	if got := show("q"); got != "3 x" {
		t.Errorf("q without settings = %q, want %q", got, "3 x")
	}

	e.h.RT.UpdateSettings("p", map[string]any{"depth": 5.0}, nil)
	if got := show("p"); got != "5 x" {
		t.Errorf("p after UpdateSettings = %q, want %q", got, "5 x")
	}

	// A reload reads the settings the config reload left.
	e.control("reload", "p")
	if got := show("p"); got != "5 x" {
		t.Errorf("p after reload = %q, want %q", got, "5 x")
	}
	e.h.RT.UpdateSettings("q", map[string]any{"label": "later"}, nil)
	e.control("reload", "q")
	if got := show("q"); got != "3 later" {
		t.Errorf("q after UpdateSettings and reload = %q, want %q", got, "3 later")
	}
}
