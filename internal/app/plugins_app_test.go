package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
)

// TestBuildRuntimeWiresThePluginPorts: the model gets the runtime, the editor port and the
// key builder, with plugins on or off.
func TestBuildRuntimeWiresThePluginPorts(t *testing.T) {
	isolateConfig(t)
	var got tui.RootPorts
	prev := newUI
	t.Cleanup(func() { newUI = prev })
	newUI = func(d tui.Deps) *tui.Model {
		got = d.Root
		return prev(d)
	}
	for _, tt := range []struct {
		name string
		opts Options
	}{{"on", Options{Root: t.TempDir()}}, {"--no-plugins", Options{Root: t.TempDir(), NoPlugins: true}}} {
		t.Run(tt.name, func(t *testing.T) {
			got = tui.RootPorts{}
			rt, err := buildRuntime(t.Context(), tt.opts, false)
			must(t, err)
			defer rt.close()
			if got.Plugins == nil || got.PluginEditor == nil || got.PluginKeys == nil {
				t.Fatalf("plugin ports = %+v, want all set", got)
			}
		})
	}
}

// TestPluginConfigReachesTheLoader checks that plugins.<name>.enabled and .settings of the
// config files, and plugin_timeout_ms, reach the runtime.
func TestPluginConfigReachesTheLoader(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{
  "plugin_timeout_ms": 80,
  "plugins": {"cfg": {"settings": {"greeting": "hi from config", "count": 3}}, "off": {"enabled": false}}
}`)
	writeUserFile(t, "plugins/cfg.lua", `
local gvim = require "gvim"
local s = gvim.settings.define{
  greeting = { type = "string", default = "default" },
  count = { type = "integer", default = 1 },
}
gvim.command.register{ name = "show", title = "Show Settings", run = function()
  gvim.notify(s.greeting .. " " .. s.count)
end }
`)
	writeUserFile(t, "plugins/off.lua", `error("must not load")`)
	s := newSession(t, Options{})
	s.waitStatus("cfg", "loaded")
	s.waitStatus("off", "disabled")
	s.runCommand("Show Settings")
	if got := s.statusRow(); !strings.Contains(got, "hi from config 3") {
		t.Fatalf("status row = %q, want the settings of the config", got)
	}
}

func TestProjectOpenerTracksTheRoot(t *testing.T) {
	isolateConfig(t)
	first, second := t.TempDir(), t.TempDir()
	p := uiadapter.NewProjects(t.Context(), nil, first, nil)
	if got := p.RootDir(); got != first {
		t.Fatalf("rootDir = %q, want %q", got, first)
	}
	if _, err := p.Open(second); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	want, _ := filepath.EvalSymlinks(second)
	if got, _ := filepath.EvalSymlinks(p.RootDir()); got != want {
		t.Fatalf("rootDir after Open = %q, want %q", p.RootDir(), second)
	}
}
