package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// themeFilesPlugin exercises the theme-file APIs and the text-properties helper.
const themeFilesPlugin = `
local gvim = require 'gvim'

gvim.command.register{ name = "themes", run = function()
  gvim.notify("all:" .. tostring(#gvim.theme.all()))
  gvim.notify("dir:" .. gvim.theme.user_dir())
  gvim.notify("exists:" .. tostring(gvim.theme.file_exists("mine.json")))
  local data = gvim.theme.data("dark")
  gvim.notify("data:" .. tostring(data ~= nil and #data > 0))
  local ref, err = gvim.theme.save_file("mine", '{"name":"mine"}')
  if ref == nil then gvim.notify("save:" .. tostring(err)) return end
  gvim.notify("save:" .. ref)
  gvim.notify("exists2:" .. tostring(gvim.theme.file_exists("mine.json")))
  gvim.notify("delete:" .. tostring(gvim.theme.delete_file("mine.json")))
  gvim.notify("reload:" .. tostring(gvim.theme.reload("mine.json")))
  local schema = gvim.theme.schema()
  gvim.notify("schema:" .. tostring(#schema.keys) .. ":" .. tostring(#schema.modifier_only) .. ":" .. tostring(#schema.named_colors))
end }

gvim.command.register{ name = "props", run = function()
  local props = gvim.buf.text_properties_at_cursor()
  if props == nil then gvim.notify("none") return end
  gvim.notify("prop:" .. tostring(props.file) .. ":" .. tostring(props.line))
end }
`

func TestThemeFileAPIs(t *testing.T) {
	host := plugintest.NewFakeHost()
	theme := host.FakeTheme()
	theme.Themes = []string{"dark", "light"}
	theme.UserDir = "/home/u/.config/gvim/themes"
	theme.Data = map[string]string{"dark": "{\"name\":\"dark\"}"}
	theme.Schema = pluginapi.ThemeSchema{
		Keys:         []pluginapi.ThemeKeyInfo{{Name: "editor.bg", Kind: "required"}, {Name: "ui.accent_bg", Kind: "required"}},
		ModifierOnly: []string{"editor.selection_modifier"},
		NamedColors:  []string{"Red", "Blue"},
	}
	h := plugintest.New(t, plugintest.Files{"tf.lua": themeFilesPlugin}, plugintest.Options{Host: host})
	h.Start()
	if err := h.Run("tf.themes", nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(h.NoticeTexts(), "|")
	want := "all:2|dir:/home/u/.config/gvim/themes|exists:false|data:true|save:mine.json|exists2:true|delete:true|reload:true|schema:2:1:2"
	if got != want {
		t.Fatalf("notices = %q, want %q", got, want)
	}
	if theme.Reloads != 1 || theme.Applied != "mine.json" {
		t.Fatalf("reload = %d %q", theme.Reloads, theme.Applied)
	}
	if len(theme.Saved) != 0 {
		t.Fatalf("saved = %+v, want the delete to remove it", theme.Saved)
	}
}

func TestTextPropertiesAtCursor(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"tf.lua": themeFilesPlugin}, plugintest.Options{})
	h.Start()
	id, err := h.Host.CreateBuffer(pluginapi.CreateBuffer{Plugin: "tf", Name: "panel", Text: "line\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Host.SetContent(id, []pluginapi.ContentEntry{{
		Text:  "line\n",
		Props: map[string]any{"file": "main.go", "line": 3},
	}}); err != nil {
		t.Fatal(err)
	}
	h.Host.SetActive(id)
	if err := h.Host.SetCursor(id, pluginapi.Pos{Line: 0, Col: 1}, false, false, false); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("tf.props", nil); err != nil {
		t.Fatal(err)
	}
	notices := h.NoticeTexts()
	if len(notices) == 0 || notices[len(notices)-1] != "prop:main.go:3" {
		t.Fatalf("notices = %q", notices)
	}
}
