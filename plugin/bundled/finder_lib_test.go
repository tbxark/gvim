package bundled_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The shared finder library (plugin/bundled/lib/finder.lua, require "gvim.finder") through a
// probe plugin: Fresh's fuzzy ranking + caps, the grouped/deduped tree builder, and the
// prompt frontend over gvim.ui.picker (query -> Lua ranking, move/select/close semantics).

const finderProbe = `
local gvim = require "gvim"
local finder = require "gvim.finder"
local items = {
  { label = "alpha.go" },
  { label = "cmd/beta.go" },
  { label = "abc.txt" },
  { label = "zzz" },
}

gvim.command.register{ name = "rank", title = "rank", run = function()
  local out = {}
  for _, q in ipairs({ "", "ab", "cmd", "nope" }) do
    local ranked = finder.filter(items, q, function(it) return it.label end, 100)
    local labels = {}
    for _, it in ipairs(ranked) do labels[#labels + 1] = it.label end
    out[#out + 1] = q .. "=" .. table.concat(labels, ",")
  end
  -- The cap: 300 items, at most 100 rows.
  local many = {}
  for i = 1, 300 do many[i] = { label = "file" .. string.format("%03d", i) } end
  local capped = finder.filter(many, "", function(it) return it.label end)
  out[#out + 1] = "cap=" .. #capped
  gvim.notify(table.concat(out, "|"))
end }

gvim.command.register{ name = "tree", title = "tree", run = function()
  local rows = {
    { path = "/w/a.go", t = "one" },
    { path = "/w/a.go", t = "one" }, -- the same id: deduped
    { path = "/w/b.go", t = "two" },
  }
  local tree = finder.tree(rows, {
    key = function(r) return r.path .. ":" .. r.t end,
    group = function(r) return { key = "f:" .. r.path, text = r.path .. ":" } end,
    row = function(r) return { text = r.t } end,
  })
  local out = {}
  for i, node in ipairs(tree.nodes) do
    out[#out + 1] = node.depth .. ":" .. node.text .. ":" .. tostring(tree.node_map[i] ~= nil)
  end
  gvim.notify("count=" .. tree.count .. " leaf=" .. finder.first_leaf(tree) .. " " .. table.concat(out, ","))
end }

gvim.command.register{ name = "prompt", title = "prompt", run = function()
  local f = finder.new{
    title = "Probe",
    load = function() return items end,
    format = function(it) return { label = it.label } end,
    on_move = function(it) gvim.notify("move:" .. it.label) end,
    on_select = function(it, index) gvim.notify("select:" .. it.label .. ":" .. index) end,
    on_close = function(reason) gvim.notify("close:" .. reason) end,
  }
  local ok, err = f:open()
  if not ok then gvim.notify("open:" .. tostring(err)) end
end }
`

func startFinderProbe(t *testing.T) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, plugintest.Files{"probe/init.lua": finderProbe}, plugintest.Options{Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func finderLastNotice(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

func TestFinderRankingAndCaps(t *testing.T) {
	t.Parallel()
	h := startFinderProbe(t)
	if err := h.Run("probe.rank", nil); err != nil {
		t.Fatal(err)
	}
	got := finderLastNotice(h)
	for _, want := range []string{
		"=alpha.go,cmd/beta.go,abc.txt,zzz", // empty query: source order
		"ab=abc.txt",                        // consecutive/segment bonuses
		"cmd=cmd/beta.go",
		"nope=",
		"cap=100",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ranking lacks %q: %q", want, got)
		}
	}
}

func TestFinderTreeGroupingDedupe(t *testing.T) {
	t.Parallel()
	h := startFinderProbe(t)
	if err := h.Run("probe.tree", nil); err != nil {
		t.Fatal(err)
	}
	want := "count=2 leaf=2 0:/w/a.go::false,1:one:true,0:/w/b.go::false,1:two:true"
	if got := finderLastNotice(h); got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
}

func TestFinderPromptFlow(t *testing.T) {
	t.Parallel()
	h := startFinderProbe(t)
	if err := h.Run("probe.prompt", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no picker opened (log %v)", h.LogText(""))
	}
	// A live picker asks on_query when it opens: the source order is the first list.
	if len(o.Picker.Items) != 4 || o.Picker.Items[0].Text != "alpha.go" {
		t.Fatalf("opened items = %+v", o.Picker.Items)
	}
	if o.Picker.Fuzzy || !o.Picker.Live {
		t.Errorf("picker spec fuzzy=%v live=%v, want a live picker ranked in Lua", o.Picker.Fuzzy, o.Picker.Live)
	}
	// A query edit is debounced by the host and then ranked in Lua.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"ab"}, More: true})
	h.Advance(70 * time.Millisecond)
	last := o.Updates[len(o.Updates)-1]
	if last.Items == nil || len(*last.Items) != 1 || (*last.Items)[0].Text != "abc.txt" {
		t.Fatalf("query updates = %+v", last)
	}
	// The highlight move reports the source item.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{0}, More: true})
	if got := finderLastNotice(h); got != "move:abc.txt" {
		t.Errorf("move notice = %q", got)
	}
	// Choosing: on_select with the item and the 1-based index, then on_close("selected").
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	texts := h.NoticeTexts()
	if len(texts) < 2 || texts[len(texts)-2] != "select:abc.txt:1" || texts[len(texts)-1] != "close:selected" {
		t.Errorf("select notices = %v", texts)
	}
	// Esc cancels: on_close("cancelled") and no selection.
	if err := h.Run("probe.prompt", nil); err != nil {
		t.Fatal(err)
	}
	o = h.Host.UI().Last("picker")
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{nil}})
	if got := finderLastNotice(h); got != "close:cancelled" {
		t.Errorf("cancel notice = %q", got)
	}
}
