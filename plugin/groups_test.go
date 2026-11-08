package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// groupsPlugin exercises the group-lite APIs: create a two-panel group, fill one panel,
// set its title, hide/show/focus panels, query them, then create and drop a scroll sync.
const groupsPlugin = `
local gvim = require 'gvim'
local group = 0

gvim.command.register{ name = "open", run = function()
  local layout = gvim.json.encode{ type = "split", direction = "h", ratio = 0.5,
    first = { type = "scrollable", id = "files" },
    second = { type = "fixed", id = "preview", height = 10 } }
  local ok, err = gvim.buf.create_group("Search", layout)
  if ok == nil then gvim.notify("open: " .. tostring(err)) return end
  group = ok
  gvim.notify("id:" .. tostring(group))
  gvim.notify("content:" .. tostring(gvim.buf.set_panel_content(group, "files", {
    { text = "main.go\n", props = { file = "main.go", line = 3 } },
    { text = "readme\n" },
  })))
  gvim.notify("title:" .. tostring(gvim.buf.set_group_title(group, { { text = "Search: " }, { panel = "files" } })))
  gvim.notify("hide:" .. tostring(gvim.buf.set_panel_visible(group, "preview", false)))
  gvim.notify("show:" .. tostring(gvim.buf.set_panel_visible(group, "preview", true)))
  gvim.notify("focus:" .. tostring(gvim.buf.focus_panel(group, "files")))
  local panels = gvim.buf.group_panels(group)
  gvim.notify("panels:" .. tostring(#panels) .. ":" .. panels[1].name .. ":" .. tostring(panels[1].visible))
end }

gvim.command.register{ name = "sync", run = function()
  local wins = gvim.win.list()
  local ok, err = gvim.win.create_scroll_sync(1, wins[1], wins[2])
  if not ok then gvim.notify("sync: " .. tostring(err)) return end
  gvim.win.set_scroll_sync_anchors(1, { { 0, 0 }, { 10, 20 } })
  gvim.notify("sync:ok")
  gvim.win.remove_scroll_sync(1)
end }

gvim.command.register{ name = "close", run = function()
  gvim.buf.close_group(group)
  gvim.notify("closed")
end }
`

func TestBufferGroupLuaAPI(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"g.lua": groupsPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("g.open", nil); err != nil {
		t.Fatal(err)
	}
	notices := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(notices, "id:1") || !strings.Contains(notices, "content:true") ||
		!strings.Contains(notices, "title:true") || !strings.Contains(notices, "hide:true") ||
		!strings.Contains(notices, "show:true") || !strings.Contains(notices, "focus:true") ||
		!strings.Contains(notices, "panels:2:files:true") {
		t.Fatalf("notices = %q", notices)
	}
	groups := h.Host.Groups()
	fg := groups.ByID[1]
	if fg == nil || len(fg.Order) != 2 || fg.Order[0] != "files" || fg.Order[1] != "preview" {
		t.Fatalf("group = %+v", fg)
	}
	if len(fg.Content["files"]) != 2 || fg.Content["files"][0].Props["file"] != "main.go" {
		t.Fatalf("content = %+v", fg.Content["files"])
	}
	if len(fg.Title) != 2 || fg.Title[1].Panel != "files" {
		t.Fatalf("title = %+v", fg.Title)
	}
	if fg.Focused != "files" {
		t.Fatalf("focused = %q", fg.Focused)
	}

	// Two windows must exist for the scroll-sync call; split the fake editor's window.
	if _, err := h.Host.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("g.sync", nil); err != nil {
		t.Fatal(err)
	}
	sg := h.Host.Groups().Scroll[1]
	// The command created the group, set two anchors, then removed it.
	if sg == nil || !sg.Removed || len(sg.Anchors) != 2 || sg.Anchors[1].Right != 20 {
		t.Fatalf("scroll sync = %+v", sg)
	}

	if err := h.Run("g.close", nil); err != nil {
		t.Fatal(err)
	}
	if !fg.Closed {
		t.Fatal("the group did not close")
	}
	// Unloading drops the group and the scroll-sync group.
	if err := h.Run("g.open", nil); err != nil {
		t.Fatal(err)
	}
	h.Unload("g")
	for _, r := range h.Host.Resources() {
		if strings.Contains(r, "group:") || strings.Contains(r, "scroll_sync:") {
			t.Fatalf("the unload left %q", r)
		}
	}
}
