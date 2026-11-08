package plugin_test

import (
	"reflect"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

func TestRuntimeInitFileAndNoPlugins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		loader   *plugin.LoaderOptions
		wantInit string
		wantOff  bool
	}{
		{"no loader", nil, "", false},
		{"loader without a config dir", &plugin.LoaderOptions{}, "", false},
		{"init file", &plugin.LoaderOptions{InitFile: "/cfg/init.lua"}, "/cfg/init.lua", false},
		{"no plugins keeps the init path", &plugin.LoaderOptions{InitFile: "/cfg/init.lua", Disabled: true}, "/cfg/init.lua", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := plugin.New(plugin.Options{Loader: tt.loader})
			if got := rt.InitFile(); got != tt.wantInit {
				t.Errorf("InitFile() = %q, want %q", got, tt.wantInit)
			}
			if got := rt.NoPlugins(); got != tt.wantOff {
				t.Errorf("NoPlugins() = %v, want %v", got, tt.wantOff)
			}
		})
	}
}

// The two blocks internal/tui writes for Macro: Save to init.lua and Macro: Promote to
// command (ui.savedMacroBlock and ui.promotedMacroBlock). The ui test pins the text; this
// test runs it, so the generated Lua is known to register and to replay its steps.
const (
	savedMacroBlock = `
-- Macro fix, recorded in gvim.
do
  local gvim = require "gvim"
  gvim.command.register{
    name = "fix",
    title = "Macro: fix",
    description = "Recorded macro (3 steps)",
    run = function()
      gvim.command.run_all{
        "move_down",
        {"insert_char", {text = "say \"hi\"\n"}},
        {"delete_line", {count = 3, ["end"] = 1.5, keep = true}},
      }
    end,
  }
end
`
	promotedMacroBlock = `
-- Macro fix, recorded in gvim.
do
  local gvim = require "gvim"
  gvim.command.register{
    name = "fix",
    title = "Macro: fix",
    description = "Recorded macro (3 steps)",
    run = function(ctx)
      -- This is plain Lua: change it freely.
      gvim.command.run("move_down")
      gvim.command.run("insert_char", {text = "say \"hi\"\n"})
      gvim.command.run("delete_line", {count = 3, ["end"] = 1.5, keep = true})
    end,
  }
end
`
)

func TestMacroBlocksRunInTheRuntime(t *testing.T) {
	tests := []struct {
		name  string
		block string
		calls []plugintest.DoCall
	}{
		{"saved: one undo group", savedMacroBlock, []plugintest.DoCall{
			{Action: "move_down", Count: 1, Group: 1},
			{Action: "insert_char", Args: map[string]any{"text": "say \"hi\"\n"}, Count: 1, Group: 1},
			{Action: "delete_line", Args: map[string]any{"end": 1.5, "keep": true}, Count: 3, Group: 1},
		}},
		{"promoted: plain calls", promotedMacroBlock, []plugintest.DoCall{
			{Action: "move_down", Count: 1},
			{Action: "insert_char", Args: map[string]any{"text": "say \"hi\"\n"}, Count: 1},
			{Action: "delete_line", Args: map[string]any{"end": 1.5, "keep": true}, Count: 3},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := cmdHost()
			// init.lua keeps its own code before the block; the block must not rely on it.
			h := start(t, "local unrelated = 1\n"+tt.block, plugintest.Options{Host: host})
			if err := h.Run("t.fix", nil); err != nil {
				t.Fatal(err)
			}
			if got := host.Cmd().Calls; !reflect.DeepEqual(got, tt.calls) {
				t.Fatalf("calls = %+v, want %+v", got, tt.calls)
			}
		})
	}
}
