package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/plugin"
)

// A key of plugins.<name>.settings the plugin does not define, a plugins.<name> that is not
// installed, a plugin whose settings have no schema: each is reported once, naming the bad key.
func TestConfigTyposAreReportedOnce(t *testing.T) {
	e := newEnv(t)
	e.user("setdemo.lua", loaderSettingsPlugin)
	e.user("plain.lua", `T.command("x", function() end)`)
	e.opts.Settings = map[string]plugin.PluginSettings{
		"setdemo":      {User: map[string]any{"bogus": true, "depth": 4.0}},
		"plain":        {User: map[string]any{"anything": 1.0}},
		"nosuchplugin": {User: map[string]any{"a": 1.0}},
	}
	e.opts.Enabled = map[string]bool{"alsonosuch": true}
	e.start()

	want := []string{
		"setdemo: config plugins.setdemo.settings has 'bogus', which the plugin does not define (known: depth, label)",
		"plain: config plugins.plain.settings has 'anything', which the plugin does not define (the plugin defines no settings)",
		"config plugins.nosuchplugin: no plugin of that name is installed",
		"config plugins.alsonosuch: no plugin of that name is installed",
	}
	all := strings.Join(e.notices(), "\n")
	for _, w := range want {
		if n := strings.Count(all, w); n != 1 {
			t.Errorf("%q reported %d times, want once; notices:\n%s", w, n, all)
		}
	}
	// reloading does not repeat the report of a key that was already named
	before := len(e.notices())
	e.control("reload", "setdemo")
	if got := strings.Count(strings.Join(e.notices()[before:], "\n"), "'bogus'"); got != 0 {
		t.Errorf("the typo was reported again on reload: %v", e.notices()[before:])
	}
}

// A registration that fails (an invalid command name, a bad chord) is said once in the log and on
// the status row: a plugin that ignores the result would otherwise just lack the command.
func TestFailedRegistrationsAreReported(t *testing.T) {
	t.Parallel()
	h := start(t, `
local gvim = require "gvim"
gvim.command.register { name = "Bad Name", run = function() end }
gvim.keymap.set("ctrl+alt+nonsense+z", "save")
`)
	for _, want := range []string{"command.register: invalid: invalid command name", "keymap.set: invalid:"} {
		if !logHas(h, "t", want) {
			t.Errorf("log lacks %q: %v", want, h.LogText("t"))
		}
		if !noticeHas(h, want) {
			t.Errorf("no notice with %q: %v", want, h.NoticeTexts())
		}
	}
}
