package plugin_test

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

// luaRows runs each row's body as a command of a fresh plugin and compares what it
// reported with T.spy. A want starting with "~" only has to be contained in the report.
func luaRows(t *testing.T, rows []struct{ name, body, want string }, o ...plugintest.Options) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body, o...)
			got := spyText(h)
			if sub, ok := strings.CutPrefix(r.want, "~"); ok {
				if !strings.Contains(got, sub) {
					t.Fatalf("spy = %q, want it to contain %q\nlog: %v", got, sub, h.LogText(""))
				}
				return
			}
			if got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

type row = struct{ name, body, want string }

func TestStdlibTonumber(t *testing.T) {
	luaRows(t, []row{
		{"exponent", `T.spy(tonumber("1e2"))`, "100"},
		{"hex", `T.spy(tonumber("0x1F"), tonumber("-0x10"))`, "31 -16"},
		{"white space", `T.spy(tonumber("  12  "))`, "12"},
		{"fractions", `T.spy(tonumber(".5"), tonumber("5."), tonumber("-1.5e-3"), tonumber("1E+2"))`, "0.5 5 -0.0015 100"},
		{"junk", `T.spy(tonumber("1e"), tonumber("inf"), tonumber("nan"), tonumber("1_0"), tonumber("0x1p4"), tonumber(""), tonumber("0x"))`, "nil nil nil nil nil nil nil"},
		{"other types", `T.spy(tonumber({}), tonumber(nil), tonumber(true), tonumber(5))`, "nil nil nil 5"},
		{"bases", `T.spy(tonumber("10", 2), tonumber("zz", 36), tonumber("8", 8), tonumber("-ff", 16), tonumber("", 10))`, "2 1295 nil -255 nil"},
		{"base out of range", `T.spy(pcall(tonumber, "1", 1))`, "~base out of range"},
		{"no argument", `T.spy(pcall(tonumber))`, "~value expected"},
		{"huge is infinity", `T.spy(math.huge == 1/0, -math.huge < 0, math.huge > 1e308)`, "true true true"},
	})
}

func TestStdlibFormat(t *testing.T) {
	luaRows(t, []row{
		{"numbers", `T.spy(("%5.1f|%d|%5d|%-5d|%05d"):format(3.14159, 42.9, 42, 42, 42))`, "  3.1|42|   42|42   |00042"},
		{"integer conversions", `T.spy(("%x %X %o %c %i %u"):format(255, 255, 8, 65, 7, 3))`, "ff FF 10 A 7 3"},
		{"negative hex", `T.spy(("%X"):format(-1))`, "FFFFFFFFFFFFFFFF"},
		{"floats", `T.spy(("%e|%g|%g|%g|%g"):format(12345.678, 100000, 1e6, 0.1, 123456789))`, "1.234568e+04|100000|1e+06|0.1|1.23457e+08"},
		{"flags", `T.spy(("%.3g|%10.4f|%+d|% d"):format(1234.5678, 3.14159265, 5, 5))`, "1.23e+03|    3.1416|+5| 5"},
		{"big integer", `T.spy(("%d"):format(2^53))`, "9007199254740992"},
		{"strings", `local o = setmetatable({}, {__tostring = function() return "OBJ" end})
			T.spy(("%s|%10s|%-6s|%.2s|"):format(o, "ab", "cd", "xyz"))`, "OBJ|        ab|cd    |xy|"},
		{"nil and booleans", `T.spy(("%%|%s|%s"):format(nil, true))`, "%|nil|true"},
		{"quote", `T.spy(("%q"):format('a"b\\c\nd'))`, "\"a\\\"b\\\\c\\\nd\""},
		{"non-finite", `T.spy(("%6.2f|%f|%f"):format(1/0, -1/0, 0/0))`, "   inf|-inf|nan"},
		{"unknown conversion", `T.spy(pcall(string.format, "%y", 1))`, "~invalid option '%y'"},
		{"lone percent", `T.spy(pcall(string.format, "%"))`, "~invalid option"},
		{"missing argument", `T.spy(pcall(string.format, "%d"))`, "~bad argument #2"},
		{"not a number", `T.spy(pcall(string.format, "%d", "x"))`, "~bad argument #2"},
		{"width too long", `T.spy(pcall(string.format, "%100d", 1))`, "~invalid format"},
		{"precision too long", `T.spy(pcall(string.format, "%.100f", 1))`, "~invalid format"},
		{"too big", `T.spy(pcall(string.format, "%s", ("x"):rep(2^20 + 1)))`, "~resulting string too large"},
		{"too big in pieces", `local f, args = ("%s"):rep(1000), {} for i = 1, 1000 do args[i] = ("y"):rep(2000) end T.spy(pcall(string.format, f, unpack(args)))`, "~resulting string too large"},
		{"log uses it", `require("gvim").log.info("%5.1f|%s", 3.14159, "x") T.spy("ok")`, "ok"},
	})
	h := cmd(t, "", `require("gvim").log.info("%5.1f|%s", 3.14159, "x")`)
	if !logHas(h, "t", "  3.1|x") {
		t.Fatalf("log = %v", h.LogText(""))
	}
}

func TestStdlibStringTable(t *testing.T) {
	luaRows(t, []row{
		{"rep", `T.spy(("ab"):rep(3), ("ab"):rep(0) == "", ("ab"):rep(-5) == "", ("ab"):rep(3, ","), (""):rep(1e9) == "")`, "ababab true true ab,ab,ab true"},
		{"byte", `T.spy(("abc"):byte(1, -1))`, "97 98 99"},
		{"byte past the end", `T.spy(select("#", ("abc"):byte(10)), ("abc"):byte(-1))`, "0 99"},
		{"byte too many", `T.spy(pcall(string.byte, ("x"):rep(70000), 1, -1))`, "~string slice too long"},
		{"concat", `T.spy(table.concat({1, 2, "x", 4.5}, ", "))`, "1, 2, x, 4.5"},
		{"concat range", `T.spy(table.concat({"a", "b", "c"}, "-", 2, 3), table.concat({}) == "", table.concat({"a"}, ",", 2, 1) == "")`, "b-c true true"},
		{"concat bad element", `T.spy(pcall(table.concat, {1, {}, 3}))`, "~invalid value (at index 2) in table for 'concat'"},
		{"concat 200k", `local t = {} for i = 1, 200000 do t[i] = "x" end local c = os.clock() local s = table.concat(t) T.spy(#s, os.clock() - c < 0.1)`, "200000 true"},
		{"unpack", `T.spy(select("#", unpack({1, 2, 3})), (unpack({1, 2, 3}, 2)), unpack == table.unpack)`, "3 2 true"},
		{"unpack 5000", `T.spy(select("#", unpack({}, 1, 5000)))`, "5000"},
		{"unpack too many", `T.spy(pcall(unpack, {}, 1, 100001))`, "~too many results to unpack"},
		{"unpack past the end", `T.spy(unpack({1, 2, 3}, 2, 5))`, "2 3 nil nil"},
		{"pack", `local t = table.pack(1, nil, 3) T.spy(t.n, t[1], tostring(t[2]), t[3])`, "3 1 nil 3"},
		{"move inside", `local t = table.move({1, 2, 3}, 1, 3, 2) T.spy(t[1], t[2], t[3], t[4])`, "1 1 2 3"},
		{"move to another", `local d = table.move({1, 2, 3}, 2, 3, 1, {}) T.spy(d[1], d[2], d[3])`, "2 3 nil"},
		{"sort", `local t = {5, 2, 8, 1, 9, 3} table.sort(t) T.spy(table.concat(t, ","))`, "1,2,3,5,8,9"},
		{"sort descending", `local t = {5, 2, 8, 1, 9, 3} table.sort(t, function(a, b) return a > b end) T.spy(table.concat(t, ","))`, "9,8,5,3,2,1"},
		{"sort strings", `local t = {"b", "a", "c", "B"} table.sort(t) T.spy(table.concat(t, ","))`, "B,a,b,c"},
		{"sort mixed", `T.spy(pcall(table.sort, {1, "a", 2}))`, "~attempt to compare"},
		{"sort comparator error keeps the table", `local t = {3, 1, 2} pcall(table.sort, t, function() error("x") end) T.spy(t[1], t[2], t[3])`, "3 1 2"},
		{"sort 100k", `local t = {} for i = 1, 100000 do t[i] = (i * 7919) % 100003 end table.sort(t)
			local ok = true for i = 2, #t do if t[i - 1] > t[i] then ok = false end end T.spy(ok, #t)`, "true 100000"},
		{"sort by metamethod", `local mt = {__lt = function(a, b) return a.v < b.v end}
			local t = {setmetatable({v = 2}, mt), setmetatable({v = 1}, mt)} table.sort(t) T.spy(t[1].v, t[2].v)`, "1 2"},
	}, plugintest.Options{Timeout: 2 * time.Second})
}

func TestStdlibOS(t *testing.T) {
	luaRows(t, []row{
		{"only four functions", `local names = {} for k in pairs(os) do names[#names + 1] = k end table.sort(names) T.spy(table.concat(names, ","))`, "clock,date,difftime,time"},
		{"nothing else", `T.spy(type(os.clock()), os.getenv, os.execute, os.remove, os.exit, io)`, "number nil nil nil nil nil"},
		{"runtime clock", `T.spy(os.time(), os.date("!%Y-%m-%d %H:%M:%S"))`, "1700000000 2023-11-14 22:13:20"},
		{"date", `T.spy(os.date("!%Y-%m-%d %H:%M:%S", 86400 + 3661))`, "1970-01-02 01:01:01"},
		{"names", `T.spy(os.date("!%j %a %A %b %B %p %y %%", 0))`, "001 Thu Thursday Jan January AM 70 %"},
		{"c locale", `T.spy(os.date("!%c", 0))`, "Thu Jan  1 00:00:00 1970"},
		{"more directives", `T.spy(os.date("!%x %X %I %e %u %w %U %W %F %T %R", 0))`, "01/01/70 00:00:00 12  1 4 4 00 00 1970-01-01 00:00:00 00:00"},
		{"table", `local t = os.date("!*t", 86400 * 365) T.spy(t.year, t.month, t.day, t.hour, t.yday, t.wday)`, "1971 1 1 0 1 6"},
		{"unknown directive", `T.spy(pcall(os.date, "%Q"))`, "~invalid conversion specifier '%Q'"},
		{"trailing percent", `T.spy(pcall(os.date, "abc%"))`, "~invalid conversion specifier"},
		{"difftime", `T.spy(os.difftime(10, 4))`, "6"},
		{"time needs fields", `T.spy(pcall(os.time, {year = 2020, month = 1}))`, "~field 'day' missing in date table"},
	})
	want := time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local).Unix()
	h := cmd(t, "", `T.spy(os.time{year = 2020, month = 1, day = 2, hour = 3, min = 4, sec = 5})`)
	if got := spyText(h); got != strconv.FormatInt(want, 10) {
		t.Fatalf("os.time = %s, want %d", got, want)
	}
	// hour defaults to 12 and out-of-range fields are normalised, like C's mktime
	want = time.Date(2020, 3, 1, 12, 0, 0, 0, time.Local).Unix()
	h = cmd(t, "", `local t = {year = 2020, month = 2, day = 30} T.spy(os.time(t), t.month, t.day)`)
	if got := spyText(h); got != strconv.FormatInt(want, 10)+" 3 1" {
		t.Fatalf("os.time normalised = %s, want %d 3 1", got, want)
	}
}

func TestStdlibPatterns(t *testing.T) {
	luaRows(t, []row{
		{"frontier find", `T.spy(("THE (quick) fox"):find("%f[%a]%a+"))`, "1 3"},
		{"frontier gsub", `T.spy(("hello world"):gsub("%f[%w]%w+", "<%0>"))`, "<hello> <world> 2"},
		{"find empty with init", `T.spy(("abc"):find("", 3))`, "3 2"},
		{"find init", `T.spy(("abcabc"):find("b", 3))`, "5 5"},
		{"find plain", `T.spy(("a.c"):find(".", 1, true))`, "2 2"},
		{"find captures", `T.spy(("key=val"):find("(%w+)=(%w+)"))`, "1 7 key val"},
		{"position captures", `T.spy(("abc"):find("()b()"))`, "2 2 2 3"},
		{"find no match", `T.spy(("abc"):find("x+"))`, "nil"},
		{"match", `T.spy(("2024-05-17"):match("(%d+)-(%d+)-(%d+)"))`, "2024 05 17"},
		{"match whole", `T.spy(("hello"):match("l+"), ("x"):match("y"))`, "ll nil"},
		{"match init", `T.spy(("abcabc"):match("a(.)", 2))`, "b"},
		{"gmatch pairs", `local t = {} for k, v in ("a=1, b=2"):gmatch("(%w+)=(%w+)") do t[#t + 1] = k .. v end T.spy(table.concat(t, ","))`, "a1,b2"},
		{"gmatch empty matches", `local n = 0 for _ in ("abc"):gmatch("x*") do n = n + 1 end T.spy(n)`, "4"},
		{"gmatch across coroutines", `local it = ("a b c"):gmatch("%a") local co = coroutine.wrap(function() return it() end) T.spy(co(), it())`, "a b"},
		{"gsub string", `T.spy(("hello world"):gsub("o", "0"))`, "hell0 w0rld 2"},
		{"gsub captures", `T.spy(("abc"):gsub("(a)(b)", "%2%1"))`, "bac 1"},
		{"gsub percent", `T.spy(("abc"):gsub("b", "%%"))`, "a%c 1"},
		{"gsub table", `T.spy(("$a $b"):gsub("%$(%w+)", {a = "1"}))`, "1 $b 2"},
		{"gsub function", `T.spy(("1 2 3"):gsub("%d", function(d) return d * 2 end))`, "2 4 6 3"},
		{"gsub function keeps on false", `T.spy(("abc"):gsub("%w", function(c) if c == "b" then return false end return c:upper() end))`, "AbC 3"},
		{"gsub limit", `T.spy(("aaa"):gsub("a", "b", 2))`, "bba 2"},
		{"gsub anchor", `T.spy(("aaa"):gsub("^a", "b"))`, "baa 1"},
		{"gsub bad replacement", `T.spy(pcall(string.gsub, "abc", "b", function() return {} end))`, "~invalid replacement value (a table)"},
		{"gsub bad template", `T.spy(pcall(string.gsub, "abc", "b", "%2"))`, "~invalid capture index"},
		{"balanced", `T.spy(("f(a(b)c) d"):match("%b()"))`, "(a(b)c)"},
		{"malformed", `T.spy(pcall(string.find, "a", "[a"))`, "~malformed pattern"},
		{"unfinished capture", `T.spy(pcall(string.find, "a", "("))`, "~unfinished capture"},
		{"bad argument", `T.spy(pcall(string.find, "a"))`, "~bad argument #2"},
		{"pattern too long", `T.spy(pcall(string.find, "a", ("a?"):rep(200)))`, "~pattern too long"},
		{"subject too long", `local s = ("x"):rep(17 * 1024 * 1024) T.spy(pcall(string.find, s, "x+y"))`, "~subject too long"},
		{"plain search has no limit", `local s = ("x"):rep(17 * 1024 * 1024) T.spy(s:find("y"), s:find("xy", 1, true))`, "nil nil"},
		{"catastrophic", `local s = ("a"):rep(30) .. "b" local c = os.clock()
			local ok, e = pcall(string.find, s, ("a*"):rep(10) .. "c")
			T.spy(ok, tostring(e):find("pattern too complex") ~= nil, os.clock() - c < 0.5, ("abc"):find("b"))`, "false true true 2 2"},
	}, plugintest.Options{Timeout: 2 * time.Second})
}

func TestPatternCancelledByDeadline(t *testing.T) {
	// A pattern that fails slowly over a 16 MiB subject is cut by the slice deadline, long
	// before its step budget, and the state stays usable.
	h := start(t, `
T.command("go", function()
  local s = ("x"):rep(16 * 1024 * 1024)
  T.spy("built")
  s:find("[%a_][%w_]*%(")
  T.spy("not reached")
end)
T.command("ok", function() T.spy("alive", ("abc"):find("b")) end)`, plugintest.Options{Timeout: 60 * time.Millisecond})
	d := timed(t, h)
	if !noticeHas(h, "timed out") {
		t.Fatalf("notices = %v log = %v", h.NoticeTexts(), h.LogText(""))
	}
	limit := time.Second
	if stdlibRace {
		limit = 5 * time.Second
	}
	if d > limit {
		t.Fatalf("the find ran %v past a 60 ms budget", d)
	}
	if got := spyText(h); got != "built" {
		t.Fatalf("spy = %q", got)
	}
	if err := h.Run("t.ok", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "built|alive 2 2" {
		t.Fatalf("spy = %q", got)
	}
}

// capRow is one row of TestStdlibCaps (AMENDMENTS C5): the call must fail with the stated
// text, in under 100 ms, without allocating much. The body times the call under test with
// os.clock and reports `os.clock() - c < LIMIT`; building its inputs is not part of that.
type capRow struct {
	name string
	body string
	want string // contained in the report
}

func TestStdlibCaps(t *testing.T) {
	rows := []capRow{
		{name: "concat_64MiB_x1000", want: "true true",
			body: `local s = ("x"):rep(64 * 1024 * 1024) local t = {} for i = 1, 1000 do t[i] = s end
				local c = os.clock() local ok, e = pcall(table.concat, t)
				T.spy(not ok and e:find("resulting string too large") ~= nil, os.clock() - c < LIMIT)`},
		{name: "rep_overflow", want: "true true true true true",
			body: `local function bad(...) local ok, e = pcall(...) return not ok and e:find("resulting string too large") ~= nil end
				local c = os.clock()
				T.spy(bad(string.rep, "x", 2^31), bad(string.rep, "abc", 2^53), bad(string.rep, "x", math.huge), bad(string.rep, "ab", 2^30, ","), os.clock() - c < LIMIT)`},
		// %q checks the argument before it builds the quoted string.
		{name: "fmt_q", want: "true true",
			body: `local s = ("\""):rep(32 * 1024 * 1024)
				local c = os.clock() local ok, e = pcall(string.format, "%q", s)
				T.spy(not ok and e:find("resulting string too large") ~= nil, os.clock() - c < LIMIT)`},
		// A separator is repeated at memory speed, not one WriteString at a time.
		{name: "rep_sep_in_range", want: "67108863 33554431 true",
			body: `local c = os.clock() local a = string.rep("", 2^26, "x") local b = string.rep("x", 2^24, ",")
				T.spy(#a, #b, os.clock() - c < LIMIT)`},
		{name: "gsub_long_repl_16MiB", want: "true true",
			body: `local r = ("x"):rep(16 * 1024 * 1024)
				local c = os.clock() local ok, e = pcall(string.gsub, "abcde", ".", r)
				T.spy(not ok and e:find("resulting string too large") ~= nil, os.clock() - c < LIMIT)`},
		// Matching the 1 MiB subject costs about 40 ms of CPU even though nothing is expanded, and
		// os.clock counts the CPU of the whole process, so this row gets three times the limit:
		// an expansion done before the check builds 100 MiB, which the allocation bound below
		// catches whatever the clock says.
		{name: "re_gsub_dollar1_expansion", want: "true true",
			body: `local s = ("a"):rep(1024 * 1024)
				local c = os.clock() local ok, e = pcall(re.gsub, s, "(a+)", ("$1"):rep(100))
				T.spy(not ok and e:find("resulting string too large") ~= nil, os.clock() - c < 3 * LIMIT)`},
		{name: "text_pad_2pow31", want: "true true true",
			body: `local gvim = require "gvim" local c = os.clock()
				local ok1, e1 = pcall(gvim.text.pad, "x", 2^31) local ok2, e2 = pcall(gvim.text.truncate, "x", 2^31)
				T.spy(not ok1 and e1:find("width too large") ~= nil, not ok2 and e2:find("width too large") ~= nil, os.clock() - c < LIMIT)`},
		{name: "os_date_huge_format", want: "true true true",
			body: `local gvim = require "gvim" local f = ("%Y"):rep(1048576) local c = os.clock()
				local ok1, e1 = pcall(os.date, f) local ok2, e2 = pcall(gvim.time.format, f)
				T.spy(not ok1 and e1:find("format too long") ~= nil, not ok2 and e2:find("format too long") ~= nil, os.clock() - c < LIMIT)`},
		// The nodes cap fires after a million elements, however long the array. 40 MiB keeps the
		// allocation of the test itself (the string and its copy) well under the bound; the next
		// row checks the 64 MiB input cap.
		{name: "json_decode_64MiB_array", want: "nil json: too many nodes true",
			body: `local s = table.concat({"[", ("0,"):rep(20 * 1024 * 1024), "0]"})
				local c = os.clock() local v, e = json.decode(s) T.spy(v, e, os.clock() - c < LIMIT)`},
		{name: "json_decode_over_64MiB", want: "nil limit: json: input larger than 67108864 bytes true",
			body: `local a = ("x"):rep(40 * 1024 * 1024) local s = a .. a
				local c = os.clock() local v, e = json.decode(s) T.spy(v, e, os.clock() - c < LIMIT)`},
		{name: "json_decode_depth_1000", want: "nil json: too deep true",
			body: `local s = ("["):rep(1000) .. ("]"):rep(1000)
				local c = os.clock() local v, e = json.decode(s) T.spy(v, e, os.clock() - c < LIMIT)`},
		{name: "json_encode_cycle", want: "true true",
			body: `local t = {} t.a = t local c = os.clock() local ok, e = pcall(json.encode, t)
				T.spy(not ok and e:find("circular") ~= nil, os.clock() - c < LIMIT)`},
		{name: "sort_millions", want: "true true",
			body: `local t = {} for i = 1, 6000 do t[i] = i end
				local n = 6000 while n < 600000 do local m = math.min(n, 600000 - n) table.move(t, 1, m, n + 1) n = n + m end
				local c = os.clock() local ok, e = pcall(table.sort, t)
				T.spy(not ok and e:find("table too large to sort") ~= nil, os.clock() - c < LIMIT)`},
	}
	limit := "0.1"
	if stdlibRace {
		limit = "2"
	}
	// The cap checks are deterministic (the error text, and the allocation bound below); the
	// 100 ms is the part that depends on the machine, because a scheduler stall on a busy
	// machine can add more than that to a call that did no work. A row therefore passes
	// when any of three runs meets all of its assertions: a cap that is checked only after
	// the work was done fails every run, on any machine.
	const attempts = 3
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			body := strings.ReplaceAll(r.body, "LIMIT", limit)
			var problems []string
			for range attempts {
				problems = problems[:0]
				h := start(t, "T.command('go', function() "+body+" end)", plugintest.Options{Timeout: 10 * time.Second})
				var before, after runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&before)
				d := timed(t, h)
				runtime.ReadMemStats(&after)
				if got := spyText(h); !strings.Contains(got, r.want) {
					problems = append(problems, fmt.Sprintf("spy = %q, want it to contain %q\nlog: %v", got, r.want, h.LogText("")))
				}
				if d > 5*time.Second {
					problems = append(problems, fmt.Sprintf("took %v", d))
				}
				if grown := after.TotalAlloc - before.TotalAlloc; grown > 200<<20 && !stdlibRace {
					problems = append(problems, fmt.Sprintf("allocated %d MiB, want under 200", grown>>20))
				}
				if len(problems) == 0 {
					return
				}
			}
			t.Fatalf("%d runs in a row failed: %s", attempts, strings.Join(problems, "; "))
		})
	}
}

func TestStdlibSortCancel(t *testing.T) {
	// sort_cancel: a 500k-element sort (the largest allowed) is cut by the deadline, and the
	// state stays usable. The table is built with table.move doublings, in the main chunk,
	// whose load budget is four times the command's: on a loaded machine the building must not
	// eat the budget of the sort.
	budget := 40 * time.Millisecond
	if stdlibRace {
		budget = 400 * time.Millisecond
	}
	h := start(t, `
local t = {}
for i = 1, 5000 do t[i] = (i * 7919) % 5003 end
local n = 5000
while n < 500000 do
  local m = math.min(n, 500000 - n)
  table.move(t, 1, m, n + 1)
  n = n + m
end
T.spy("built", #t)
T.command("go", function()
  table.sort(t)
  T.spy("not reached")
end)
T.command("ok", function() T.spy("alive") end)`, plugintest.Options{Timeout: budget})
	d := timed(t, h)
	over := d - budget
	t.Logf("the deadline was hit %v after the %v budget", over, budget)
	if !noticeHas(h, "timed out") {
		t.Fatalf("notices = %v log = %v (took %v)", h.NoticeTexts(), h.LogText(""), d)
	}
	// How promptly a sort notices the deadline is a count of comparisons, asserted without a
	// clock by TestSortPollsTheContextEveryFewComparisons. Here the wall clock only has to show
	// that the sort was cut at all, so the bound is generous enough for a loaded machine.
	if limit := 5 * time.Second; over > limit {
		t.Fatalf("the sort ran %v past the deadline, want under %v", over, limit)
	}
	if got := spyText(h); got != "built 500000" {
		t.Fatalf("spy = %q (an empty report means the building loop used up the budget)", got)
	}
	if err := h.Run("t.ok", nil); err != nil {
		t.Fatal(err)
	}
}

func TestLoadBudgetCapped(t *testing.T) {
	// The load budget is min(4 x plugin_timeout_ms, 5 s): a user value of 60 s does not give
	// a main chunk a minute.
	rows := []struct {
		timeout time.Duration
		load    time.Duration
		event   time.Duration
	}{
		{60 * time.Second, 5 * time.Second, 50 * time.Millisecond},
		{2 * time.Second, 5 * time.Second, 50 * time.Millisecond},
		{1 * time.Second, 4 * time.Second, 50 * time.Millisecond},
		{500 * time.Millisecond, 2 * time.Second, 50 * time.Millisecond},
		{10 * time.Millisecond, 40 * time.Millisecond, 10 * time.Millisecond},
	}
	for _, r := range rows {
		b := plugin.Budgets(plugin.New(plugin.Options{Timeout: r.timeout}))
		if b["load"] != r.load || b["event"] != r.event || b["command"] != r.timeout {
			t.Errorf("timeout %v: budgets = %v, want load %v event %v command %v", r.timeout, b, r.load, r.event, r.timeout)
		}
	}
}
