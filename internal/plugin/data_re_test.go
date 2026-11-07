package plugin_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

func TestRE(t *testing.T) {
	luaRows(t, []row{
		{"same table everywhere", `local gvim = require "gvim" T.spy(re == gvim.re, require("gvim.re") == re)`, "true true"},
		{"find", `T.spy(re.find("hello world", "o w"))`, "5 7"},
		{"find captures", `T.spy(re.find("key=val", "(\\w+)=(\\w+)"))`, "1 7 key val"},
		{"find init", `T.spy(re.find("abcabc", "b", 3))`, "5 5"},
		{"find negative init", `T.spy(re.find("abcabc", "b", -2))`, "5 5"},
		{"find past the end", `T.spy(re.find("abc", "b", 10))`, "nil"},
		{"find no match", `T.spy(re.find("abc", "x+"))`, "nil"},
		{"find group that did not match", `T.spy(re.find("ac", "(a)(b)?"))`, "1 1 a false"},
		{"match groups", `T.spy(re.match("2024-05-17", "(\\d+)-(\\d+)"))`, "2024 05"},
		{"match whole", `T.spy(re.match("hello", "l+"), re.match("x", "y"))`, "ll nil"},
		{"test", `T.spy(re.test("abc", "b"), re.test("abc", "^b"))`, "true false"},
		{"gmatch", `local t = {} for k, v in re.gmatch("a=1,b=2", "(\\w)=(\\d)") do t[#t + 1] = k .. v end T.spy(table.concat(t, ","))`, "a1,b2"},
		{"gmatch whole", `local n = 0 for m in re.gmatch("a1b22c333", "\\d+") do n = n + #m end T.spy(n)`, "6"},
		{"gmatch empty matches", `local n = 0 for _ in re.gmatch("abc", "x*") do n = n + 1 end T.spy(n)`, "4"},
		{"gsub groups", `T.spy(re.gsub("John Smith", "(\\w+) (\\w+)", "$2, $1"))`, "Smith, John 1"},
		{"gsub named", `T.spy(re.gsub("John Smith", "(?P<first>\\w+) (?P<last>\\w+)", "${last} ${first}"))`, "Smith John 1"},
		{"gsub dollars", `T.spy(re.gsub("a", "(a)", "$$|${1}x|$9|$"))`, "$|ax||$ 1"},
		{"gsub table", `T.spy(re.gsub("$a $b", "\\$(\\w+)", {a = "1"}))`, "1 $b 2"},
		{"gsub function", `T.spy(re.gsub("1 2 3", "\\d", function(d) return d * 2 end))`, "2 4 6 3"},
		{"gsub function keeps on false", `T.spy(re.gsub("abc", "\\w", function(c) if c == "b" then return false end return c:upper() end))`, "AbC 3"},
		{"gsub limit", `T.spy(re.gsub("aaa", "a", "b", 2))`, "bba 2"},
		{"gsub no match", `T.spy(re.gsub("abc", "x", "y"))`, "abc 0"},
		{"gsub empty matches", `T.spy(re.gsub("abc", "x*", "-"))`, "-a-b-c- 4"},
		{"gsub bad replacement", `T.spy(pcall(re.gsub, "a", "a", function() return {} end))`, "~invalid replacement value (a table)"},
		{"gsub bad argument", `T.spy(pcall(re.gsub, "a", "a", true))`, "~string/function/table expected"},
		{"split", `T.spy(table.concat(re.split("a, b,c", ",\\s*"), "|"))`, "a|b|c"},
		{"split limit", `T.spy(table.concat(re.split("a,b,c", ",", 2), "|"))`, "a|b,c"},
		{"split edges", `local a, b = re.split("", ","), re.split(",a", ",") T.spy(#a, a[1] == "", #b, b[1] == "", b[2])`, "1 true 2 true a"},
		{"escape", `T.spy(re.escape("a.b*c"))`, `a\.b\*c`},
		{"flags", `T.spy(re.compile("HELLO", "i"):test("say hello"), re.compile("^b", "m"):test("a\nb"), re.compile("a.b", "s"):test("a\nb"), re.test("a\nb", "a.b"))`, "true true true false"},
		{"bad flag", `T.spy(pcall(re.compile, "a", "x"))`, "~flags may contain i, m and s"},
		{"regex methods", `local rx = re.compile("(\\d+)-(\\d+)")
			T.spy(rx:find("x 10-20"))
			T.spy(rx:match("10-20"))
			T.spy(rx:test("none"))
			T.spy(rx:gsub("10-20 3-4", "$2-$1"))
			T.spy(table.concat(rx:split("a1-2b3-4c"), "|"))
			local n = 0 for a in rx:gmatch("1-2 3-4") do n = n + a end T.spy(n)`, "3 7 10 20|10 20|false|20-10 4-3 2|a|b|c|4"},
		{"tostring", `T.spy(tostring(re.compile("a+")))`, `Regex("a+")`},
		{"invalid pattern", `T.spy(re.compile("("))`, "~nil invalid: error parsing regexp: missing closing )"},
		{"invalid pattern in find", `T.spy(select(2, re.find("a", "[")))`, "~invalid: error parsing regexp: missing closing ]"},
		{"gmatch invalid pattern", `T.spy(pcall(re.gmatch, "a", "("))`, "~true nil invalid: error parsing regexp"},
		{"pattern too long", `T.spy(select(2, re.compile(("a"):rep(5000))))`, "~limit: pattern longer than 4096 bytes"},
		{"wrong pattern type", `T.spy(pcall(re.find, "a", 5))`, "~pattern string or Regex expected"},
		{"too many matches", `local s = ("a"):rep(1000001) T.spy(select(2, re.gsub(s, "a", "b")))`, "~limit: more than 1000000 matches"},
		{"too many pieces", `local s = ("a"):rep(1000001) T.spy(select(2, re.split(s, "a")))`, "~limit: more than 1000000 pieces"},
	}, plugintest.Options{Timeout: 60 * time.Second})
}

// TestREWorkCap: work = len(subject) x len(program) must stay within 4e7, otherwise the call
// is refused up front; a literal pattern only has the subject cap (AMENDMENTS C7).
func TestREWorkCap(t *testing.T) {
	const limitText = "limit: subject too large for this pattern"
	rows := []struct {
		name, body, want string
	}{
		{"expanding counted repeat", `local s = ("a"):rep(1048576) local c = os.clock() local ok, e = re.test(s, "(?:[a-z]+\\s*){1,50}!") T.spy(ok, e, os.clock() - c < LIMIT)`,
			"nil " + limitText + "; use b:find_lines/b:find or scan line by line true"},
		{"nested repeat", `local s = ("a"):rep(1048576) local c = os.clock() local ok, e = re.find(s, "(?s)(.*a){20}z") T.spy(ok, e:sub(1, 41), os.clock() - c < LIMIT)`,
			"nil " + limitText + " true"},
		{"literal on 16 MiB", `local s = ("x"):rep(16 * 1024 * 1024 - 3) .. "foo" T.spy(re.find(s, "foo"))`, "16777214 16777216"},
		{"word boundary on 1 MiB", `local s = ("x "):rep(524285) .. "foo" T.spy(re.find(s, "\\bfoo\\b"))`, "1048571 1048573"},
		{"subject over 16 MiB", `local s = ("x"):rep(16 * 1024 * 1024) .. "y" T.spy(select(2, re.find(s, "foo")))`, "limit: subject larger than 16777216 bytes"},
		{"split and gsub are covered too", `local s = ("a"):rep(1048576) T.spy(select(2, re.split(s, "(?:[a-z]+\\s*){1,50}!")) ~= nil, select(2, re.gsub(s, "(?s)(.*a){20}z", "")) ~= nil)`, "true true"},
		{"many groups on a long subject", `local s = ("a"):rep(39000) T.spy(select(2, re.gsub(s, ("()"):rep(500), "")))`,
			"~limit: more than 3992 matches"},
		{"many groups, gmatch raises", `local s = ("a"):rep(39000) T.spy(pcall(re.gmatch, s, ("()"):rep(500)))`,
			"~limit: more than 3992 matches"},
		{"many groups with an explicit count", `local s = ("a"):rep(39000) T.spy(select(2, re.gsub(s, ("()"):rep(500), "", 10)))`, "10"},
		{"gmatch raises", `local s = ("a"):rep(1048576) T.spy(pcall(re.gmatch, s, "(?s)(.*a){20}z"))`, "~false"},
	}
	limit := "0.005" // a refusal is O(1): 5 ms
	if stdlibRace {
		limit = "0.2"
	}
	// A refusal is decided by the size of the program and the subject, and is checked by the
	// error text; the 5 ms only says it did no work. A scheduler stall on a busy machine can add
	// more than that to a call that did none, so a row passes when any of three runs does.
	const attempts = 3
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			want := strings.TrimPrefix(r.want, "~")
			var failure string
			for range attempts {
				h := cmd(t, "", strings.ReplaceAll(r.body, "LIMIT", limit), plugintest.Options{Timeout: 10 * time.Second})
				got := spyText(h)
				switch {
				case strings.HasPrefix(r.want, "~") && strings.Contains(got, want), got == want:
					return
				case strings.HasPrefix(r.want, "~"):
					failure = fmt.Sprintf("spy = %q, want it to contain %q", got, want)
				default:
					failure = fmt.Sprintf("spy = %q, want %q\nlog: %v", got, want, h.LogText(""))
				}
			}
			t.Fatalf("%d runs in a row failed: %s", attempts, failure)
		})
	}
}

func TestREProgramTooLarge(t *testing.T) {
	luaRows(t, []row{
		{"over 2000 instructions", `T.spy(re.compile("a{1000}b{1000}c"))`, "~nil limit: pattern too complex"},
		{"just under", `T.spy(type(re.compile("a{1000}b{900}")))`, "userdata"},
		{"find reports it too", `T.spy(select(2, re.find("a", "a{1000}b{1000}c")))`, "~limit: pattern too complex"},
	})
}
