package plugin_test

import (
	"strings"
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

const gutterStylePlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "go", run = function()
  local buf = gvim.buf.current()
  local ns = gvim.decor.namespace("g")
  gvim.notify("set:" .. tostring(ns:set(buf, {
    { kind = "virtual_line_above", line = 1, text = "x", gutter_glyph = "+", gutter_style = { fg = "#00ff00" } },
  })))
  local _, err = gvim.decor.add_virtual_line(buf, 1, "y", { gutter_color = "#nothex" })
  gvim.notify("bad:" .. tostring(err))
end }
`

// TestVirtualLineGutterStyleKeepsItsColor checks gutter_style.fg survives when no
// gutter_color is given, and a bad gutter_color is an error, not silently dropped.
func TestVirtualLineGutterStyleKeepsItsColor(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"g.lua": gutterStylePlugin}, plugintest.Options{})
	h.Start()
	id := h.Host.AddBuffer("/w/a.go", "a\nb\n")
	if err := h.Run("g.go", nil); err != nil {
		t.Fatal(err)
	}
	items := h.Host.Decor().Items("g", "g", id)
	if len(items) != 1 || !items[0].GutterStyle.FG.Set || items[0].GutterStyle.FG.G != 255 {
		t.Fatalf("items = %+v, want the gutter_style fg kept", items)
	}
	joined := ""
	for _, n := range h.NoticeTexts() {
		joined += n + "|"
	}
	if !containsAll(joined, "set:true|", "bad:invalid") {
		t.Fatalf("notices = %q, want the bad gutter_color refused", joined)
	}
}

// TestDecorConvenienceOptions checks that the opts of the convenience functions are optional
// (a missing table used to be a Go nil dereference) and that a bad value is refused, not
// dropped silently.
func TestDecorConvenienceOptions(t *testing.T) {
	tests := []struct {
		name string
		call string
		want string // "ok" or the start of the error
	}{
		{"virtual line without opts", `gvim.decor.add_virtual_line(b, 0, "t")`, "ok"},
		{"virtual line below without opts", `gvim.decor.add_virtual_line_below(b, 0, "t")`, "ok"},
		{"conceal without opts", `gvim.decor.add_conceal(b, gvim.pos(0, 0), gvim.pos(0, 2))`, "ok"},
		{"soft break without opts", `gvim.decor.add_soft_break(b, gvim.pos(0, 1))`, "ok"},
		{"virtual line bad activation", `gvim.decor.add_virtual_line(b, 0, "t", {activation = "nope"})`, `invalid: unknown activation "nope"`},
		{"virtual line bad scope", `gvim.decor.add_virtual_line(b, 0, "t", {scope = 1})`, "invalid: scope must be a table"},
		{"conceal bad style", `gvim.decor.add_conceal(b, gvim.pos(0, 0), gvim.pos(0, 2), nil, {style = 1})`, "invalid: style:"},
		{"conceal bad activation", `gvim.decor.add_conceal(b, gvim.pos(0, 0), gvim.pos(0, 2), nil, {activation = 1})`, "invalid: activation must be a string"},
		{"soft break bad position", `gvim.decor.add_soft_break(b, "bogus")`, "invalid: soft break position:"},
		{"soft break bad prefix", `gvim.decor.add_soft_break(b, gvim.pos(0, 1), {prefix = 1})`, "invalid: prefix must be a table"},
		{"cursor line overlay bad style", `gvim.decor.set_cursor_line_overlay(b, "red")`, "invalid: style:"},
		{"scrollbar bad range", `gvim.decor.set_scrollbar_markers_in_range(b, "bad", "add")`, "invalid: range:"},
		{"scrollbar bad color", `gvim.decor.set_scrollbar_markers_in_range(b, gvim.range(0, 0, 1, 0), "#12")`, "invalid: color:"},
		{"folding range bad field", `gvim.decor.set_folding_ranges(b, {{start_line = "x"}})`, "invalid: range 1:"},
		{"too many folding ranges", `gvim.decor.set_folding_ranges(b, (function() local t = {} for i = 1, 2001 do t[i] = {start_line = i, end_line = i + 1} end return t end)())`, "limit: too many folding ranges"},
		{"item bad activation", `gvim.decor.namespace("n"):set(b, {{kind = "virtual_line_above", line = 0, text = "t", activation = "nope"}})`, `invalid: item 1: unknown activation "nope"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := bufRun(t, "one\ntwo", `local r, err = `+tc.call+"\nT.spy(r and 'ok' or err)")
			got := spyText(h)
			if tc.want == "ok" {
				if got != "ok" {
					t.Fatalf("spy = %q, want ok (log %v)", got, h.LogText(""))
				}
				return
			}
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("spy = %q, want it to start with %q (log %v)", got, tc.want, h.LogText(""))
			}
		})
	}
}

// add_fold and set_folding_ranges name the fold kind the same way: `kind`.
func TestAddFoldTakesKind(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"d.lua": `local gvim = require "gvim"
gvim.command.register { name = "fold", title = "fold", run = function()
  gvim.decor.add_fold(gvim.buf.current(), 1, 3, { kind = "region", namespace = "k" })
end }`}, plugintest.Options{})
	h.Start()
	h.Host.AddBuffer("/w/a.go", "package a\n")
	if err := h.Run("d.fold", nil); err != nil {
		t.Fatal(err)
	}
	folds := h.Host.FoldRanges()
	if len(folds) != 1 || len(folds[0].Ranges) != 1 || folds[0].Ranges[0].Kind != "region" {
		t.Fatalf("folds = %+v, want one range of kind region", folds)
	}
}
