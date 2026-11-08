package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

// menuPlugin adds a translated row of its own command to the View menu, then removes it and
// clears; the fake host records the whole-set replaces.
const menuPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "toggle", title = "Do Thing", run = function() end }

gvim.command.register{ name = "add", run = function()
  local ok, err = gvim.menu.add_item{
    menu = "View", label = "%cmd.menu_label", action = gvim.plugin.name .. ".toggle",
    checkbox = "thing_on", when = "has_buffer", after = "toggle_file_explorer",
  }
  if not ok then gvim.notify("add: " .. tostring(err)) end
end }

gvim.command.register{ name = "remove", run = function()
  local ok, err = gvim.menu.remove_item{ menu = "View", action = gvim.plugin.name .. ".toggle" }
  if not ok then gvim.notify("remove: " .. tostring(err)) end
end }

gvim.command.register{ name = "clear", run = function() gvim.menu.clear() end }

gvim.command.register{ name = "spam", run = function()
  for i = 1, 40 do
    local ok, err = gvim.menu.add_item{ menu = "View", label = "row" .. i, action = gvim.plugin.name .. ".toggle" }
    if not ok then gvim.notify("spam: " .. tostring(err)) return end
  end
end }
`

const menuI18n = `{"en": {"cmd.menu_label": "Do the thing"}}`

func TestMenuAddRemoveClear(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"m.lua": menuPlugin, "m.i18n.json": menuI18n}, plugintest.Options{})
	h.Start()
	if err := h.Run("m.add", nil); err != nil {
		t.Fatal(err)
	}
	rows := h.Host.Menu().ByPlugin["m"]
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
	got := rows[0]
	if got.Menu != "View" || got.Label != "Do the thing" || got.Action != "m.toggle" ||
		got.Checkbox != "thing_on" || got.When != "has_buffer" || got.After != "toggle_file_explorer" {
		t.Fatalf("row = %+v, want the translated View row", got)
	}
	if err := h.Run("m.remove", nil); err != nil {
		t.Fatal(err)
	}
	if rows := h.Host.Menu().ByPlugin["m"]; len(rows) != 0 {
		t.Fatalf("rows after remove = %+v", rows)
	}
	// Removing an absent row is an error the plugin hears.
	if err := h.Run("m.remove", nil); err != nil {
		t.Fatal(err)
	}
	notices := h.NoticeTexts()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "not_found") {
		t.Fatalf("notices = %q, want a not_found error", notices)
	}
	// Add then clear.
	if err := h.Run("m.add", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("m.clear", nil); err != nil {
		t.Fatal(err)
	}
	if rows := h.Host.Menu().ByPlugin["m"]; len(rows) != 0 {
		t.Fatalf("rows after clear = %+v", rows)
	}
}

func TestMenuQuotaAndUnloadCleanup(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"m.lua": menuPlugin, "m.i18n.json": menuI18n}, plugintest.Options{})
	h.Start()
	if err := h.Run("m.spam", nil); err != nil {
		t.Fatal(err)
	}
	if rows := h.Host.Menu().ByPlugin["m"]; len(rows) != 32 {
		t.Fatalf("rows = %d, want the quota 32", len(rows))
	}
	notices := h.NoticeTexts()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "limit") {
		t.Fatalf("notices = %q, want a limit error", notices)
	}
	// Unloading the plugin drops its rows (OwnerGone).
	if err := h.Run("m.add", nil); err != nil {
		t.Fatal(err)
	}
	h.Unload("m")
	for _, r := range h.Host.Resources() {
		if strings.Contains(r, "menu:") {
			t.Fatalf("the unload left %q", r)
		}
	}
}
