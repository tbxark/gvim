package plugin_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// Virtual buffers (stage S18): gvim.buf.create and the methods set_content, props_at,
// set_read_only, set_mode, set_option and get_option, against the fake host.

// virtualRows runs each row's body with b = the buffer a.go (text "regular") and compares the
// spy lines joined by "|".
func virtualRows(t *testing.T, rows []bufRow) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			text := r.text
			if text == "" {
				text = "regular"
			}
			h, _ := bufRun(t, text, r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestBufVirtualCreate(t *testing.T) {
	virtualRows(t, []bufRow{
		{name: "defaults", body: `
local v = gvim.buf.create{name = "out", text = "a\nb"}
T.spy(v:is_virtual(), v:name(), tostring(v:path()), v:line_count(), v:is_read_only(), v:is_modified(), v:is_large())
T.spy(tostring(v:language()), tostring(v:info().mode), v:info().active, b:is_virtual())`,
			want: "true *out* nil 2 false false false|nil nil false false"},
		{name: "all options", body: `
local v = gvim.buf.create{name = "log", text = "x", language = "go", read_only = true, mode = "m1", focus = true}
T.spy(v:language(), v:is_read_only(), v:info().mode, v:info().active, gvim.buf.current() == v)`,
			want: "go true m1 true true"},
		{name: "the buffer is in the list and the handle is stable", body: `
local v = gvim.buf.create{name = "n"}
T.spy(#gvim.buf.list(), gvim.buf.list()[2] == v, gvim.buf.get(v.id) == v, v:valid())`,
			want: "2 true true true"},
		{name: "no name", body: `T.spy(gvim.buf.create{})`, want: "nil invalid: a virtual buffer needs a name"},
		{name: "blank name", body: `T.spy(gvim.buf.create{name = "  "})`, want: "nil invalid: a virtual buffer needs a name"},
		{name: "text over 16 MiB", body: `local v, e = gvim.buf.create{name = "n", text = ("x"):rep(17 * 1024 * 1024)} T.spy(tostring(v), gvim.err_code(e))`,
			want: "nil too_large"},
		{name: "mode name too long", body: `local v, e = gvim.buf.create{name = "n", mode = ("m"):rep(200)} T.spy(tostring(v), gvim.err_code(e))`,
			want: "nil invalid"},
		{name: "option of the wrong type", body: `
local ok, e = pcall(gvim.buf.create, {name = 5})
T.spy(ok, e:find("option 'name' must be a string", 1, true) ~= nil)`,
			want: "false true"},
		{name: "not a table", body: `T.spy((pcall(gvim.buf.create, "x")))`, want: "false"},
		{name: "a handle works with the generic methods", body: `
local v = gvim.buf.create{name = "n", text = "one\ntwo"}
T.spy(v:line(1), v:text(), tostring(v:cursor().line))
T.spy(v:edit({{range = gvim.range(0, 0, 0, 0), text = ">"}}), v:line(0), v:is_modified())`,
			want: "two one\ntwo 0|true >one false"},
	})
}

func TestBufVirtualCreatePassesThePluginName(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	h := plugintest.New(t, plugintest.Files{"owner.lua": `local gvim = require "gvim"
gvim.buf.create{name = "mine"}`}, plugintest.Options{Host: host})
	h.Start()
	var got pluginapi.CreateBuffer
	for _, c := range host.Calls() {
		if c.Method == "CreateBuffer" {
			got = c.Args[0].(pluginapi.CreateBuffer)
		}
	}
	if got.Plugin != "owner" || got.Name != "mine" {
		t.Fatalf("CreateBuffer got %+v, want plugin owner", got)
	}
	if res := host.Resources(); !slices.Contains(res, "vbuf:owner/2") {
		t.Fatalf("resources = %v", res)
	}
}

func TestBufVirtualSetContent(t *testing.T) {
	type run = plugintest.StyledRun
	bold := pluginapi.Style{Bold: true}
	red := pluginapi.Style{FG: pluginapi.Color{Set: true, R: 255}}
	tests := []struct {
		name  string
		lua   string
		text  string
		runs  []run
		props map[pluginapi.Pos]string // position -> `kind` prop of props_at
	}{
		{"concatenated verbatim", `{ {text = "a"}, {text = "b\nc\n"}, {text = "d"} }`, "ab\nc\nd", nil, nil},
		{"empty list empties the buffer", `{}`, "", nil, nil},
		{"entry style", `{ {text = "head\n", style = {bold = true}}, {text = "body"} }`, "head\nbody",
			[]run{{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 4, Style: bold}}, nil},
		{"segments replace text and sit above the entry style",
			`{ {text = "ignored", style = {bold = true}, segments = { {text = "ab", style = {fg = "#ff0000"}}, {text = "cd"} }} }`,
			"abcd", []run{
				{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 2, Style: red, Priority: 1},
				{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 4, Style: bold},
			}, nil},
		{"segments only", `{ {segments = { {text = "x"}, {text = "y\n"} }}, {text = "z"} }`, "xy\nz", nil, nil},
		{"props", `{ {text = "t\n"}, {text = "one\n", props = {kind = "a"}}, {text = "two", props = {kind = "b"}} }`, "t\none\ntwo", nil,
			map[pluginapi.Pos]string{{Line: 0, Col: 0}: "", {Line: 1, Col: 1}: "a", {Line: 2, Col: 3}: "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, host := bufRun(t, "regular", `
local v = gvim.buf.create{name = "c"}
T.spy(v:set_content(`+tt.lua+`))
T.spy(v:text())`)
			if got := spyText(h); got != "true|"+tt.text {
				t.Fatalf("spy = %q, want true|%q (log %v)", got, tt.text, h.LogText(""))
			}
			_, runs := host.VirtualContent(2)
			if !slices.Equal(runs, tt.runs) {
				t.Fatalf("styled runs = %+v, want %+v", runs, tt.runs)
			}
			for pos, want := range tt.props {
				got, err := host.PropsAt(2, pos)
				if err != nil {
					t.Fatal(err)
				}
				if kind, _ := got["kind"].(string); kind != want {
					t.Errorf("props at %v = %v, want kind %q", pos, got, want)
				}
			}
			if regular := bufContent(host, 0); regular != "regular" {
				t.Fatalf("the regular buffer changed: %q", regular)
			}
		})
	}
}

func TestBufVirtualSetContentRefusals(t *testing.T) {
	big := `("x"):rep(9 * 1024 * 1024)`
	rows := []struct{ name, body, want string }{
		{"entry is not a table", `T.spy(pcall(v.set_content, v, {"text"}))`, "false"},
		{"entry has no text", `T.spy(pcall(v.set_content, v, {{style = {bold = true}}}))`, "false"},
		{"text is not a string", `T.spy(pcall(v.set_content, v, {{text = 5}}))`, "false"},
		{"segments is not an array", `T.spy(pcall(v.set_content, v, {{segments = "x"}}))`, "false"},
		{"segment without text", `T.spy(pcall(v.set_content, v, {{segments = {{style = {}}}}}))`, "false"},
		{"props is not a table", `T.spy(pcall(v.set_content, v, {{text = "x", props = 3}}))`, "false"},
		{"props is an array", `T.spy(pcall(v.set_content, v, {{text = "x", props = {1, 2}}}))`, "false"},
		{"props has a function", `local ok, e = v:set_content({{text = "x", props = {f = print}}}) T.spy(tostring(ok), gvim.err_code(e))`, "nil invalid"},
		{"style is not a table", `T.spy(pcall(v.set_content, v, {{text = "x", style = "bold"}}))`, "false"},
		{"bad color", `T.spy(pcall(v.set_content, v, {{text = "x", style = {fg = 5}}}))`, "false"},
		{"not a table argument", `T.spy(pcall(v.set_content, v, "x"))`, "false"},
		{"too many entries", `
local entries = {}
for i = 1, 200001 do entries[i] = {text = ""} end
local ok, e = v:set_content(entries)
T.spy(tostring(ok), gvim.err_code(e))`, "nil limit"},
		{"too many segments", `
local segs = {}
for i = 1, 100001 do segs[i] = {text = ""} end
local ok, e = v:set_content({{segments = segs}})
T.spy(tostring(ok), gvim.err_code(e))`, "nil limit"},
		{"text over 16 MiB", `local ok, e = v:set_content({{text = ` + big + `}, {text = ` + big + `}}) T.spy(tostring(ok), gvim.err_code(e))`,
			"nil too_large"},
		{"segments count toward the byte cap", `local ok, e = v:set_content({{segments = {{text = ` + big + `}, {text = ` + big + `}}}}) T.spy(tostring(ok), gvim.err_code(e))`,
			"nil too_large"},
		{"props share one node budget", `
local entries = {}
local wide = {}
for i = 1, 9000 do wide["k" .. i] = i end
for i = 1, 200 do entries[i] = {text = "x\n", props = wide} end
local ok, e = v:set_content(entries)
T.spy(tostring(ok), gvim.err_code(e))`, "nil limit"},
		{"a regular buffer", `T.spy(select(2, b:set_content({})))`, "invalid: buffer 1 is not a virtual buffer"},
		{"a closed buffer", `v:close() local ok, e = v:set_content({}) T.spy(tostring(ok), gvim.err_code(e))`, "nil closed"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			// The rows that build a big content need more than the default 100 ms budget on a
			// busy machine; the limits they test are not about time.
			h, host := bufRun(t, "regular", `
local v = gvim.buf.create{name = "c", text = "keep"}
`+r.body, plugintest.Options{Timeout: 10 * time.Second})
			if got := spyText(h); !strings.HasPrefix(got, r.want) {
				t.Fatalf("spy = %q, want prefix %q (log %v)", got, r.want, h.LogText(""))
			}
			if len(host.Buffers) > 1 && bufContent(host, 1) != "keep" {
				t.Fatalf("a refused call changed the text: %q", bufContent(host, 1))
			}
		})
	}
}

func TestBufVirtualPropsAt(t *testing.T) {
	virtualRows(t, []bufRow{
		{name: "explicit position and the cursor by default", body: `
local v = gvim.buf.create{name = "o"}
v:set_content({
  {text = "title\n"},
  {text = "one\n", props = {line = 10, kind = "h1", nested = {list = {1, 2}, ok = true}}},
  {text = "two", props = {line = 20}},
})
T.spy(tostring(v:props_at(gvim.pos(0, 1))), v:props_at(gvim.pos(1, 2)).line, v:props_at(gvim.pos(2, 3)).line)
v:set_cursor(gvim.pos(1, 0))
local p = v:props_at()
T.spy(p.kind, p.nested.list[2], tostring(p.nested.ok))
v:set_cursor(gvim.pos(0, 0))
T.spy(tostring(v:props_at()))`,
			want: "nil 10 20|h1 2 true|nil"},
		{name: "a position as a plain table", body: `
local v = gvim.buf.create{name = "o"}
v:set_content({{text = "x", props = {a = 1}}})
T.spy(v:props_at({line = 0, col = 0}).a)`, want: "1"},
		{name: "props are copies", body: `
local v = gvim.buf.create{name = "o"}
v:set_content({{text = "x", props = {a = 1}}})
local p = v:props_at(gvim.pos(0, 0))
p.a = 99
T.spy(v:props_at(gvim.pos(0, 0)).a)`, want: "1"},
		{name: "a regular buffer has none", body: `T.spy(tostring(b:props_at(gvim.pos(0, 0))))`, want: "nil"},
		{name: "an edit ends the props", body: `
local v = gvim.buf.create{name = "o"}
v:set_content({{text = "x", props = {a = 1}}})
v:insert(gvim.pos(0, 1), "y")
T.spy(tostring(v:props_at(gvim.pos(0, 0))))
v:undo()
T.spy(v:props_at(gvim.pos(0, 0)).a)`, want: "nil|1"},
		{name: "a bad position", body: `
local v = gvim.buf.create{name = "o"}
T.spy((pcall(v.props_at, v, "x")))`, want: "false"},
	})
}

func TestBufVirtualReadOnlyAndMode(t *testing.T) {
	virtualRows(t, []bufRow{
		{name: "set_read_only toggles the flag and the edits follow", body: `
local v = gvim.buf.create{name = "o", text = "x"}
T.spy(v:is_read_only(), v:set_read_only(true), v:is_read_only())
local ok, e = v:insert(gvim.pos(0, 0), "a")
T.spy(tostring(ok), gvim.err_code(e))
T.spy(v:set_read_only(false), v:insert(gvim.pos(0, 0), "a"), v:text())`,
			want: "false true true|nil read_only|true true ax"},
		{name: "streaming recipe: append works between set_read_only(false) and (true)", body: `
local v = gvim.buf.create{name = "o", text = "a\n", read_only = true}
local ok, e = v:append("x\n")
T.spy(tostring(ok), gvim.err_code(e))
v:set_read_only(false)
T.spy(v:append("b\n"), v:set_read_only(true), v:text())
local ok2, e2 = v:append("c\n")
T.spy(tostring(ok2), gvim.err_code(e2))`,
			want: "nil read_only|true true a\nb\n|nil read_only"},
		{name: "set_read_only on a regular buffer", body: `local ok, e = b:set_read_only(true) T.spy(tostring(ok), e)`,
			want: "nil invalid: buffer 1 is not a virtual buffer"},
		{name: "set_read_only needs a boolean", body: `local v = gvim.buf.create{name = "o"} T.spy((pcall(v.set_read_only, v, "yes")))`, want: "false"},
		{name: "set_mode sets, changes and clears the mode of any buffer", body: `
local v = gvim.buf.create{name = "o"}
T.spy(v:set_mode("outline"), tostring(v:info().mode))
T.spy(b:set_mode("vi"), b:info().mode)
T.spy(v:set_mode(nil), tostring(v:info().mode))
T.spy(b:set_mode(), tostring(b:info().mode))`,
			want: "true outline|true vi|true nil|true nil"},
		{name: "set_mode errors", body: `
local v = gvim.buf.create{name = "o"}
local ok, e = v:set_mode(("m"):rep(200))
T.spy(tostring(ok), gvim.err_code(e), (pcall(v.set_mode, v, 5)))`, want: "nil invalid false"},
	})
}

func TestBufVirtualOptions(t *testing.T) {
	virtualRows(t, []bufRow{
		{name: "set and get each option", body: `
local v = gvim.buf.create{name = "o"}
T.spy(tostring(v:get_option("wrap")), tostring(v:get_option("line_numbers")), tostring(v:get_option("tab_size")))
T.spy(v:set_option("wrap", false), v:set_option("line_numbers", true), v:set_option("tab_size", 8))
T.spy(tostring(v:get_option("wrap")), tostring(v:get_option("line_numbers")), tostring(v:get_option("tab_size")))
T.spy(v:set_option("wrap", nil), v:set_option("tab_size"), tostring(v:get_option("wrap")), tostring(v:get_option("tab_size")))`,
			want: "nil nil nil|true true true|false true 8|true true nil nil"},
		{name: "options belong to any buffer", body: `T.spy(b:set_option("wrap", false), tostring(b:get_option("wrap")))`, want: "true false"},
		{name: "refusals", body: `
local function code(...) local v, e = ... return tostring(v) .. " " .. tostring(gvim.err_code(e)) end
T.spy(code(b:set_option("bogus", 1)))
T.spy(code(b:set_option("wrap", 1)))
T.spy(code(b:set_option("tab_size", 0)))
T.spy(code(b:set_option("tab_size", 2.5)))
T.spy(code(b:get_option("bogus")))
T.spy((pcall(b.set_option, b, "wrap", {})), (pcall(b.set_option, b, "wrap", "yes")))`,
			want: "nil invalid|nil invalid|nil invalid|nil invalid|nil invalid|false false"},
	})
}

// A virtual buffer is not a file.
func TestBufVirtualCannotBeSavedOrReverted(t *testing.T) {
	virtualRows(t, []bufRow{
		{name: "save", body: `
local v = gvim.buf.create{name = "o", text = "x"}
local ok, e = v:save()
T.spy(tostring(ok), gvim.err_code(e))
local ok2, e2 = v:save("/tmp/x.txt")
T.spy(tostring(ok2), gvim.err_code(e2))`, want: "nil invalid|nil invalid"},
		{name: "revert", body: `
local v = gvim.buf.create{name = "o", text = "x"}
local ok, e = v:revert()
T.spy(tostring(ok), gvim.err_code(e))`, want: "nil invalid"},
		{name: "saved lines", body: `
local v = gvim.buf.create{name = "o", text = "x"}
T.spy(tostring(v:diff_saved()))`, want: "nil"},
	})
}

func TestBufVirtualVersionTokens(t *testing.T) {
	virtualRows(t, []bufRow{
		{name: "set_content gives a new token and an old one is stale", body: `
local v = gvim.buf.create{name = "o", text = "a"}
local old = v:version()
v:set_content({{text = "b"}})
T.spy(v:version() ~= old)
local ok, e = v:edit({{range = gvim.range(0, 0, 0, 0), text = "x"}}, {version = old})
T.spy(tostring(ok), gvim.err_code(e))
T.spy(v:edit({{range = gvim.range(0, 0, 0, 0), text = "x"}}, {version = v:version()}), v:text())`,
			want: "true|nil stale|true xb"},
		{name: "undo does not go back past the content", body: `
local v = gvim.buf.create{name = "o", text = "a"}
v:set_content({{text = "b"}})
local ok, e = v:undo()
T.spy(tostring(ok), gvim.err_code(e), v:text())`, want: "nil not_found b"},
	})
}

func TestBufVirtualIsAFeature(t *testing.T) {
	h := start(t, `local gvim = require "gvim" T.spy(gvim.has("virtual_buffers"), type(gvim.buf.create))`)
	if got := spyText(h); got != "true function" {
		t.Fatalf("spy = %q", got)
	}
}

// Unloading a plugin closes the virtual buffers it made, and leaves the ones of others.
func TestBufVirtualUnloadClosesTheBuffersOfThePlugin(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	before := host.Resources()
	h := plugintest.New(t, plugintest.Files{
		"p.lua": `local gvim = require "gvim"
gvim.buf.create{name = "one", text = "1"}
gvim.buf.create{name = "two", text = "2"}`,
		"q.lua": `local gvim = require "gvim"
gvim.buf.create{name = "other"}`,
	}, plugintest.Options{Host: host})
	h.Start()
	if n := len(host.Buffers); n != 4 {
		t.Fatalf("%d buffers after the start, want 4", n)
	}
	if res := host.Resources(); !slices.Contains(res, "vbuf:p/2") || !slices.Contains(res, "vbuf:p/3") || !slices.Contains(res, "vbuf:q/4") {
		t.Fatalf("resources = %v", res)
	}
	h.Unload("p")
	if n := len(host.Buffers); n != 2 {
		t.Fatalf("%d buffers after p unloaded, want a.go and q's buffer", n)
	}
	if res := host.Resources(); slices.Contains(res, "vbuf:p/2") || !slices.Contains(res, "vbuf:q/4") {
		t.Fatalf("resources = %v", res)
	}
	h.Unload("q")
	if got := host.Resources(); !slices.Equal(got, before) {
		t.Fatalf("resources after both unloads = %v, want %v", got, before)
	}
}
