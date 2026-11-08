package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// warned counts the notices and log lines that contain sub.
func (e *env) warned(sub string) (notices, logs int) {
	for _, n := range e.notices() {
		if strings.Contains(n, sub) {
			notices++
		}
	}
	for _, l := range e.h.LogText("") {
		if strings.Contains(l, sub) {
			logs++
		}
	}
	return
}

const lintSettingsPlugin = `
local gvim = require "gvim"
gvim.settings.define { depth = { type = "integer", default = 3 }, label = { type = "string", default = "x" } }
`

// A key that is not a setting of the plugin, and a plugin that is not installed, are named
// once (typos in plugins.<name> of the config).
func TestConfigTyposAreWarnedOnce(t *testing.T) {
	e := newEnv(t)
	e.user("setdemo.lua", lintSettingsPlugin)
	e.user("plain.lua", `T.spy("plain")`)
	e.opts.Settings = map[string]plugin.PluginSettings{
		"setdemo":      {User: map[string]any{"bogus": true, "depth": 2.0}},
		"nosuchplugin": {User: map[string]any{"a": 1.0}},
		"plain":        {User: map[string]any{"x": 1.0}},
	}
	e.start()

	for _, want := range []string{"'bogus'", "plugins.nosuchplugin", "plugins.plain"} {
		if n, l := e.warned(want); n != 1 || l < 1 {
			t.Errorf("%s: %d notices, %d log lines, want one notice and a log line; notices %q", want, n, l, e.notices())
		}
	}
	if n, _ := e.warned("'depth'"); n != 0 {
		t.Errorf("a known setting was reported: %q", e.notices())
	}
	// a reload does not say it again
	e.control("reload_all", "")
	if n, _ := e.warned("plugins.nosuchplugin"); n != 1 {
		t.Errorf("the warning came again after a reload: %q", e.notices())
	}
}

// A binding of an action nothing provides is named once, when the plugins have loaded.
func TestBindingToAnUnknownActionIsWarned(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.ActionList = []string{"move_left", "undo"}
	e := newEnv(t)
	e.user("keys.lua", `
local gvim = require "gvim"
gvim.command.register { name = "real", run = function() end }
gvim.keymap.set("ctrl+alt+z", "nonexistent.action")
gvim.keymap.set("ctrl+alt+x", "move_leftt")
gvim.keymap.set("ctrl+alt+c", "move_left")
gvim.keymap.set("ctrl+alt+v", "keys.real")
gvim.keymap.set("ctrl+alt+b", function() end)
`)
	e.start(plugintest.Options{Host: host})
	for _, want := range []string{"nonexistent.action", "move_leftt"} {
		if n, l := e.warned(want); n != 1 || l < 1 {
			t.Errorf("%s: %d notices, %d log lines; notices %q", want, n, l, e.notices())
		}
	}
	for _, ok := range []string{"'move_left'", "keys.real", "ctrl+alt+c", "ctrl+alt+v", "ctrl+alt+b"} {
		if n, _ := e.warned(ok); n != 0 {
			t.Errorf("%s was reported: %q", ok, e.notices())
		}
	}
	if n, _ := e.warned("ctrl+alt+z"); n != 1 {
		t.Errorf("the warning does not name the key: %q", e.notices())
	}
}

// A failed command.register or keymap.set that the plugin ignores leaves a line in the log.
func TestIgnoredRegistrationFailuresAreLogged(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
gvim.command.register { name = "Bad Name", run = function() end }
gvim.keymap.set("ctrl+nosuchkey+!!", "undo")
`)
	got := strings.Join(h.LogText("t"), "\n")
	for _, want := range []string{"command.register", "Bad Name", "keymap.set"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}
}
