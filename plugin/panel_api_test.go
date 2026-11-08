package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// panelPlugin is a plugin that mounts a widget panel from a command, updates it, commands
// its widgets and reports the widget events back as notices. It is the end-to-end session of
// the declarative panel API: Lua calls the host, the fake host records, and an emitted
// widget_event reaches the handler.
const panelPlugin = `
local gvim = require 'gvim'
local panel = 0

gvim.on("widget_event", function(ev)
  gvim.notify(ev.event .. ":" .. tostring(ev.widget) .. ":" .. tostring(ev.index))
end)

gvim.command.register{ name = "mount", run = function()
  panel = gvim.ui.mount_widget_panel({
    root = { kind = "col", children = {
      { kind = "label", text = "Files" },
      { kind = "button", key = "ok", label = "OK" },
      { kind = "toggle", key = "wrap", label = "Wrap", checked = true },
      { kind = "list", key = "rows", items = { "one", { text = "two", detail = "2" } }, selected_index = 1 },
    } },
    focus_key = "ok",
  }, { title = "Panel", focus = true, width = 50, height = 40 })
  if panel == nil then gvim.notify("mount failed") end
end }

gvim.command.register{ name = "update", run = function()
  local ok, err = gvim.ui.update_widget_panel(panel, { root = { kind = "label", text = "updated" } })
  if not ok then gvim.notify("update: " .. tostring(err)) end
end }

gvim.command.register{ name = "cmd", run = function()
  gvim.ui.widget_command(panel, { kind = "focusAdvance", delta = 1 })
  gvim.ui.widget_mutate(panel, { kind = "setChecked", widget_key = "wrap", checked = false })
end }

gvim.command.register{ name = "unmount", run = function()
  gvim.ui.unmount_widget_panel(panel)
end }

gvim.command.register{ name = "sidebar", run = function()
  local ok, err = gvim.ui.mount_sidebar_section({
    root = { kind = "tree", nodes = { { text = "Heading", expanded = true }, { text = "Sub", depth = 1 } } },
  }, { title = "Outline", rows = 6, closable = false })
  if ok == nil then gvim.notify("sidebar: " .. tostring(err)) end
end }

gvim.command.register{ name = "bad", run = function()
  local id, err = gvim.ui.mount_widget_panel({ kind = "nope" })
  gvim.notify(tostring(id) .. " " .. tostring(err))
end }
`

// panelEvents returns the widget-event notices of the plugin, one string per handler call.
func panelEvents(h *plugintest.Harness) []string {
	var out []string
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, ":") && !strings.HasPrefix(n, "mount") && !strings.HasPrefix(n, "update") && !strings.HasPrefix(n, "sidebar") {
			out = append(out, n)
		}
	}
	return out
}

func TestPanelMountUpdateCommand(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"panels.lua": panelPlugin}, plugintest.Options{})
	h.Start()
	if p, ok := h.Plugin("panels"); !ok || p.Status != "loaded" {
		t.Fatalf("plugin status = %+v, log %v", p, h.LogText(""))
	}
	if err := h.Run("panels.mount", nil); err != nil {
		t.Fatal(err)
	}
	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 1 {
		t.Fatalf("mounted = %+v, want one", mounted)
	}
	fp := mounted[0]
	if fp.Mount.Placement != pluginapi.PlacementFloat || fp.Mount.Title != "Panel" || !fp.Mount.Focus {
		t.Fatalf("mount = %+v, want a focused float titled Panel", fp.Mount)
	}
	if fp.Mount.WidthPct != 50 || fp.Mount.HeightPct != 40 {
		t.Fatalf("size = %d, %d", fp.Mount.WidthPct, fp.Mount.HeightPct)
	}
	if fp.Panel.Root.Kind != pluginapi.WidgetCol || len(fp.Panel.Root.Children) != 4 {
		t.Fatalf("root = %+v, want a col of four children", fp.Panel.Root)
	}
	if fp.Panel.FocusKey != "ok" {
		t.Fatalf("focus_key = %q", fp.Panel.FocusKey)
	}
	list := fp.Panel.Root.Children[3]
	if list.Kind != pluginapi.WidgetList || len(list.Items) != 2 || list.Selected != 0 || list.Items[1].Detail != "2" {
		t.Fatalf("list = %+v", list)
	}

	// A widget_event reaches the handler.
	h.Emit(pluginapi.Event{Name: "widget_event", Data: map[string]any{
		"panel": uint64(fp.ID), "event": "activate", "widget": "ok", "index": 1,
	}})
	events := panelEvents(h)
	if len(events) != 1 || events[0] != "activate:ok:1" {
		t.Fatalf("events = %q, want activate:ok:1", events)
	}

	// Commands and mutations reach the host.
	if err := h.Run("panels.cmd", nil); err != nil {
		t.Fatal(err)
	}
	if len(fp.Commands) != 1 || fp.Commands[0].Kind != "focusAdvance" || fp.Commands[0].Delta != 1 {
		t.Fatalf("commands = %+v", fp.Commands)
	}
	if len(fp.Mutations) != 1 || fp.Mutations[0].Kind != "setChecked" || fp.Mutations[0].WidgetKey != "wrap" || fp.Mutations[0].Checked {
		t.Fatalf("mutations = %+v", fp.Mutations)
	}

	// An update replaces the content.
	if err := h.Run("panels.update", nil); err != nil {
		t.Fatal(err)
	}
	if fp.Panel.Root.Kind != pluginapi.WidgetLabel || fp.Panel.Root.Text != "updated" {
		t.Fatalf("updated root = %+v", fp.Panel.Root)
	}

	// Unmount closes it.
	if err := h.Run("panels.unmount", nil); err != nil {
		t.Fatal(err)
	}
	if got := h.Host.Panels().Mounted(); len(got) != 0 {
		t.Fatalf("still mounted: %+v", got)
	}
}

func TestSidebarSectionMountAndCleanup(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"panels.lua": panelPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("panels.sidebar", nil); err != nil {
		t.Fatal(err)
	}
	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 1 || mounted[0].Mount.Placement != pluginapi.PlacementSidebar {
		t.Fatalf("mounted = %+v, want one sidebar section", mounted)
	}
	if mounted[0].Mount.Title != "Outline" || mounted[0].Mount.Rows != 6 || mounted[0].Mount.Closable {
		t.Fatalf("section = %+v, want Outline, 6 rows, not closable", mounted[0].Mount)
	}
	// Unloading the plugin drops the section (OwnerGone -> the fake's hook).
	if got := h.Host.Resources(); len(got) == 0 {
		t.Fatal("the section is not counted as a resource")
	}
	h.Unload("panels")
	if got := h.Host.Panels().Mounted(); len(got) != 0 {
		t.Fatalf("a section survived the unload: %+v", got)
	}
	for _, r := range h.Host.Resources() {
		if strings.Contains(r, "panel:") {
			t.Fatalf("the unload left %q", r)
		}
	}
}

func TestPanelBadSpecReportsInvalid(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"panels.lua": panelPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("panels.bad", nil); err != nil {
		t.Fatal(err)
	}
	notices := h.NoticeTexts()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "invalid") {
		t.Fatalf("notices = %q, want an invalid error", notices)
	}
}

// The widget API is covered by the stub/docs contract tests: a plugin that touches every
// new function must find them in a live state, so the docs and gvim.d.lua names stay exact.
func TestWidgetPanelAPISurface(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"panels.lua": panelPlugin}, plugintest.Options{})
	h.Start()
	inv, ok := plugin.APIInventory(h.RT, "panels")
	if !ok {
		t.Fatal("plugin did not load")
	}
	want := []string{
		"gvim.ui.mount_widget_panel", "gvim.ui.update_widget_panel", "gvim.ui.unmount_widget_panel",
		"gvim.ui.widget_command", "gvim.ui.widget_mutate", "gvim.ui.mount_sidebar_section",
	}
	have := map[string]bool{}
	for _, f := range inv.Funcs {
		have[f] = true
	}
	for _, f := range want {
		if !have[f] {
			t.Errorf("the live state has no %s", f)
		}
	}
}
