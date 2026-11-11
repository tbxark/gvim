package plugin_test

import (
	"fmt"
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
	// The fixed-height leaf carries its height (the JSON {type="fixed",height=10}).
	if fg.Spec.Layout.Second == nil || fg.Spec.Layout.Second.Leaf == nil ||
		!fg.Spec.Layout.Second.Leaf.Fixed || fg.Spec.Layout.Second.Leaf.Height != 10 {
		t.Fatalf("fixed leaf = %+v", fg.Spec.Layout.Second)
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

// intruderPlugin tries to drive, close and remove what another plugin made: the ids are
// global and guessable, so each call must answer closed.
const intruderPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "poke", run = function(ctx)
  local group, panel = ctx.args.group, ctx.args.panel
  local _, e1 = gvim.buf.close_group(group)
  local _, e2 = gvim.buf.set_panel_visible(group, "files", false)
  local _, e3 = gvim.win.set_scroll_sync_anchors(7, { { 0, 0 } })
  local _, e4 = gvim.win.remove_scroll_sync(7)
  local _, e5 = gvim.ui.unmount_widget_panel(panel)
  gvim.notify(table.concat({ tostring(e1), tostring(e2), tostring(e3), tostring(e4), tostring(e5) }, "|"))
end }
`

const ownerPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "make", run = function()
  local layout = gvim.json.encode{ type = "scrollable", id = "files" }
  local group = gvim.buf.create_group("Mine", layout)
  local wins = gvim.win.list()
  gvim.win.create_scroll_sync(7, wins[1], wins[2])
  local panel, err = gvim.ui.mount_widget_panel({ kind = "text", text = "x" })
  gvim.notify("made:" .. tostring(group) .. ":" .. tostring(panel) .. ":" .. tostring(err))
end }
`

func TestPluginsCannotTouchEachOthersGroups(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"owner.lua": ownerPlugin, "intruder.lua": intruderPlugin}, plugintest.Options{})
	h.Start()
	if _, err := h.Host.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("owner.make", nil); err != nil {
		t.Fatal(err)
	}
	var group, panel int
	for _, n := range h.NoticeTexts() {
		if rest, ok := strings.CutPrefix(n, "made:"); ok {
			if _, err := fmt.Sscanf(strings.ReplaceAll(rest, ":", " "), "%d %d", &group, &panel); err != nil {
				t.Fatalf("owner notice %q: %v", n, err)
			}
		}
	}
	if group == 0 || panel == 0 {
		t.Fatalf("the owner made nothing: %q", h.NoticeTexts())
	}
	if err := h.Run("intruder.poke", map[string]any{"group": group, "panel": panel}); err != nil {
		t.Fatal(err)
	}
	notices := strings.Join(h.NoticeTexts(), "\n")
	if strings.Count(notices, "not mounted by this plugin") != 5 {
		t.Fatalf("notices = %q, want five refusals", notices)
	}
	groups := h.Host.Groups()
	if fg := groups.ByID[pluginapi.BufferGroupID(group)]; fg == nil || fg.Closed {
		t.Fatalf("the owner's group = %+v, want it open", fg)
	}
	if sg := groups.Scroll[7]; sg == nil || sg.Removed {
		t.Fatalf("the owner's scroll sync = %+v, want it kept", sg)
	}
}
