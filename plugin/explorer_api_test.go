package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// explorerPlugin exercises gvim.explorer's decorations and slots.
const explorerPlugin = `
local gvim = require 'gvim'
local root = gvim.workspace.root()
gvim.command.register{ name = "set", run = function()
  gvim.notify("d:" .. tostring(gvim.explorer.set_decorations("git_explorer", {
    { path = root .. "/a.go", symbol = "M", color = "ui.file_status_modified_fg", priority = 50 },
    { path = root .. "/b.go", symbol = "U", color = { r = 1, g = 2, b = 3 }, priority = 30 },
  })))
  gvim.notify("s:" .. tostring(gvim.explorer.set_slots("git_explorer", {
    { path = root .. "/a.go", trailing = { text = "M", color = "#00ff00" }, suppress_leading = true },
    { path = root .. "/b.go", name_color = "ui.file_status_added_fg", suppress_trailing = true, priority = 10 },
  })))
end }
gvim.command.register{ name = "clear", run = function()
  gvim.notify("cd:" .. tostring(gvim.explorer.clear_decorations("git_explorer")))
  gvim.notify("cs:" .. tostring(gvim.explorer.clear_slots("git_explorer")))
end }
`

// TestExplorerLuaSurface checks the Lua binding, the recorded decorations/slots and the
// clears.
func TestExplorerLuaSurface(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"ex.lua": explorerPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("ex.set", nil); err != nil {
		t.Fatal(err)
	}
	notices := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(notices, "d:true") || !strings.Contains(notices, "s:true") {
		t.Fatalf("notices = %q", notices)
	}
	decos := h.Host.ExplorerDecorations("ex", "git_explorer")
	if len(decos) != 2 {
		t.Fatalf("decorations = %+v", decos)
	}
	if decos[0].Symbol != "M" || decos[0].Color.Key != "ui.file_status_modified_fg" || decos[0].Priority != 50 {
		t.Fatalf("decoration 0 = %+v", decos[0])
	}
	if decos[1].Color.Key != "" || !decos[1].Color.Set || decos[1].Color.R != 1 {
		t.Fatalf("rgb decoration = %+v", decos[1])
	}
	slots := h.Host.ExplorerSlots("ex", "git_explorer")
	if len(slots) != 2 || !slots[0].SuppressLeading || slots[0].Trailing == nil || slots[0].Trailing.Text != "M" {
		t.Fatalf("slots = %+v", slots)
	}
	if !slots[1].SuppressTrailing || slots[1].NameColor.Key == "" {
		t.Fatalf("slot 1 = %+v", slots[1])
	}
	if err := h.Run("ex.clear", nil); err != nil {
		t.Fatal(err)
	}
	notices = strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(notices, "cd:true") || !strings.Contains(notices, "cs:true") {
		t.Fatalf("clear notices = %q", notices)
	}
	if got := h.Host.ExplorerDecorations("ex", "git_explorer"); len(got) != 0 {
		t.Fatalf("decorations after clear = %+v", got)
	}
	if got := h.Host.ExplorerSlots("ex", "git_explorer"); len(got) != 0 {
		t.Fatalf("slots after clear = %+v", got)
	}
	_ = pluginapi.MaxExplorerEntries
}
