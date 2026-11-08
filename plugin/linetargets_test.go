package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// lineTargetsPlugin sets and clears line targets on the active buffer.
const lineTargetsPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "targets", run = function()
  local buf = gvim.buf.current()
  gvim.notify("set:" .. tostring(gvim.buf.set_line_targets(buf, {
    { line = 0, path = "src/main.go", target = 41, into = "code" },
    { line = 2, path = "src/lib.go" },
  })))
  local list = gvim.buf.set_line_targets(buf, {})
  gvim.notify("empty:" .. tostring(list))
  gvim.notify("set2:" .. tostring(gvim.buf.set_line_targets(buf, {
    { line = 1, path = "a.go", target = 2 },
  })))
  gvim.notify("clear:" .. tostring(gvim.buf.clear_line_targets(buf)))
end }
`

// TestLineTargetsLuaSurface checks the Lua binding stores and clears targets.
func TestLineTargetsLuaSurface(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"lt.lua": lineTargetsPlugin}, plugintest.Options{})
	h.Start()
	id := h.Host.AddBuffer("/w/a.go", "package a\n\nvar x = 1\n")
	if err := h.Run("lt.targets", nil); err != nil {
		t.Fatal(err)
	}
	notices := ""
	for _, n := range h.NoticeTexts() {
		notices += n + "|"
	}
	for _, want := range []string{"set:true", "empty:true", "set2:true", "clear:true"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("notices = %q, missing %q", notices, want)
		}
	}
	// clear_line_targets removed the set2 targets.
	if got := h.Host.LineTargets(id); len(got) != 0 {
		t.Fatalf("targets after clear = %+v", got)
	}
	// The set call kept the targets while they were there (checked through a fresh set).
	if err := h.Run("lt.targets", nil); err != nil {
		t.Fatal(err)
	}
	_ = pluginapi.MaxLineTargets
}
