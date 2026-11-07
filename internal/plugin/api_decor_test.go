package plugin_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func rgb(r, g, b uint8) pluginapi.Color { return pluginapi.Color{R: r, G: g, B: b, Set: true} }
func key(k string) pluginapi.Color      { return pluginapi.Color{Key: k} }

func prng(l1, c1, l2, c2 int) pluginapi.Range {
	return pluginapi.Range{From: pluginapi.Pos{Line: l1, Col: c1}, To: pluginapi.Pos{Line: l2, Col: c2}}
}

func TestDecorItemsReachTheHost(t *testing.T) {
	tests := []struct {
		name string
		item string
		want pluginapi.Decoration
	}{
		{"highlight", `{kind="highlight", range=gvim.range(1, 2, 1, 5), style={fg="#ff0000", bold=true, extend=true}, priority=3}`,
			pluginapi.Decoration{Kind: pluginapi.DecorHighlight, Range: prng(1, 2, 1, 5), Style: pluginapi.Style{FG: rgb(255, 0, 0), Bold: true, Extend: true}, Priority: 3}},
		{"line", `{kind="line", line=2, style={bg={10, 20, 30}, italic=true}}`,
			pluginapi.Decoration{Kind: pluginapi.DecorLine, Line: 2, Style: pluginapi.Style{BG: rgb(10, 20, 30), Italic: true}}},
		{"eol", `{kind="eol", line=0, text="  <- note", style={fg="editor.line_number_fg", underline=true, strike=true, reverse=true}}`,
			pluginapi.Decoration{Kind: pluginapi.DecorEOL, Text: "  <- note", Style: pluginapi.Style{FG: key("editor.line_number_fg"), Underline: true, Strike: true, Reverse: true}}},
		{"gutter", `{kind="gutter", line=1, text="+", style={fg="add"}}`,
			pluginapi.Decoration{Kind: pluginapi.DecorGutter, Line: 1, Text: "+", Style: pluginapi.Style{FG: key("add")}}},
		{"scroll of a line", `{kind="scroll", line=3, color="change"}`,
			pluginapi.Decoration{Kind: pluginapi.DecorScroll, Line: 3, Color: key("change")}},
		{"scroll of a range", `{kind="scroll", range=gvim.range(1, 0, 3, 0), color={r=1, g=2, b=3}}`,
			pluginapi.Decoration{Kind: pluginapi.DecorScroll, Range: prng(1, 0, 3, 0), Color: rgb(1, 2, 3)}},
		{"replace", `{kind="replace", pos=gvim.pos(0, 1), text="x", style={bg="#00ff00"}}`,
			pluginapi.Decoration{Kind: pluginapi.DecorReplace, Pos: pluginapi.Pos{Col: 1}, Text: "x", Style: pluginapi.Style{BG: rgb(0, 255, 0)}}},
		{"no style, no priority", `{kind="line", line=0}`, pluginapi.Decoration{Kind: pluginapi.DecorLine}},
		{"extra fields are the plugin's own", `{kind="line", line=0, note="mine"}`, pluginapi.Decoration{Kind: pluginapi.DecorLine}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, host := bufRun(t, "one\ntwo\nthree\nfour", `
local ns = gvim.decor.namespace("blame")
T.spy(ns:set(b, {`+tc.item+`}))`)
			if got := spyText(h); got != "true" {
				t.Fatalf("spy = %q, log %v", got, h.LogText(""))
			}
			got := host.Decor().Items("t", "blame", 1)
			if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("host got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecorInvalidItemsAreRefusedWhole(t *testing.T) {
	tests := []struct {
		name string
		item string
		want string // the start of the error
	}{
		{"not a table", `"nope"`, "invalid: item 2: an item must be a table"},
		{"no kind", `{line=1}`, "invalid: item 2: an item needs a string 'kind'"},
		{"unknown kind", `{kind="underline", line=1}`, `invalid: item 2: unknown kind "underline"`},
		{"highlight without a range", `{kind="highlight"}`, "invalid: item 2: highlight 'range'"},
		{"highlight with a bad range", `{kind="highlight", range={from=1, to=2}}`, "invalid: item 2: highlight 'range'"},
		{"line without a line", `{kind="line"}`, "invalid: item 2: line needs a numeric 'line'"},
		{"eol without text", `{kind="eol", line=1}`, "invalid: item 2: eol needs a string 'text'"},
		{"eol with huge text", `{kind="eol", line=1, text=("x"):rep(2000)}`, "invalid: item 2: eol text is longer than 1024 bytes"},
		{"replace without a pos", `{kind="replace", text="x"}`, "invalid: item 2: replace 'pos'"},
		{"scroll with neither", `{kind="scroll"}`, "invalid: item 2: scroll needs a numeric 'line'"},
		{"bad hex color", `{kind="line", line=1, style={fg="#12"}}`, "invalid: item 2: style fg: color \"#12\" is not #rrggbb"},
		{"bad hex digits", `{kind="line", line=1, style={bg="#gggggg"}}`, "invalid: item 2: style bg: color \"#gggggg\" is not #rrggbb"},
		{"component out of range", `{kind="line", line=1, style={fg={300, 0, 0}}}`, "invalid: item 2: style fg: color component 300 is outside 0..255"},
		{"short color table", `{kind="line", line=1, style={fg={1, 2}}}`, "invalid: item 2: style fg: color table needs three numbers"},
		{"color of the wrong type", `{kind="line", line=1, style={fg=true}}`, "invalid: item 2: style fg: a color is a string or a table"},
		{"style of the wrong type", `{kind="line", line=1, style="bold"}`, "invalid: item 2: style must be a table"},
		{"flag of the wrong type", `{kind="line", line=1, style={bold="yes"}}`, "invalid: item 2: style: option 'bold' must be a boolean"},
		{"priority of the wrong type", `{kind="line", line=1, priority="high"}`, "invalid: item 2: option 'priority' must be a number"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, host := bufRun(t, "a\nb\nc", `
local ns = gvim.decor.namespace("blame")
T.spy(ns:set(b, {{kind="line", line=0}}))
local ok, e = ns:set(b, {{kind="line", line=2}, `+tc.item+`})
T.spy(tostring(ok), tostring(e))`)
			got := spy(h)
			if len(got) != 2 || !strings.HasPrefix(got[1], "nil "+tc.want) {
				t.Fatalf("spy = %q, want an error starting %q (log %v)", got, tc.want, h.LogText(""))
			}
			// The failed call changed nothing, and did not reach the host.
			items := host.Decor().Items("t", "blame", 1)
			if len(items) != 1 || items[0].Line != 0 {
				t.Fatalf("host holds %+v after a refused set, want the first one", items)
			}
			if host.CallCount("DecorSet") != 1 {
				t.Fatalf("DecorSet called %d times", host.CallCount("DecorSet"))
			}
		})
	}
}

func TestDecorSetAddRemoveClear(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "one\ntwo")
	host.AddBuffer("b.go", "x")
	h, _ := bufRun(t, "", `
local a, bb = gvim.buf.get(1), gvim.buf.get(2)
local ns, other = gvim.decor.namespace("n"), gvim.decor.namespace("other")
T.spy(ns:set(a, {{kind="line", line=0}, {kind="line", line=1}}))
local id1 = ns:add(a, {kind="gutter", line=0, text="+"})
local id2 = ns:add(a, {kind="gutter", line=1, text="-"})
T.spy(type(id1), id1 ~= id2)
T.spy(ns:set(bb, {{kind="line", line=0}}), other:set(a, {{kind="eol", line=0, text="x"}}))
T.spy(ns:remove(a, id1))
ns:clear(a)
T.spy("cleared")
ns:clear()
T.spy("cleared all")`, plugintest.Options{Host: host})
	if got := spyText(h); got != "true|number true|true true|true|cleared|cleared all" {
		t.Fatalf("spy = %q, log %v", got, h.LogText(""))
	}
	d := host.Decor()
	if got := d.Items("t", "n", 1); len(got) != 0 {
		t.Fatalf("namespace n on buffer 1 holds %+v after clear", got)
	}
	if got := d.Items("t", "n", 2); len(got) != 0 {
		t.Fatalf("clear() with no buffer left %+v on buffer 2", got)
	}
	if got := d.Items("t", "other", 1); len(got) != 1 {
		t.Fatalf("another namespace was cleared: %+v", got)
	}
	// Remove took the id Add returned, in this plugin's namespace and buffer.
	var removed []string
	for _, c := range host.Calls() {
		if c.Method == "DecorRemove" {
			removed = append(removed, fmt.Sprint(c.Args))
		}
	}
	if len(removed) != 1 || !strings.HasPrefix(removed[0], "[t n 1 ") {
		t.Fatalf("DecorRemove calls = %q", removed)
	}
}

func TestDecorSetReplacesEverything(t *testing.T) {
	h, host := bufRun(t, "a\nb\nc", `
local ns = gvim.decor.namespace("n")
ns:set(b, {{kind="line", line=0}, {kind="line", line=1}})
ns:add(b, {kind="line", line=2})
ns:set(b, {{kind="eol", line=1, text="only"}})`)
	_ = h
	got := host.Decor().Items("t", "n", 1)
	if len(got) != 1 || got[0].Kind != pluginapi.DecorEOL {
		t.Fatalf("after the second set the host holds %+v, want only the eol", got)
	}
}

func TestDecorNamespaceQuotaAndNames(t *testing.T) {
	h, _ := bufRun(t, "a", `
local names = {}
for i = 1, 64 do
  names[i] = gvim.decor.namespace("ns" .. i)
end
T.spy(#names, tostring(names[1]))
-- an old name again is not a new namespace
T.spy(tostring(gvim.decor.namespace("ns1")))
local x, e = gvim.decor.namespace("one too many")
T.spy(tostring(x), e)
local y, e2 = gvim.decor.namespace("")
T.spy(tostring(y), e2)
local z, e3 = gvim.decor.namespace(("n"):rep(200))
T.spy(tostring(z), e3)`)
	want := []string{
		"64 Namespace(ns1)",
		"Namespace(ns1)",
		"nil limit: namespace quota (64)",
		"nil invalid: namespace name must be 1 to 128 bytes",
		"nil invalid: namespace name must be 1 to 128 bytes",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q", got, want)
	}
}

func TestDecorNamespacesArePerPlugin(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	h := plugintest.New(t, plugintest.Files{
		"p1.lua": `local gvim = require "gvim" gvim.decor.namespace("n"):set(gvim.buf.get(1), {{kind="line", line=0}})`,
		"p2.lua": `local gvim = require "gvim" gvim.decor.namespace("n"):set(gvim.buf.get(1), {{kind="eol", line=0, text="2"}})`,
	}, plugintest.Options{Host: host})
	h.Start()
	d := host.Decor()
	if len(d.Items("p1", "n", 1)) != 1 || len(d.Items("p2", "n", 1)) != 1 {
		t.Fatalf("entries = %+v", d.Entries)
	}
	if d.Items("p1", "n", 1)[0].Kind != pluginapi.DecorLine || d.Items("p2", "n", 1)[0].Kind != pluginapi.DecorEOL {
		t.Fatalf("one plugin's set replaced the other's: %+v", d.Entries)
	}
}

func TestDecorTooManyItems(t *testing.T) {
	h, host := bufRun(t, "a", `
local ns = gvim.decor.namespace("n")
local items = {}
for i = 1, 20000 do items[i] = {kind="line", line=0} end
T.spy(ns:set(b, items))
items[20001] = {kind="line", line=0}
local ok, e = ns:set(b, items)
T.spy(tostring(ok), e)`, plugintest.Options{Timeout: 20 * time.Second}) // building 20000 tables is slow under -race
	if got := spy(h); len(got) != 2 || got[0] != "true" || got[1] != "nil limit: too many items (max 20000 per namespace)" {
		t.Fatalf("spy = %q, log %v", got, h.LogText(""))
	}
	if host.CallCount("DecorSet") != 1 {
		t.Fatalf("the oversized set reached the host")
	}
}

func TestDecorSetStaleOnUneditedBuffer(t *testing.T) {
	// The old version=0 hole, for decorations: a token taken on a buffer that was never
	// edited must still see a later edit.
	h, host := bufRun(t, "abc", `
local ns = gvim.decor.namespace("n")
local tok = b:version()
T.spy(ns:set(b, {{kind="line", line=0}}, {version = tok}))
b:insert(gvim.pos(0, 0), "x")
local ok, e = ns:set(b, {{kind="line", line=0}, {kind="line", line=0}}, {version = tok})
T.spy(tostring(ok), gvim.err_code(e))
local id, e2 = ns:add(b, {kind="line", line=0}, {version = tok})
T.spy(tostring(id), gvim.err_code(e2))
T.spy(ns:set(b, {{kind="line", line=0}}, {version = b:version()}))
T.spy(ns:set(b, {{kind="line", line=0}}), type(ns:add(b, {kind="line", line=0}, {version = nil})))`)
	if got := spyText(h); got != "true|nil stale|nil stale|true|true number" {
		t.Fatalf("spy = %q, log %v", got, h.LogText(""))
	}
	if n := len(host.Decor().Items("t", "n", 1)); n != 2 {
		t.Fatalf("the host holds %d items, want the 1 + 1 of the last two calls", n)
	}
}

func TestDecorBadVersionTokenIsReturned(t *testing.T) {
	// AMENDMENTS C4: a malformed token is `nil, "invalid: bad version token"` for ns:set and
	// ns:add, as for b:edit; it must not raise.
	h, host := bufRun(t, "a", `
local ns = gvim.decor.namespace("n")
for _, bad in ipairs({"", "1", "x.y", "garbage", "1.2.3", 7, true}) do
  local ok, v, e = pcall(ns.set, ns, b, {}, {version = bad})
  T.spy("set", tostring(ok), tostring(v), tostring(e))
  local ok2, id, e2 = pcall(ns.add, ns, b, {kind="line", line=0}, {version = bad})
  T.spy("add", tostring(ok2), tostring(id), tostring(e2))
end`)
	got := spy(h)
	if len(got) != 14 {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
	for i, l := range got {
		want := "set true nil invalid: bad version token"
		if i%2 == 1 {
			want = "add true nil invalid: bad version token"
		}
		if l != want {
			t.Fatalf("row %d = %q, want %q", i, l, want)
		}
	}
	if host.CallCount("DecorSet")+host.CallCount("DecorAdd") != 0 {
		t.Fatal("a bad token reached the host")
	}
}

func TestDecorOnAClosedBuffer(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	host.AddBuffer("b.go", "y")
	h := start(t, `
local gvim = require "gvim"
local ns = gvim.decor.namespace("n")
local dead
T.command("check", function()
  local ok, e = ns:set(dead, {{kind="line", line=0}})
  T.spy(tostring(ok), gvim.err_code(e))
  local id, e2 = ns:add(dead, {kind="line", line=0})
  T.spy(tostring(id), gvim.err_code(e2))
  T.spy(ns:remove(dead, 1), ns:clear(dead))
end)
dead = gvim.buf.get(2)`, plugintest.Options{Host: host})
	host.RemoveBuffer(2)
	if err := h.Run("t.check", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "nil closed|nil closed|true true" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestDecorUnloadClearsWhatThePluginOwns(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	before := host.Resources()
	h := plugintest.New(t, plugintest.Files{
		"p.lua": `local gvim = require "gvim"
gvim.decor.namespace("a"):set(gvim.buf.get(1), {{kind="line", line=0}})
gvim.decor.namespace("b"):add(gvim.buf.get(1), {kind="gutter", line=0, text="+"})`,
	}, plugintest.Options{Host: host})
	h.Start()
	if got := host.Resources(); !slices.Contains(got, "decor:p/a@1") || !slices.Contains(got, "decor:p/b@1") {
		t.Fatalf("resources = %v", got)
	}
	h.Unload("p")
	if got := host.Resources(); !slices.Equal(got, before) {
		t.Fatalf("resources after unload = %v, want %v", got, before)
	}
}

func TestDecorIsAFeature(t *testing.T) {
	h := start(t, `local gvim = require "gvim" T.spy(gvim.has("decor"), type(gvim.decor.namespace))`)
	if got := spyText(h); got != "true function" {
		t.Fatalf("spy = %q", got)
	}
}
