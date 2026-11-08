package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// tier1cDecorPlugin exercises the tier-1c-A decoration surface: virtual lines with text
// overlays, conceal, soft breaks, cursor-line overlay, scrollbar ranges, plugin folds,
// clears by range/namespace and refresh_lines.
const tier1cDecorPlugin = `
local gvim = require 'gvim'

gvim.command.register{ name = "decorate", run = function()
  local buf = gvim.buf.current()
  local ns = gvim.decor.namespace("t1c")
  gvim.notify("vline:" .. tostring(gvim.decor.add_virtual_line(buf, 2, "removed old line", {
    above = true, gutter_glyph = "-", gutter_color = "error", priority = 3,
    style = { fg = "#ff0000", bold = true },
    text_overlays = { { start = 0, ["end"] = 7, style = { bold = true, underline = true } } },
  })))
  gvim.notify("vbelow:" .. tostring(gvim.decor.add_virtual_line_below(buf, 4, "added line", {})))
  gvim.notify("conceal:" .. tostring(gvim.decor.add_conceal(buf,
    { line = 5, col = 0 }, { line = 5, col = 10 }, "…", { activation = "unless-cursor-in" })))
  gvim.notify("soft:" .. tostring(gvim.decor.add_soft_break(buf, { line = 6, col = 0 },
    { indent = 4, prefix = { text = "↳ ", style = { fg = "dim" } } })))
  gvim.notify("cursor:" .. tostring(gvim.decor.set_cursor_line_overlay(buf, { bg = "#202020" })))
  gvim.notify("scroll:" .. tostring(gvim.decor.set_scrollbar_markers_in_range(buf,
    { from = { line = 1, col = 0 }, to = { line = 3, col = 0 } }, "error", {})))
  gvim.notify("fold:" .. tostring(gvim.decor.add_fold(buf, 10, 20, { placeholder = "20 lines", collapsed = true })))
  gvim.notify("ranges:" .. tostring(gvim.decor.set_folding_ranges(buf, {
    { start_line = 30, end_line = 40, kind = "function", placeholder = "fn" },
  })))
  gvim.notify("refresh:" .. tostring(gvim.decor.refresh_lines(buf)))
  gvim.notify("clear:" .. tostring(gvim.decor.clear_decorations(buf,
    { from = { line = 100, col = 0 }, to = { line = 200, col = 0 } }, { namespace = "t1c" })))
  gvim.notify("clearfolds:" .. tostring(gvim.decor.clear_folds(buf, { namespace = "t1c" })))
  -- ns:set still accepts the new item kinds directly.
  gvim.notify("set:" .. tostring(ns:set(buf, {
    { kind = "conceal", range = { from = { line = 0, col = 0 }, to = { line = 0, col = 2 } }, text = "x" },
    { kind = "soft_break", pos = { line = 1, col = 0 }, indent = 2 },
  })))
end }
`

func TestTier1CDecorations(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"d.lua": tier1cDecorPlugin}, plugintest.Options{})
	h.Start()
	id := h.Host.AddBuffer("/w/a.go", "package a\n")
	if err := h.Run("d.decorate", nil); err != nil {
		t.Fatal(err)
	}
	notices := h.NoticeTexts()
	joined := ""
	for _, n := range notices {
		joined += n + "|"
	}
	if !containsAll(joined, "vline:1|", "vbelow:2|", "conceal:3|", "soft:4|", "cursor:5|", "scroll:6|", "fold:true|", "ranges:true|", "refresh:true|", "clear:true|", "clearfolds:true|", "set:true|") {
		t.Fatalf("notices = %q", joined)
	}
	// Six convenience adds made it to the host (the trailing ns:set then replaced the
	// namespace's items with its two new-kind items, which we check below).
	if got := h.Host.CallCount("DecorAdd"); got != 6 {
		t.Fatalf("DecorAdd calls = %d, want 6", got)
	}
	if got := len(h.Host.FoldRanges()); got != 1 {
		t.Fatalf("fold ranges = %d", got)
	}
	// The last ns:set replaced the namespace's items with the two new-kind items.
	items := h.Host.Decor().Items("d", "t1c", id)
	if len(items) != 2 || items[0].Kind != pluginapi.DecorConceal || items[1].Kind != pluginapi.DecorSoftBreak || items[1].Indent != 2 {
		t.Fatalf("ns:set items = %+v", items)
	}
	if h.Host.CallCount("DecorRefreshLines") != 1 {
		t.Fatalf("refresh calls = %d", h.Host.CallCount("DecorRefreshLines"))
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
