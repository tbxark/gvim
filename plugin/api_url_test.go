package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

const urlPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "open", run = function()
  local ok, err = gvim.url.open("https://example.com/owner/repo")
  gvim.notify("open:" .. tostring(ok) .. ":" .. tostring(err))
end }
`

// TestURLPluginAPISurface checks gvim.url.open reaches the host with the URL.
func TestURLPluginAPISurface(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"url.lua": urlPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("url.open", nil); err != nil {
		t.Fatal(err)
	}
	if got := h.Host.URLs(); len(got) != 1 || got[0] != "https://example.com/owner/repo" {
		t.Fatalf("host URLs = %v, notices %v", got, h.NoticeTexts())
	}
	found := false
	for _, n := range h.NoticeTexts() {
		found = found || strings.Contains(n, "open:true")
	}
	if !found {
		t.Fatalf("notices = %v, want open:true", h.NoticeTexts())
	}
}
