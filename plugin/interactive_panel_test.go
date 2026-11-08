package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// interactivePlugin mounts a panel of the tier-1b interactive widgets, sends widget commands
// and mutations from Lua, and reports widget_events as notices.
const interactivePlugin = `
local gvim = require 'gvim'
local panel = 0

gvim.on("widget_event", function(ev)
  gvim.notify(ev.event .. ":" .. tostring(ev.value) .. ":" .. tostring(ev.cursor_byte) .. ":" .. tostring(ev.focus_key))
end)

gvim.command.register{ name = "mount", run = function()
  local ok, err = gvim.ui.mount_widget_panel({
    root = { kind = "col", children = {
      { kind = "text", key = "name", text = "abc", rows = 2, max_rows = 4, read_only = false, placeholder = "type", field_width = 20, completions = { "a", "b" } },
      { kind = "number", key = "count", value = 3, min = 0, max = 9, step = 2, integer = true },
      { kind = "dropdown", key = "kind", options = { "x", "y" }, selected_index = 1, combo = true },
      { kind = "radio", key = "radio", options = { "r1", "r2" } },
      { kind = "dualList", key = "dual", options = { "d1", "d2" }, included = { "d1" } },
      { kind = "labeledSection", title = "Box", children = {
        { kind = "button", key = "ok", label = "OK", intent = "primary" },
      } },
      { kind = "component", children = { { kind = "label", text = "wrapped" } } },
      { kind = "popup", title = "Pop", children = { { kind = "label", text = "inside" } } },
      { kind = "windowEmbed", window = 3, rows = 2 },
    } },
    focus_key = "name",
  }, { title = "Interactive", focus = true, page = true, auto_focus_first = false, width = 70, height = 80 })
  if ok == nil then gvim.notify("mount: " .. tostring(err)) end
  panel = ok
end }

gvim.command.register{ name = "act", run = function()
  gvim.ui.widget_command(panel, { kind = "textInputChar", text = "Z" })
  gvim.ui.widget_command(panel, { kind = "textInputKey", key = "backspace" })
  gvim.ui.widget_command(panel, { kind = "key", key = "down" })
end }

gvim.command.register{ name = "mutate", run = function()
  local ok, err = gvim.ui.widget_mutate(panel, { kind = "setValue", widget_key = "name", value = "hello", cursor_byte = 2 })
  if not ok then gvim.notify("setValue: " .. tostring(err)) end
  gvim.ui.widget_mutate(panel, { kind = "setNumber", widget_key = "count", number = 7 })
  gvim.ui.widget_mutate(panel, { kind = "setCompletions", widget_key = "name", items = { "c", "d" } })
  gvim.ui.widget_mutate(panel, { kind = "setDualIncluded", widget_key = "dual", included = { "d2" } })
  gvim.ui.widget_mutate(panel, { kind = "setItems", widget_key = "name", items = { { text = "e" } }, item_keys = { "ke" } })
  gvim.ui.widget_mutate(panel, { kind = "setExpandedKeys", widget_key = "name", keys = { "a", "b" } })
  gvim.ui.widget_mutate(panel, { kind = "setCheckedKeys", widget_key = "name", keys = { "a" }, checked = true })
  gvim.ui.widget_mutate(panel, { kind = "appendTreeNodes", widget_key = "name", new_nodes = { { text = "n", key = "nk" } } })
  gvim.ui.widget_mutate(panel, { kind = "setRawEntries", widget_key = "name", entries = { { text = "raw" } } })
  gvim.ui.widget_mutate(panel, { kind = "setFocusKey", focus_key = "ok" })
  gvim.ui.widget_mutate(panel, { kind = "previewTreeMove", widget_key = "name", source_key = "a", after_key = "b", label = "moving" })
  gvim.ui.widget_mutate(panel, { kind = "endTreeMovePreview", widget_key = "name", commit = true })
end }
`

func TestInteractivePanelLuaSurface(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"ip.lua": interactivePlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("ip.mount", nil); err != nil {
		t.Fatal(err)
	}
	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 1 {
		t.Fatalf("mounted = %+v, notices %q log %v", mounted, h.NoticeTexts(), h.LogText(""))
	}
	m := mounted[0]
	if !m.Mount.Page || m.Mount.AutoFocusFirst == nil || *m.Mount.AutoFocusFirst {
		t.Fatalf("mount options = %+v", m.Mount)
	}
	root := m.Panel.Root
	if root.Kind != pluginapi.WidgetCol || len(root.Children) != 9 {
		t.Fatalf("root = %+v", root)
	}
	name := root.Children[0]
	if name.Kind != pluginapi.WidgetText || name.Text != "abc" || name.Rows != 2 || name.MaxRows != 4 || name.Placeholder != "type" || name.FieldWidth != 20 || len(name.Completions) != 2 {
		t.Fatalf("text = %+v", name)
	}
	num := root.Children[1]
	if num.Kind != pluginapi.WidgetNumber || num.Number != 3 || num.Min != 0 || num.Max != 9 || num.Step != 2 || !num.Integer {
		t.Fatalf("number = %+v", num)
	}
	dd := root.Children[2]
	if dd.Kind != pluginapi.WidgetDropdown || len(dd.Options) != 2 || dd.Selected != 0 || !dd.Combo {
		t.Fatalf("dropdown = %+v", dd)
	}
	dual := root.Children[4]
	if dual.Kind != pluginapi.WidgetDualList || len(dual.Included) != 1 || dual.Included[0] != "d1" {
		t.Fatalf("dual = %+v", dual)
	}
	section := root.Children[5]
	if section.Kind != pluginapi.WidgetLabeledSection || section.Title != "Box" || len(section.Children) != 1 || section.Children[0].Key != "ok" {
		t.Fatalf("section = %+v", section)
	}

	// Widget commands reach the host with their payloads.
	if err := h.Run("ip.act", nil); err != nil {
		t.Fatal(err)
	}
	cmds := m.Commands
	if len(cmds) != 3 || cmds[0].Kind != "textInputChar" || cmds[0].Text != "Z" || cmds[1].Key != "backspace" || cmds[2].Key != "down" {
		t.Fatalf("commands = %+v", cmds)
	}

	// Mutations reach the host with every field.
	if err := h.Run("ip.mutate", nil); err != nil {
		t.Fatal(err)
	}
	muts := m.Mutations
	if len(muts) != 12 {
		t.Fatalf("mutations = %d: %+v", len(muts), muts)
	}
	if muts[0].Kind != "setValue" || muts[0].Value != "hello" || !muts[0].HasCursor || muts[0].CursorByte != 2 {
		t.Fatalf("setValue = %+v", muts[0])
	}
	if muts[1].Number != 7 {
		t.Fatalf("setNumber = %+v", muts[1])
	}
	if len(muts[2].Items) != 2 || muts[2].Items[0].Text != "c" {
		t.Fatalf("setCompletions = %+v", muts[2])
	}
	if len(muts[3].Included) != 1 || muts[3].Included[0] != "d2" {
		t.Fatalf("setDualIncluded = %+v", muts[3])
	}
	if len(muts[4].Items) != 1 || muts[4].Items[0].Text != "e" || muts[4].ItemKeys[0] != "ke" {
		t.Fatalf("setItems = %+v", muts[4])
	}
	if len(muts[5].Keys) != 2 {
		t.Fatalf("setExpandedKeys = %+v", muts[5])
	}
	if !muts[6].Checked || muts[6].Keys[0] != "a" {
		t.Fatalf("setCheckedKeys = %+v", muts[6])
	}
	if len(muts[7].NewNodes) != 1 || muts[7].NewNodes[0].Key != "nk" {
		t.Fatalf("appendTreeNodes = %+v", muts[7])
	}
	if len(muts[8].Entries) != 1 || muts[8].Entries[0].Text != "raw" {
		t.Fatalf("setRawEntries = %+v", muts[8])
	}
	if muts[9].FocusKey != "ok" {
		t.Fatalf("setFocusKey = %+v", muts[9])
	}
	if muts[10].SourceKey != "a" || muts[10].AfterKey != "b" || muts[10].Label != "moving" {
		t.Fatalf("previewTreeMove = %+v", muts[10])
	}
	if !muts[11].Commit {
		t.Fatalf("endTreeMovePreview = %+v", muts[11])
	}

	// A widget event reaches the Lua handler with the additive payload fields.
	h.Emit(pluginapi.Event{Name: "widget_event", Data: map[string]any{
		"panel": uint64(m.ID), "event": "change", "widget": "name", "value": "hello",
		"cursor_byte": 2, "focus_key": "name",
	}})
	notices := h.NoticeTexts()
	if len(notices) == 0 || notices[len(notices)-1] != "change:hello:2:name" {
		t.Fatalf("notices = %q", notices)
	}
}
