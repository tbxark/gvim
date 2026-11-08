package plugin_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const gvimPre = `local gvim = require "gvim" `

func gvimRows(rows []row) []row {
	out := make([]row, len(rows))
	for i, r := range rows {
		r.body = gvimPre + r.body
		out[i] = r
	}
	return out
}

func TestText(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"width", `T.spy(gvim.text.width("héllo"), gvim.text.width("日本"), gvim.text.width("e\204\129"), gvim.text.width(""))`, "5 4 1 0"},
		{"truncate", `T.spy(gvim.text.truncate("abcdef", 4, "…"), gvim.text.truncate("abc", 10), gvim.text.truncate("abcdef", 4))`, "abc… abc abcd"},
		{"truncate wide", `T.spy(gvim.text.truncate("日本語", 5, "…"))`, "日本…"},
		{"pad left", `T.spy(gvim.text.pad("ab", 5) .. "|")`, "ab   |"},
		{"pad right", `T.spy(gvim.text.pad("ab", 5, "right"))`, "   ab"},
		{"pad center", `T.spy(gvim.text.pad("ab", 5, "center") .. "|")`, " ab  |"},
		{"pad wide", `T.spy(gvim.text.pad("日", 4) .. "|")`, "日  |"},
		{"pad narrower than the text", `T.spy(gvim.text.pad("abcdef", 3))`, "abcdef"},
		{"pad bad align", `T.spy(pcall(gvim.text.pad, "a", 3, "top"))`, "~align must be"},
		{"negative width", `T.spy(gvim.text.pad("ab", -3), gvim.text.truncate("abc", -1) == "")`, "ab true"},
		{"upper lower", `T.spy(gvim.text.upper("héllo"), gvim.text.lower("HÉLLO"))`, "HÉLLO héllo"},
		{"invalid utf-8 changes only ascii", `T.spy(gvim.text.upper("a\255é") == "A\255é", gvim.text.lower("A\255É") == "a\255É")`, "true true"},
		{"trim", `T.spy("[" .. gvim.text.trim("  x y \n") .. "]")`, "[x y]"},
		{"prefix and suffix", `T.spy(gvim.text.starts_with("hello", "he"), gvim.text.starts_with("hello", "lo"), gvim.text.ends_with("hello", "lo"), gvim.text.ends_with("hello", ""))`, "true false true true"},
		{"split plain", `local p = gvim.text.split("a,b,,c", ",", true) T.spy(#p, p[3] == "", p[4])`, "4 true c"},
		{"split plain with dots", `T.spy(table.concat(gvim.text.split("a.b.c", ".", true), "|"))`, "a|b|c"},
		{"split pattern", `T.spy(table.concat(gvim.text.split("a1b22c", "%d+"), "|"))`, "a|b|c"},
		{"split pattern edges", `local p = gvim.text.split("1a2", "%d") T.spy(#p, p[1] == "", p[2], p[3] == "")`, "3 true a true"},
		{"split every char", `T.spy(#gvim.text.split("ab", "."))`, "3"},
		{"split nothing found", `local p = gvim.text.split("abc", ",") T.spy(#p, p[1])`, "1 abc"},
		{"split empty separator", `T.spy(pcall(gvim.text.split, "abc", ""))`, "~separator is empty"},
		{"split too many pieces", `local s = ("a"):rep(1000001) T.spy(pcall(gvim.text.split, s, "a", true))`, "~too many pieces"},
		{"decode latin1", `T.spy(gvim.text.decode("caf\233", "latin1"))`, "café"},
		{"decode normalizes on request", `T.spy(gvim.text.decode("a\r\nb\rc", "utf-8", {normalize = true}) == "a\nb\nc", gvim.text.decode("a\r\nb", "utf-8") == "a\r\nb")`, "true true"},
		{"decode unknown encoding", `T.spy(gvim.text.decode("x", "klingon"))`, "~nil unsupported: unknown encoding"},
	}), plugintest.Options{Timeout: 5 * time.Second})
}

func TestTextFuzzy(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"ranking and indexes", `local r = gvim.text.fuzzy("ab", {"xab", "ba", "a_b"})
			T.spy(#r, r[1].item, r[1].score, table.concat(r[1].indexes, ","), r[2].item, table.concat(r[2].indexes, ","))`, "2 xab 22 2,3 a_b 1,3"},
		{"keeps the original element", `local items = {{name = "xab", id = 7}, {name = "zzz"}}
			local r = gvim.text.fuzzy("ab", items, {key = "name"}) T.spy(#r, r[1].item == items[1], r[1].item.id)`, "1 true 7"},
		{"limit", `T.spy(#gvim.text.fuzzy("a", {"a", "aa", "aaa"}, {limit = 2}))`, "2"},
		{"no match", `T.spy(#gvim.text.fuzzy("q", {"a", "b"}))`, "0"},
		{"not a string", `T.spy(pcall(gvim.text.fuzzy, "a", {"a", {}}))`, "~item 2 is not a string"},
		{"query too long", `T.spy(gvim.text.fuzzy(("x"):rep(257), {"x"}))`, "~nil too_large: text.fuzzy: query longer than 256 bytes"},
		{"longest query", `T.spy(#gvim.text.fuzzy(("x"):rep(256), {("x"):rep(256)}))`, "1"},
		{"long items are clipped", `T.spy(#gvim.text.fuzzy("z", {("a"):rep(5000) .. "z"}), #gvim.text.fuzzy("z", {("a"):rep(4000) .. "z"}))`, "0 1"},
		{"too many items", `local t = {} for i = 1, 50001 do t[i] = "x" end T.spy(gvim.text.fuzzy("x", t))`, "~nil too_large: text.fuzzy: more than 50000 items"},
	}), plugintest.Options{Timeout: 5 * time.Second})
}

func TestTextWithoutServices(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"width falls back to the rune count", `T.spy(gvim.text.width("日本"), gvim.text.truncate("abcdef", 4, "…"))`, "2 abc…"},
		{"decode is unsupported", `T.spy(gvim.text.decode("x", "latin1"))`, "~nil unsupported: text.decode"},
		{"grep is unsupported", `T.spy(gvim.workspace.grep("x"))`, "~nil unsupported: workspace.grep"},
		{"diff finds nothing", `T.spy(#gvim.diff.lines({"a"}, {"b"}))`, "0"},
	}), plugintest.Options{Timeout: 5 * time.Second, Services: plugin.NoServices()})
}

func TestUTF8(t *testing.T) {
	luaRows(t, []row{
		{"same table everywhere", `local gvim = require "gvim" T.spy(utf8 == gvim.utf8, bit == gvim.bit)`, "true true"},
		{"len", `T.spy(utf8.len("héllo"), utf8.len(""), utf8.len("héllo", 1, 2), utf8.len("日本語", -3))`, "5 0 2 1"},
		{"len of invalid", `T.spy(utf8.len("a\255b"))`, "nil 2"},
		{"char", `T.spy(utf8.char(72, 233, 0x1F600), utf8.char() == "")`, "Hé😀 true"},
		{"char out of range", `T.spy(pcall(utf8.char, 0x110000))`, "~value out of range"},
		{"codepoint", `T.spy(utf8.codepoint("héllo", 1, -1))`, "104 233 108 108 111"},
		{"codepoint default", `T.spy(utf8.codepoint("é"))`, "233"},
		{"codepoint invalid", `T.spy(pcall(utf8.codepoint, "\255"))`, "~invalid UTF-8 code"},
		{"offset forward", `T.spy(utf8.offset("héllo", 3), utf8.offset("héllo", 1), utf8.offset("héllo", 6), utf8.offset("héllo", 7))`, "4 1 7 nil"},
		{"offset backward", `T.spy(utf8.offset("héllo", -1), utf8.offset("héllo", -4), utf8.offset("héllo", -6))`, "6 2 nil"},
		{"offset zero", `T.spy(utf8.offset("héllo", 0, 3), utf8.offset("héllo", 0, 4))`, "2 4"},
		{"offset continuation", `T.spy(pcall(utf8.offset, "héllo", 1, 3))`, "~initial position is a continuation byte"},
		{"codes", `local t = {} for p, c in utf8.codes("hé!") do t[#t + 1] = p .. ":" .. c end T.spy(table.concat(t, " "))`, "1:104 2:233 4:33"},
		{"codes invalid", `T.spy(pcall(function() for _ in utf8.codes("a\255") do end end))`, "~invalid UTF-8 code"},
		{"valid", `T.spy(utf8.valid("héllo"), utf8.valid("a\255"), utf8.valid(""))`, "true false true"},
		{"sub", `T.spy(utf8.sub("héllo", 2, 3), utf8.sub("héllo", -3), utf8.sub("héllo", 2), utf8.sub("日本語", 2, 2), utf8.sub("abc", 0, 1))`, "él llo éllo 本 a"},
		{"sub empty", `T.spy(utf8.sub("abc", 5) == "", utf8.sub("abc", 3, 2) == "", utf8.sub("abc", 2, 100))`, "true true bc"},
		{"byte_to_char", `T.spy(utf8.byte_to_char("héllo", 1), utf8.byte_to_char("héllo", 2), utf8.byte_to_char("héllo", 3), utf8.byte_to_char("héllo", 4), utf8.byte_to_char("héllo", 99), utf8.byte_to_char("héllo", 0))`, "1 2 2 3 nil nil"},
		{"char_to_byte", `T.spy(utf8.char_to_byte("héllo", 1), utf8.char_to_byte("héllo", 2), utf8.char_to_byte("héllo", 3), utf8.char_to_byte("héllo", 5), utf8.char_to_byte("héllo", 6), utf8.char_to_byte("héllo", 0))`, "1 2 4 6 nil nil"},
		{"charpattern", `T.spy(("héllo"):gsub(utf8.charpattern, "x"))`, "xxxxx 5"},
	})
}

func TestBit(t *testing.T) {
	luaRows(t, []row{
		{"band bor bxor bnot", `T.spy(bit.band(0xFF, 0x0F), bit.bor(1, 2, 4), bit.bxor(5, 3), bit.bnot(0), bit.band(0xFF, 0x3C, 0x0F))`, "15 7 6 -1 12"},
		{"shifts", `T.spy(bit.lshift(1, 4), bit.rshift(256, 4), bit.lshift(1, 31), bit.rshift(-1, 28), bit.lshift(1, 32))`, "16 16 -2147483648 15 1"},
		{"32 bits", `T.spy(bit.band(2^32 + 5, 7), bit.band(-1, 0xFFFFFFFF), bit.bor(0x80000000, 0), bit.bxor(0xFFFFFFFF, 1))`, "5 -1 -2147483648 -2"},
		{"non numbers", `T.spy(pcall(bit.band, "x", 1))`, "~bad argument"},
		{"nan and infinity are zero", `T.spy(bit.bor(0/0, 1/0, 8))`, "8"},
	})
}

func TestTime(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"now_ms is the runtime clock", `T.spy(gvim.time.now_ms())`, "1700000000000"},
		{"format", `T.spy(gvim.time.format("!%Y-%m-%d %H:%M:%S", 86400000 + 3661000))`, "1970-01-02 01:01:01"},
		{"format defaults to now", `T.spy(gvim.time.format("!%Y-%m-%d %H:%M:%S"))`, "2023-11-14 22:13:20"},
		{"format milliseconds", `T.spy(gvim.time.format("!%S|%j|%%", 61999))`, "01|001|%"},
		{"unknown directive", `T.spy(pcall(gvim.time.format, "%Q"))`, "~invalid conversion specifier '%Q'"},
		{"monotonic is a number", `T.spy(type(gvim.time.monotonic_ms()))`, "number"},
	}), plugintest.Options{Timeout: 5 * time.Second})

	// the monotonic clock follows the runtime clock
	h := start(t, `T.command("go", function() T.spy(require("gvim").time.monotonic_ms()) end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(1500 * time.Millisecond)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	sp := spy(h)
	if len(sp) != 2 {
		t.Fatalf("spy = %v", sp)
	}
	a, b := atof(sp[0]), atof(sp[1])
	if b-a != 1500 {
		t.Fatalf("monotonic_ms advanced by %v, want 1500 (%v)", b-a, sp)
	}
}

func atof(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		panic(err)
	}
	return f
}

func TestTheme(t *testing.T) {
	host := plugintest.NewFakeHost()
	ws := host.Workspace()
	ws.ThemeName, ws.Dark = "solarized", false
	ws.Colors["editor.selection_bg"] = [3]uint8{1, 2, 255}
	luaRows(t, gvimRows([]row{
		{"name and darkness", `T.spy(gvim.theme.name(), gvim.theme.is_dark())`, "solarized false"},
		{"color", `local c = gvim.theme.color("editor.selection_bg") T.spy(c.r, c.g, c.b)`, "1 2 255"},
		{"unknown color", `T.spy(gvim.theme.color("editor.nothing"))`, "nil"},
	}), plugintest.Options{Host: host})
}

func TestConfig(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.Workspace().Settings = map[string]any{"tab_size": 4, "theme": "dark", "insert_spaces": true, "secret": "x"}
	luaRows(t, gvimRows([]row{
		{"get a key", `T.spy(gvim.config.get("tab_size"), gvim.config.get("theme"), gvim.config.get("insert_spaces"))`, "4 dark true"},
		{"get all", `local c = gvim.config.get() local n = 0 for _ in pairs(c) do n = n + 1 end T.spy(n, c.tab_size, c.secret)`, "3 4 nil"},
		{"not allowlisted", `T.spy(gvim.config.get("secret"))`, "nil not_found: unknown config key 'secret'"},
		{"allowlisted but unavailable", `T.spy(gvim.config.get("line_wrap"))`, "nil not_found: config key 'line_wrap' is not available"},
		{"bad argument", `T.spy(pcall(gvim.config.get, {}))`, "~bad argument #1"},
	}), plugintest.Options{Host: host})
}

func TestWorkspace(t *testing.T) {
	host := plugintest.NewFakeHost()
	ws := host.Workspace()
	ws.RootDir = "/repo"
	ws.FileList = []string{"a.go", "b.go", "c.go"}
	ws.Loading = true
	svc := &plugintest.FakeServices{GrepHits: []pluginapi.GrepHit{
		{Path: "/repo/a.go", Line: 3, Col: 4, Text: "foo bar"},
		{Path: "/repo/b.go", Line: 0, Col: 0, Text: "Foo"},
		{Path: "/repo/c.go", Line: 9, Col: 1, Text: "xfoo"},
	}}
	luaRows(t, gvimRows([]row{
		{"root", `T.spy(gvim.workspace.root())`, "/repo"},
		{"files", `local f = gvim.workspace.files() T.spy(#f.files, f.files[1], f.loading)`, "3 a.go true"},
		{"files limit", `T.spy(#gvim.workspace.files({limit = 2}).files)`, "2"},
		{"grep", `local h = gvim.workspace.grep("foo", {case_sensitive = true})
			T.spy(#h, h[1].path, h[1].line, h[1].col, h[1].text)`, "2 /repo/a.go 3 4 foo bar"},
		{"grep ignores case by default", `T.spy(#gvim.workspace.grep("foo"))`, "3"},
		{"grep max", `T.spy(#gvim.workspace.grep("foo", {max = 1}))`, "1"},
		{"grep empty query", `T.spy(gvim.workspace.grep(""))`, "~nil invalid: grep: the query is empty"},
		{"grep bad argument", `T.spy(pcall(gvim.workspace.grep, 5))`, "~bad argument"},
	}), plugintest.Options{Host: host, Services: svc})
}
