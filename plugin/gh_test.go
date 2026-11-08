package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// ghPlugin exercises the action popup, the prompt suggestion list and the theme overrides.
const ghPlugin = `
local gvim = require 'gvim'

gvim.command.register{ name = "popup", run = function()
  local id = gvim.ui.action_popup{
    title = "Update", message = "A new version is available.",
    actions = { { id = "install", label = "Install" }, { id = "later", label = "Later" } },
  }
  gvim.notify("chose:" .. tostring(id))
end }

gvim.command.register{ name = "ask", run = function()
  local v = gvim.ui.input{ prompt = "File:", on_change = function(text)
    local ok, err = gvim.ui.set_prompt_suggestions({
      { text = text .. "a" },
      { text = text .. "b", value = text .. "B" },
    }, 2)
    if not ok then gvim.notify("suggest:" .. tostring(err)) end
  end }
  gvim.notify("answer:" .. tostring(v))
end }

gvim.command.register{ name = "override", run = function()
  local ok, err = gvim.theme.override{ ["ui.accent_bg"] = "#112233", ["editor.fg"] = "Red" }
  if not ok then gvim.notify("theme:" .. tostring(err)) end
  gvim.notify(gvim.json.encode(gvim.theme.overrides()))
end }

gvim.command.register{ name = "clear", run = function()
  gvim.theme.clear()
  gvim.notify("cleared:" .. tostring(next(gvim.theme.overrides()) == nil))
end }
`

func TestActionPopupWaitAndEvent(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"gh.lua": ghPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("gh.popup", nil); err != nil {
		t.Fatal(err)
	}
	ov := h.Host.UI().Last("action_popup")
	if ov == nil {
		t.Fatal("no action popup was requested")
	}
	if ov.ActionPopup.Title != "Update" || len(ov.ActionPopup.Actions) != 2 || ov.ActionPopup.Actions[0].ID != "install" {
		t.Fatalf("popup = %+v", ov.ActionPopup)
	}
	// The plugin hears the result event, and the call returns the chosen id.
	h.Emit(pluginapi.Event{Name: "action_popup_result", Data: map[string]any{"popup": uint64(ov.ID), "action": "install"}})
	h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"install"}})
	notices := h.NoticeTexts()
	if len(notices) == 0 || notices[len(notices)-1] != "chose:install" {
		t.Fatalf("notices = %q", notices)
	}
	// Closing without a choice returns nil.
	if err := h.Run("gh.popup", nil); err != nil {
		t.Fatal(err)
	}
	ov = h.Host.UI().Last("action_popup")
	h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{nil}})
	notices = h.NoticeTexts()
	if notices[len(notices)-1] != "chose:nil" {
		t.Fatalf("notices = %q", notices)
	}
}

func TestPromptSuggestionsFromOnChange(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"gh.lua": ghPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("gh.ask", nil); err != nil {
		t.Fatal(err)
	}
	in := h.Host.UI().Last("input")
	if in == nil {
		t.Fatal("no prompt was opened")
	}
	in.Input.OnChange("x")
	h.Settle()
	got := h.Host.UI().Suggestions
	if len(got) != 2 || got[0].Text != "xa" || got[1].Value != "xB" || h.Host.UI().Selected != 1 {
		t.Fatalf("suggestions = %+v selected %d", got, h.Host.UI().Selected)
	}
	h.Resolve(pluginapi.Completion{Token: in.Token, Values: []any{"xa"}})
	notices := h.NoticeTexts()
	if len(notices) == 0 || notices[len(notices)-1] != "answer:xa" {
		t.Fatalf("notices = %q", notices)
	}
}

func TestThemeOverrides(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"gh.lua": ghPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("gh.override", nil); err != nil {
		t.Fatal(err)
	}
	over := h.Host.FakeTheme().Overrides
	if over["ui.accent_bg"] != "#112233" || over["editor.fg"] != "Red" {
		t.Fatalf("overrides = %+v", over)
	}
	notices := h.NoticeTexts()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "ui.accent_bg") {
		t.Fatalf("overrides() = %q", notices)
	}
	if err := h.Run("gh.clear", nil); err != nil {
		t.Fatal(err)
	}
	if len(h.Host.FakeTheme().Overrides) != 0 {
		t.Fatalf("overrides survived clear: %+v", h.Host.FakeTheme().Overrides)
	}
	if notices := h.NoticeTexts(); notices[len(notices)-1] != "cleared:true" {
		t.Fatalf("notices = %q", notices)
	}
}
