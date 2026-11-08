package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

// capPlugin reports the paint capability flag.
const capPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "cap", run = function()
  gvim.notify("paint:" .. tostring(gvim.has("decor_virtual_paint")))
end }
`

// TestDecorVirtualPaintFeature checks gvim.has("decor_virtual_paint") follows the host
// feature seam: true by default on the fake (B landed), false when a test turns it off.
func TestDecorVirtualPaintFeature(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"c.lua": capPlugin}, plugintest.Options{})
	h.Start()
	if err := h.Run("c.cap", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "paint:true") {
		t.Fatalf("notices = %q, want the paint capability on", got)
	}

	reduced := plugintest.New(t, plugintest.Files{"c.lua": capPlugin}, plugintest.Options{})
	reduced.Host.SetFeature("decor_virtual_paint", false)
	reduced.Start()
	if err := reduced.Run("c.cap", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(reduced.NoticeTexts(), "|"); !strings.Contains(got, "paint:false") {
		t.Fatalf("reduced notices = %q, want the paint capability off", got)
	}
}
