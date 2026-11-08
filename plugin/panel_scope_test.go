package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// scopePlugin mounts float panels with each chrome scope and an invalid one, so the Lua
// mount option is checked end to end.
const scopePlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "scopes", run = function()
  local window = gvim.ui.mount_widget_panel({ kind = "label", text = "w" }, { scope = "window" })
  local editor = gvim.ui.mount_widget_panel({ kind = "label", text = "e" }, { scope = "editor" })
  local buffer = gvim.ui.mount_widget_panel({ kind = "label", text = "b" }, { scope = "buffer" })
  local bad = gvim.ui.mount_widget_panel({ kind = "label", text = "x" }, { scope = "pane" })
  gvim.notify("mounts:" .. tostring(window) .. ":" .. tostring(editor) .. ":" .. tostring(buffer) .. ":" .. tostring(bad))
end }
`

// TestPanelMountScopeOption checks the scope option is parsed, defaulted and validated by
// the Lua host surface.
func TestPanelMountScopeOption(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"scopes.lua": scopePlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("scopes.scopes", nil); err != nil {
		t.Fatal(err)
	}
	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 3 {
		t.Fatalf("mounted = %d, want window+editor+buffer (bad scope refused)", len(mounted))
	}
	scopes := make([]pluginapi.WidgetScope, len(mounted))
	for i, p := range mounted {
		scopes[i] = p.Mount.Scope
	}
	if scopes[0] != pluginapi.ScopeWindow || scopes[1] != pluginapi.ScopeEditor || scopes[2] != pluginapi.ScopeBuffer {
		t.Fatalf("scopes = %v", scopes)
	}
	notices := h.NoticeTexts()
	last := notices[len(notices)-1]
	if !strings.Contains(last, "mounts:") || strings.Contains(last, "mounts::") {
		t.Fatalf("mount notices = %q", last)
	}
}
