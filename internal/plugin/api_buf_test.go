package plugin_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// bufShow is a Lua serialiser the rows use to compare tables: sorted keys, no spaces.
const bufShow = `
local function show(v)
  if type(v) ~= "table" then return tostring(v) end
  local keys = {}
  for k in pairs(v) do keys[#keys + 1] = k end
  table.sort(keys, function(a, b) return tostring(a) < tostring(b) end)
  local out = {}
  for _, k in ipairs(keys) do out[#out + 1] = tostring(k) .. "=" .. show(v[k]) end
  return "{" .. table.concat(out, ",") .. "}"
end
`

// keepText in a row's final field means "do not check the buffer text".
const keepText = "\x00keep"

type bufRow struct {
	name  string
	text  string // content of the buffer a.go
	body  string // the command body, after `local b = gvim.buf.current()`
	want  string // T.spy lines joined by "|"
	final string // buffer text afterwards (keepText: unchecked)
}

// bufRun loads a plugin whose command "go" runs body with b = the buffer a.go, runs it and
// returns the harness. With an explicit host in the options, text is ignored.
func bufRun(t *testing.T, text, body string, o ...plugintest.Options) (*plugintest.Harness, *plugintest.FakeHost) {
	t.Helper()
	opts := plugintest.Options{}
	if len(o) > 0 {
		opts = o[0]
	}
	if opts.Host == nil {
		opts.Host = plugintest.NewFakeHost()
		opts.Host.AddBuffer("a.go", text)
	}
	h := cmd(t, bufShow, "local b = gvim.buf.current()\n"+body, opts)
	return h, opts.Host
}

func bufContent(f *plugintest.FakeHost, i int) string { return strings.Join(f.Buffers[i].Lines, "\n") }

func runBufRows(t *testing.T, rows []bufRow) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h, host := bufRun(t, r.text, r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
			if r.final != keepText && bufContent(host, 0) != r.final {
				t.Fatalf("text = %q, want %q", bufContent(host, 0), r.final)
			}
		})
	}
}

func TestBufRegistry(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("/w/a.go", "a")
	host.AddBuffer("/w/b.go", "b")
	h, _ := bufRun(t, "", `
local a, bb = gvim.buf.get(1), gvim.buf.get(2)
T.spy(tostring(gvim.buf.current()), tostring(gvim.buf.get(2)), tostring(gvim.buf.get(99)), tostring(gvim.buf.get(-1)))
T.spy(gvim.buf.current() == a, gvim.buf.current() == gvim.buf.get(1), a == bb, a.id, bb.id)
local list = gvim.buf.list()
T.spy(#list, list[1] == a, list[2] == bb)
T.spy(gvim.buf.find("/w/b.go") == bb, tostring(gvim.buf.find("/nope")))
local set = {[a] = "A"} T.spy(set[gvim.buf.get(1)])
local ok, e = pcall(function() a.id = 5 end)
T.spy(tostring(ok), tostring(e):find("attempt to modify a Buffer handle", 1, true) ~= nil)
T.spy(a:valid(), a:path(), a:name())`, plugintest.Options{Host: host})
	want := []string{
		"Buffer(1) Buffer(2) nil nil",
		"true true false 1 2",
		"2 true true",
		"true nil",
		"A",
		"false true",
		"true /w/a.go a.go",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestBufInfo(t *testing.T) {
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("/w/a.go", "l1\nl2")
	host.Buffers[0].Info.Language = "go"
	host.Buffers[0].Info.ReadOnly = true
	untitled := host.AddBuffer("", "x")
	host.Buffers[1].Info.Name = "[No Name]"
	large := host.AddBuffer("/w/big.log", "x")
	host.Buffers[2].Info.Large = true
	host.Buffers[2].Info.LineCount = -1
	h, _ := bufRun(t, "", fmt.Sprintf(`
local a, u, big = gvim.buf.get(%d), gvim.buf.get(%d), gvim.buf.get(%d)
T.spy(show(a:info()))
T.spy(show(u:info()))
T.spy(a:path(), a:name(), a:language(), a:is_modified(), a:is_read_only(), a:is_virtual(), a:is_large(), a:version(), a:line_count())
T.spy(tostring(u:path()), u:name(), tostring(u:language()))
T.spy(tostring(big:line_count()), big:is_large(), tostring(big:info().line_count))`, id, untitled, large), plugintest.Options{Host: host})
	want := []string{
		"{active=true,encoding=utf-8,id=1,language=go,large=false,line_count=2,line_ending=lf,modified=false,name=a.go,path=/w/a.go,preview=false,read_only=true,version=1.0,virtual=false}",
		"{active=false,encoding=utf-8,id=2,large=false,line_count=1,line_ending=lf,modified=false,name=[No Name],preview=false,read_only=false,version=1.0,virtual=false}",
		"/w/a.go a.go go false true false false 1.0 2",
		"nil [No Name] nil",
		"nil true nil",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestBufDeadHandle(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "x")
	host.AddBuffer("b.go", "y")
	h := cmd(t, bufShow+`
local dead
T.command("check", function()
  T.spy(dead:valid(), gvim.buf.get(2) == nil)
  for _, r in ipairs({
    function() return dead:line(0) end, function() return dead:lines() end, function() return dead:text() end,
    function() return dead:info() end, function() return dead:path() end, function() return dead:version() end,
    function() return dead:insert(gvim.pos(0, 0), "x") end, function() return dead:set_lines(0, 0, {"x"}) end,
    function() return dead:cursor() end, function() return dead:find("x") end, function() return dead:find_lines("x") end,
    function() return dead:close() end, function() return dead:show() end, function() return dead:undo() end,
    function() return dead:edit({}) end, function() return dead:undo_group(function() end) end,
    function() return dead:byte_length() end, function() return dead:selection() end,
  }) do
    local v, e = r()
    T.spy(tostring(v), tostring(gvim.err_code(e)))
  end
end)`, `dead = gvim.buf.get(2)`, plugintest.Options{Host: host})
	host.RemoveBuffer(2)
	if err := h.Run("t.check", nil); err != nil {
		t.Fatal(err)
	}
	got := spy(h)
	if len(got) != 19 || got[0] != "false true" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
	for i, l := range got[1:] {
		if l != "nil closed" {
			t.Fatalf("call %d on a dead handle gave %q, want \"nil closed\"", i, l)
		}
	}
}

func TestBasesTable(t *testing.T) {
	runBufRows(t, []bufRow{
		{"line numbers are 0-based, Lua arrays 1-based", "héllo\nworld\n\nlast", `
T.spy(b:line(0), b:line(1), tostring(b:line(4)), tostring(b:line(-1)))
local ls = b:lines()
T.spy(#ls, ls[1], ls[4])
local two = b:lines(1, 2)
T.spy(#two, two[1], two[2] == "")`,
			"héllo world nil nil|4 héllo last|2 world true", keepText},
		{"columns are byte offsets", "héllo\nworld", `
local r = b:find("llo")
T.spy(#r, r[1].from.line, r[1].from.col, r[1].to.line, r[1].to.col)
T.spy(b:text{from = gvim.pos(0, 1), to = gvim.pos(0, 3)})
b:set_cursor(gvim.pos(0, 3))
T.spy(show(b:cursor()))`,
			"1 0 3 0 6|é|{col=3,line=0}", keepText},
		{"a column inside a UTF-8 sequence rounds down to its start", "héllo", `
b:set_cursor(gvim.pos(0, 2))
T.spy(show(b:cursor()), b:offset_of(gvim.pos(0, 2)))
T.spy(b:text{from = gvim.pos(0, 0), to = gvim.pos(0, 2)})`,
			"{col=1,line=0} 1|h", keepText},
		{"offsets are 0-based bytes of the LF text", "héllo\nworld", `
T.spy(b:offset_of(gvim.pos(0, 0)), b:offset_of(gvim.pos(1, 0)), b:byte_length())
T.spy(show(b:pos_of(0)), show(b:pos_of(7)), show(b:pos_of(12)), show(b:pos_of(999)))`,
			"0 7 12|{col=0,line=0} {col=0,line=1} {col=5,line=1} {col=5,line=1}", keepText},
		{"find_lines returns 0-based line numbers in a 1-based array", "ax\nb\ncx\nd", `
local ls, nxt = b:find_lines("x")
T.spy(#ls, ls[1], ls[2], tostring(nxt))`,
			"2 0 2 nil", keepText},
		{"line_count is a count, not a last index", "a\nb\nc", `T.spy(b:line_count(), b:info().line_count)`, "3 3", keepText},
		{"version token is a string", "a", `T.spy(type(b:version()), b:version())`, "string 1.0", keepText},
		{"cursor and selection use 0-based positions", "abc\ndef", `
b:select{from = gvim.pos(0, 1), to = gvim.pos(1, 2)}
T.spy(show(b:selection()), b:selected_text())`,
			"{from={col=1,line=0},to={col=2,line=1}} bc\nde", keepText},
	})
}

func TestTextRangeEndRows(t *testing.T) {
	const doc = "one\ntwo\nthree"
	runBufRows(t, []bufRow{
		{"mid line", doc, `T.spy(b:text{from = gvim.pos(0, 1), to = gvim.pos(0, 3)})`, "ne", keepText},
		{"end of line", doc, `T.spy(b:text{from = gvim.pos(1, 0), to = gvim.pos(1, 3)})`, "two", keepText},
		{"column past the end clamps to the line end", doc, `T.spy(b:text{from = gvim.pos(0, 0), to = gvim.pos(0, 99)})`, "one", keepText},
		{"to the start of the next line includes the newline", doc, `T.spy((b:text{from = gvim.pos(0, 0), to = gvim.pos(1, 0)}):gsub("\n", "<nl>"))`, "one<nl> 1", keepText},
		{"to the last line start", doc, `T.spy((b:text{from = gvim.pos(1, 0), to = gvim.pos(2, 0)}):gsub("\n", "<nl>"))`, "two<nl> 1", keepText},
		{"to the line count clamps to the end, no newline added", doc, `T.spy((b:text{from = gvim.pos(1, 0), to = gvim.pos(3, 0)}):gsub("\n", "<nl>"))`, "two<nl>three 1", keepText},
		{"to far past the end", doc, `T.spy((b:text{from = gvim.pos(1, 1), to = gvim.pos(1000, 0)}):gsub("\n", "<nl>"))`, "wo<nl>three 1", keepText},
		{"from past the end is empty", doc, `T.spy(#b:text{from = gvim.pos(5, 0), to = gvim.pos(6, 0)})`, "0", keepText},
		{"whole text with no argument", doc, `T.spy((b:text()):gsub("\n", "<nl>"))`, "one<nl>two<nl>three 2", keepText},
		{"text ending with a newline has a last empty line", "one\n", `T.spy(b:line_count(), #b:line(1), (b:text()):gsub("\n", "<nl>"))`, "2 0 one<nl> 1", keepText},
		{"delete uses the same rules: a whole line with its newline", doc, `T.spy(b:delete{from = gvim.pos(0, 0), to = gvim.pos(1, 0)})`, "true", "two\nthree"},
		{"delete to the end of the document", doc, `T.spy(b:delete{from = gvim.pos(1, 1), to = gvim.pos(9, 9)})`, "true", "one\nt"},
		{"delete the last line leaves the newline before it", "one\ntwo", `T.spy(b:delete{from = gvim.pos(1, 0), to = gvim.pos(2, 0)})`, "true", "one\n"},
		{"delete a multibyte character by its byte range", "aéb", `T.spy(b:delete{from = gvim.pos(0, 1), to = gvim.pos(0, 3)})`, "true", "ab"},
	})
}

func TestSetLinesRows(t *testing.T) {
	runBufRows(t, []bufRow{
		{"insert at 0", "a\nb\nc", `T.spy(b:set_lines(0, 0, {"x"}))`, "true", "x\na\nb\nc"},
		{"insert in the middle", "a\nb\nc", `T.spy(b:set_lines(1, 1, {"x", "y"}))`, "true", "a\nx\ny\nb\nc"},
		{"middle replace", "a\nb\nc", `T.spy(b:set_lines(1, 2, {"B1", "B2"}))`, "true", "a\nB1\nB2\nc"},
		{"replace one line with one", "a\nb\nc", `T.spy(b:set_lines(0, 1, {"A"}))`, "true", "A\nb\nc"},
		{"append at n", "a\nb\nc", `T.spy(b:set_lines(3, 3, {"x"}))`, "true", "a\nb\nc\nx"},
		{"append several at n", "a\nb\nc", `T.spy(b:set_lines(3, 3, {"x", "y"}))`, "true", "a\nb\nc\nx\ny"},
		{"delete all", "a\nb\nc", `T.spy(b:set_lines(0, 3, {}))`, "true", ""},
		{"delete last", "a\nb\nc", `T.spy(b:set_lines(2, 3, {}))`, "true", "a\nb"},
		{"delete first", "a\nb\nc", `T.spy(b:set_lines(0, 1, {}))`, "true", "b\nc"},
		{"delete middle", "a\nb\nc", `T.spy(b:set_lines(1, 2, {}))`, "true", "a\nc"},
		{"replace the tail", "a\nb\nc", `T.spy(b:set_lines(1, 3, {"y"}))`, "true", "a\ny"},
		{"replace all", "a\nb\nc", `T.spy(b:set_lines(0, 3, {"q", "r"}))`, "true", "q\nr"},
		{"bounds are clamped", "a\nb\nc", `T.spy(b:set_lines(-5, 99, {"z"}))`, "true", "z"},
		{"first after last is invalid", "a\nb\nc", `local ok, e = b:set_lines(2, 1, {"z"}) T.spy(tostring(ok), gvim.err_code(e))`, "nil invalid", "a\nb\nc"},
		{"empty insert is a no-op", "a\nb\nc", `T.spy(b:set_lines(1, 1, {}), b:set_lines(3, 3, {}))`, "true true", "a\nb\nc"},
		{"an element with a newline is text, joined as is", "a\nb\nc", `T.spy(b:set_lines(1, 1, {"p\nq"}))`, "true", "a\np\nq\nb\nc"},
		{"empty document: replace the empty line", "", `T.spy(b:set_lines(0, 1, {"x"}))`, "true", "x"},
		{"empty document: append after the empty line", "", `T.spy(b:set_lines(1, 1, {"x"}))`, "true", "\nx"},
		{"empty document: insert before the empty line", "", `T.spy(b:set_lines(0, 0, {"x"}))`, "true", "x\n"},
		{"empty document: delete everything", "", `T.spy(b:set_lines(0, 1, {}))`, "true", ""},
		{"single line: delete it", "only", `T.spy(b:set_lines(0, 1, {}))`, "true", ""},
		{"a non-string element is a programmer error", "a", `T.spy((pcall(b.set_lines, b, 0, 1, {"x", 5})))`, "false", "a"},
		{"one undo step", "a\nb\nc", `b:set_lines(0, 3, {"x", "y", "z", "w"}) T.spy(b:undo(), (b:text():gsub("\n", ",")))`, "true a,b,c", "a\nb\nc"},
	})
}

func TestBufReadLimitsAndErrors(t *testing.T) {
	host := plugintest.NewFakeHost()
	var sb strings.Builder
	for i := 0; i < 200_001; i++ {
		sb.WriteString("x\n")
	}
	host.AddBuffer("big.txt", sb.String()) // 200 002 lines
	host.AddBuffer("small.txt", "a\nb\nc")
	h, _ := bufRun(t, "", `
local big, small = gvim.buf.get(1), gvim.buf.get(2)
local function code(v, e) return tostring(v and #v) .. " " .. tostring(gvim.err_code(e)) end
T.spy(code(big:lines()))
T.spy(code(big:lines(2, 200000)))
T.spy(code(big:lines(0, 200001)))
T.spy(code(big:lines(200000)))
T.spy(code(big:lines(0, 5)))
T.spy(code(small:lines(2, 10)), code(small:lines(9)), code(small:lines(-4, 2)))
T.spy(tostring(small:line(1.9)), tostring(small:line(3)), tostring(small:line(1e12)), tostring(small:line(0/0)))`, plugintest.Options{Host: host, Timeout: 5 * time.Second})
	want := []string{
		"nil too_large",
		"200000 nil",
		"nil too_large",
		"2 nil",
		"5 nil",
		"1 nil 0 nil 2 nil",
		"b nil nil nil",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestBufLinesLargeFileUnknownCount(t *testing.T) {
	host := plugintest.NewFakeHost()
	var sb strings.Builder
	for i := 0; i < 300_000; i++ {
		sb.WriteString("y\n")
	}
	host.AddBuffer("huge.log", sb.String())
	host.Buffers[0].Info.Large = true
	host.Buffers[0].Info.LineCount = -1
	h, _ := bufRun(t, "", `
local b = gvim.buf.get(1)
local v, e = b:lines()
T.spy(tostring(v), gvim.err_code(e))
local v2 = b:lines(10, 3)
T.spy(#v2, tostring(b:line_count()))`, plugintest.Options{Host: host, Timeout: 5 * time.Second})
	if got := spyText(h); got != "nil too_large|3 nil" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestBufEditRows(t *testing.T) {
	runBufRows(t, []bufRow{
		{"insert", "hello", `T.spy(b:insert(gvim.pos(0, 5), " world"))`, "true", "hello world"},
		{"insert inside a multibyte sequence rounds down", "é!", `T.spy(b:insert(gvim.pos(0, 1), "X"))`, "true", "Xé!"},
		{"insert past the end clamps", "ab\ncd", `T.spy(b:insert(gvim.pos(1, 50), "!"), b:insert(gvim.pos(50, 0), "?"))`, "true true", "ab\ncd!?"},
		{"insert a newline", "ab", `T.spy(b:insert(gvim.pos(0, 1), "\n"))`, "true", "a\nb"},
		{"delete", "hello world", `T.spy(b:delete{from = gvim.pos(0, 5), to = gvim.pos(0, 11)})`, "true", "hello"},
		{"replace", "hello world", `T.spy(b:replace({from = gvim.pos(0, 6), to = gvim.pos(0, 11)}, "there"))`, "true", "hello there"},
		{"replace across lines", "a\nb\nc", `T.spy(b:replace({from = gvim.pos(0, 1), to = gvim.pos(2, 0)}, "-"))`, "true", "a-c"},
		{"set_text", "a\nb", `T.spy(b:set_text("x\ny\nz"))`, "true", "x\ny\nz"},
		{"set_text empty", "a\nb", `T.spy(b:set_text(""))`, "true", ""},
		{"append", "a\nb", `T.spy(b:append("!\nc"))`, "true", "a\nb!\nc"},
		{"append to an empty buffer", "", `T.spy(b:append("x"))`, "true", "x"},
		{"edit: unsorted edits in pre-edit coordinates", "abc\ndef", `
T.spy(b:edit({
  {range = {from = gvim.pos(1, 0), to = gvim.pos(1, 1)}, text = "D"},
  {range = {from = gvim.pos(0, 2), to = gvim.pos(0, 3)}, text = "C"},
  {range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = ">"},
}))`, "true", ">abC\nDef"},
		{"edit: text may be omitted for a delete", "abc", `T.spy(b:edit({{range = {from = gvim.pos(0, 1), to = gvim.pos(0, 2)}}}))`, "true", "ac"},
		{"edit: empty list is a no-op", "abc", `T.spy(b:edit({}))`, "true", "abc"},
		{"edit: adjacent edits are fine", "abcd", `T.spy(b:edit({
  {range = {from = gvim.pos(0, 0), to = gvim.pos(0, 2)}, text = "X"},
  {range = {from = gvim.pos(0, 2), to = gvim.pos(0, 4)}, text = "Y"}}))`, "true", "XY"},
		{"edit: overlapping edits are invalid and change nothing", "abcd", `
local ok, e = b:edit({
  {range = {from = gvim.pos(0, 0), to = gvim.pos(0, 3)}, text = "X"},
  {range = {from = gvim.pos(0, 2), to = gvim.pos(0, 4)}, text = "Y"}})
T.spy(tostring(ok), e)`, "nil invalid: overlapping edits", "abcd"},
		{"edit: one undo step", "abc", `
b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = "1"}, {range = {from = gvim.pos(0, 3), to = gvim.pos(0, 3)}, text = "2"}})
T.spy(b:text()) b:undo() T.spy(b:text())`, "1abc2|abc", "abc"},
		{"edit: bad range is a programmer error", "abc", `T.spy((pcall(b.edit, b, {{range = 5, text = "x"}})))`, "false", "abc"},
		{"edit: bad text type is a programmer error", "abc", `T.spy((pcall(b.edit, b, {{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = 5}})))`, "false", "abc"},
		{"edit: an element must be a table", "abc", `T.spy((pcall(b.edit, b, {"x"})))`, "false", "abc"},
		{"edit: bad cursor mode", "abc", `T.spy((pcall(b.edit, b, {}, {cursor = "sideways"})))`, "false", "abc"},
		{"edit: bad option type", "abc", `T.spy((pcall(b.edit, b, {}, {cursor = 5})))`, "false", "abc"},
		{"insert: bad position", "abc", `T.spy((pcall(b.insert, b, 5, "x")))`, "false", "abc"},
		{"insert: text must be a string", "abc", `T.spy((pcall(b.insert, b, gvim.pos(0, 0), {})))`, "false", "abc"},
	})
}

func TestBufEditCursorModes(t *testing.T) {
	rows := []struct {
		name, mode, want string
	}{
		{"map shifts the caret", `cursor = "map"`, "{col=5,line=0}"},
		{"default is map", ``, "{col=5,line=0}"},
		{"keep leaves the caret where it was", `cursor = "keep"`, "{col=3,line=0}"},
		{"end moves the caret after the last edit", `cursor = "end"`, "{col=8,line=0}"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h, _ := bufRun(t, "abcdef", `
b:set_cursor(gvim.pos(0, 3))
b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = "12"}, {range = {from = gvim.pos(0, 5), to = gvim.pos(0, 5)}, text = "Z"}}, {`+r.mode+`})
T.spy(show(b:cursor()))`)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestBufEditErrors(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("ro.txt", "x")
	host.Buffers[0].Info.ReadOnly = true
	host.AddBuffer("large.txt", "y")
	host.Buffers[1].Info.Large = true
	h, _ := bufRun(t, "", `
local ro, large = gvim.buf.get(1), gvim.buf.get(2)
local function code(ok, e) return tostring(ok) .. " " .. tostring(gvim.err_code(e)) end
T.spy(code(ro:insert(gvim.pos(0, 0), "a")))
T.spy(code(ro:set_text("a")))
T.spy(code(ro:set_lines(0, 1, {"a"})))
T.spy(code(ro:append("a")))
T.spy(code(ro:undo()))
T.spy(code(large:replace({from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, "a")))
T.spy(code(large:offset_of(gvim.pos(0, 0))))
T.spy(code(large:find("y")))`, plugintest.Options{Host: host})
	want := []string{
		"nil read_only", "nil read_only", "nil read_only", "nil read_only", "nil read_only",
		"nil too_large", "nil too_large", "nil too_large",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestEditTooManyEdits(t *testing.T) {
	rows := []struct {
		name, body, want string
		edits            int // Host.Edit calls expected
	}{
		{"50001 edits are refused before the host is called", `
local edits = {}
for i = 1, 50001 do edits[i] = {range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = ""} end
local ok, e = b:edit(edits)
T.spy(tostring(ok), e)`, "nil limit: too many edits (max 50000)", 0},
		{"50000 edits are accepted", `
local edits = {}
for i = 1, 50000 do edits[i] = {range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = "x"} end
T.spy(b:edit(edits), #b:text())`, "true 50000", 1},
		{"more than 8 MiB of inserted text is refused", `
local chunk = string.rep("x", 1048576)
local edits = {}
for i = 1, 9 do edits[i] = {range = {from = gvim.pos(0, 0), to = gvim.pos(0, 0)}, text = chunk} end
local ok, e = b:edit(edits)
T.spy(tostring(ok), gvim.err_code(e))`, "nil limit", 0},
		{"single-range calls over 8 MiB are refused", `
local ok, e = b:insert(gvim.pos(0, 0), string.rep("x", 9437184))
T.spy(tostring(ok), gvim.err_code(e))
local ok2, e2 = b:set_text(string.rep("y", 8388609))
T.spy(tostring(ok2), gvim.err_code(e2))
local ok3, e3 = b:set_lines(0, 1, {string.rep("z", 8388609)})
T.spy(tostring(ok3), gvim.err_code(e3))`, "nil limit|nil limit|nil limit", 0},
		{"exactly 8 MiB is accepted", `T.spy(b:insert(gvim.pos(0, 0), string.rep("x", 8388608)))`, "true", 1},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h, host := bufRun(t, "", r.body, plugintest.Options{Timeout: 10 * time.Second})
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
			if n := host.CallCount("Edit"); n != r.edits {
				t.Fatalf("Host.Edit called %d times, want %d", n, r.edits)
			}
		})
	}
}

func TestVersionTokenFormat(t *testing.T) {
	h, host := bufRun(t, "a", `
local v = b:version()
T.spy(v, b:info().version, v == b:version())
b:insert(gvim.pos(0, 0), "x")
T.spy(b:version(), b:version() == v)
for _, bad in ipairs({"", "1", "1.", ".1", "01.2", "1.02", "1.2.3", "-1.0", "a.b", " 1.0", "1.0 ", "1e1.0"}) do
  local ok, e = b:edit({}, {version = bad})
  T.spy(tostring(ok), e)
end
T.spy(b:edit({}, {version = nil}), b:edit({}, {}))
local ok, e = b:edit({}, {version = 5})
T.spy(tostring(ok), e)`)
	want := []string{"1.0 1.0 true", "1.1 false"}
	for range 12 {
		want = append(want, "nil invalid: bad version token")
	}
	want = append(want, "true true", "nil invalid: bad version token")
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
	if host.CallCount("Edit") != 1 {
		t.Fatalf("bad tokens reached the host: %d Edit calls", host.CallCount("Edit"))
	}
}

func TestStaleOnUneditedBuffer(t *testing.T) {
	// The old version=0 hole: a fresh buffer has Buffer.Version() == 0, and a token taken
	// there must still detect a later edit.
	h, _ := bufRun(t, "abc", `
local tok = b:version()
b:insert(gvim.pos(0, 0), "x")
local ok, e = b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "Z"}}, {version = tok})
T.spy(tostring(ok), gvim.err_code(e), b:text())`)
	if got := spyText(h); got != "nil stale xabc" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestUndoRestoresEqualToken(t *testing.T) {
	h, _ := bufRun(t, "abc", `
local v0 = b:version()
b:insert(gvim.pos(0, 0), "x")
local v1 = b:version()
b:undo()
T.spy(b:version() == v0, v1 == v0, b:text())
T.spy(b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "Z"}}, {version = v0}), b:text())
b:undo() b:redo()
T.spy(b:version() ~= v0, b:text())`)
	if got := spyText(h); got != "true false abc|true Zbc|true Zbc" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}

func TestEditExpectStale(t *testing.T) {
	h, host := bufRun(t, "abc", `
local tok = b:version()
T.spy(b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "1"}}, {version = tok}), b:text())
local ok, e = b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "2"}}, {version = tok})
T.spy(tostring(ok), gvim.err_code(e), b:text())
T.spy(b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "3"}}, {version = b:version()}), b:text())
T.spy(b:edit({{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "4"}}), b:text())`)
	if got := spyText(h); got != "true 1bc|nil stale 1bc|true 3bc|true 4bc" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
	// The token reaches the host as the typed Version.
	var exp []pluginapi.EditOptions
	for _, c := range host.Calls() {
		if c.Method == "Edit" {
			exp = append(exp, c.Args[2].(pluginapi.EditOptions))
		}
	}
	if len(exp) != 4 || exp[0].Expect == nil || *exp[0].Expect != (pluginapi.Version{Gen: 1}) || exp[3].Expect != nil {
		t.Fatalf("Edit options = %+v", exp)
	}
}

func TestBufUndoGroup(t *testing.T) {
	runBufRows(t, []bufRow{
		{"one undo step, results returned", "abc", `
local a, c = b:undo_group(function()
  b:insert(gvim.pos(0, 0), "1") b:insert(gvim.pos(0, 4), "2") b:set_text(b:text() .. "3")
  return "r1", 7
end)
T.spy(a, c, b:text()) b:undo() T.spy(b:text())`, "r1 7 1abc23|abc", "abc"},
		{"nested groups are one step", "abc", `
b:undo_group(function()
  b:insert(gvim.pos(0, 0), "1")
  b:undo_group(function() b:insert(gvim.pos(0, 0), "2") end)
  b:insert(gvim.pos(0, 0), "3")
end)
T.spy(b:text()) b:undo() T.spy(b:text())`, "321abc|abc", "abc"},
		{"edits outside the group are separate steps", "abc", `
b:insert(gvim.pos(0, 0), "1")
b:undo_group(function() b:insert(gvim.pos(0, 0), "2") b:insert(gvim.pos(0, 0), "3") end)
b:undo() T.spy(b:text()) b:undo() T.spy(b:text())`, "1abc|abc", "abc"},
		{"an error in the body is re-raised with its object, edits stay grouped", "abc", `
local ok, e = pcall(b.undo_group, b, function() b:insert(gvim.pos(0, 0), "1") error({code = 42}) end)
T.spy(tostring(ok), type(e), e.code, b:text()) b:undo() T.spy(b:text())`, "false table 42 1abc|abc", "abc"},
		{"a string error keeps its text", "abc", `
local ok, e = pcall(b.undo_group, b, function() error("boom", 0) end)
T.spy(tostring(ok), e)`, "false boom", "abc"},
		{"yielding inside the body is an error", "abc", `
local ok, e = pcall(b.undo_group, b, function() gvim.async.suspend(function() end) end)
T.spy(tostring(ok), tostring(e):find("must not yield", 1, true) ~= nil)`, "false true", "abc"},
		{"the body must be a function", "abc", `T.spy((pcall(b.undo_group, b, 5)))`, "false", "abc"},
		{"an empty group returns nothing", "abc", `T.spy(select("#", b:undo_group(function() end)))`, "0", "abc"},
	})
}

func TestBufUndoGroupIsBudgeted(t *testing.T) {
	h, _ := bufRun(t, "abc", `
b:undo_group(function() b:insert(gvim.pos(0, 0), "x") while true do end end)
T.spy("not reached")`, plugintest.Options{Timeout: 40 * time.Millisecond})
	if got := spyText(h); got != "" {
		t.Fatalf("code ran past the deadline: %q", got)
	}
	if !noticeHas(h, "timed out (40 ms)") {
		t.Fatalf("notices = %v log = %v", h.NoticeTexts(), h.LogText(""))
	}
	// the state is usable afterwards
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
}

func TestBufCursors(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "hello\nworld")
	h, _ := bufRun(t, "", `
T.spy(show(b:cursor()), tostring(b:selection()), tostring(b:selected_text()), show(b:cursors()))
T.spy(b:set_cursor(gvim.pos(1, 2)), show(b:cursor()))
T.spy(b:set_cursor(gvim.pos(0, 1), {extend = true}), show(b:selection()), b:selected_text())
T.spy(b:set_cursor(gvim.pos(99, 99)), show(b:cursor()), tostring(b:selection()))
T.spy(b:select{from = gvim.pos(1, 3), to = gvim.pos(0, 2)}, show(b:cursor()), show(b:selection()), b:selected_text())
T.spy(show(b:cursors()))
b:set_cursor(gvim.pos(0, 0), {reveal = false, center = true})
T.spy((pcall(b.set_cursor, b, gvim.pos(0, 0), {center = "yes"})))`, plugintest.Options{Host: host})
	want := []string{
		"{col=0,line=0} nil nil {1={pos={col=0,line=0}}}",
		"true {col=2,line=1}",
		"true {from={col=1,line=0},to={col=2,line=1}} ello\nwo",
		"true {col=5,line=1} nil",
		"true {col=2,line=0} {from={col=2,line=0},to={col=3,line=1}} llo\nwor",
		"{1={anchor={col=3,line=1},pos={col=2,line=0}}}",
		"false",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
	var flags [][3]bool
	for _, c := range host.Calls() {
		if c.Method == "SetCursor" {
			flags = append(flags, [3]bool{c.Args[2].(bool), c.Args[3].(bool), c.Args[4].(bool)})
		}
	}
	wantFlags := [][3]bool{{false, true, false}, {true, true, false}, {false, true, false}, {false, false, true}}
	if !slices.Equal(flags, wantFlags) {
		t.Fatalf("SetCursor(extend, reveal, center) = %v, want %v", flags, wantFlags)
	}
}

func TestBufFindTruncatedFlag(t *testing.T) {
	const text = "foo Foo\nbar foo\nfoobar FOO\nfoo"
	rows := []struct {
		name, body, want string
	}{
		{"max reached sets truncated", `local r, tr = b:find("foo", {max = 2}) T.spy(#r, tr, r[2].from.line, r[2].from.col)`, "2 true 0 4"},
		{"fewer matches than max is not truncated", `local r, tr = b:find("foo", {max = 10}) T.spy(#r, tr)`, "6 false"},
		{"default max is 1000 and no truncation on a small file", `local r, tr = b:find("foo") T.spy(#r, tr)`, "6 false"},
		{"case sensitive", `local r, tr = b:find("foo", {case_sensitive = true}) T.spy(#r, tr)`, "4 false"},
		{"whole word", `local r = b:find("foo", {whole_word = true}) T.spy(#r)`, "5"},
		{"regex", `local r = b:find("fo+bar", {regex = true}) T.spy(#r, r[1].from.line, r[1].from.col, r[1].to.col)`, "1 2 0 6"},
		{"a literal pattern has no metacharacters", `local r = b:find(".") T.spy(#r)`, "0"},
		{"within", `local r = b:find("foo", {within = gvim.range(1, 0, 2, 0)}) T.spy(#r, r[1].from.line)`, "1 1"},
		{"no match is an empty array and false", `local r, tr = b:find("zzz") T.spy(#r, tr)`, "0 false"},
		{"empty pattern", `local r, e = b:find("") T.spy(tostring(r), e)`, "nil invalid: empty pattern"},
		{"bad regex", `local r, e = b:find("(", {regex = true}) T.spy(tostring(r), gvim.err_code(e))`, "nil invalid"},
		{"pattern too long", `local r, e = b:find(string.rep("a", 5000)) T.spy(tostring(r), gvim.err_code(e))`, "nil limit"},
		{"bad option type", `T.spy((pcall(b.find, b, "foo", {max = "many"})))`, "false"},
		{"bad within", `T.spy((pcall(b.find, b, "foo", {within = 5})))`, "false"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h, _ := bufRun(t, text, r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestBufFindQueryReachesHost(t *testing.T) {
	rows := []struct {
		name, body string
		want       pluginapi.SearchQuery
	}{
		{"defaults", `b:find("x")`, pluginapi.SearchQuery{Text: "x", Max: 1000}},
		{"all options", `b:find("x", {regex = true, case_sensitive = true, whole_word = true, max = 7})`,
			pluginapi.SearchQuery{Text: "x", Regex: true, CaseSensitive: true, WholeWord: true, Max: 7}},
		{"hard cap", `b:find("x", {max = 10000000})`, pluginapi.SearchQuery{Text: "x", Max: 100_000}},
		{"non-positive max means default", `b:find("x", {max = 0})`, pluginapi.SearchQuery{Text: "x", Max: 1000}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h, host := bufRun(t, "x", r.body)
			var got []pluginapi.SearchQuery
			for _, c := range host.Calls() {
				if c.Method == "Search" {
					got = append(got, c.Args[1].(pluginapi.SearchQuery))
				}
			}
			if len(got) != 1 || got[0] != r.want {
				t.Fatalf("Search queries = %+v, want %+v (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestFindLinesBudgetNext(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		if i%100 == 7 {
			sb.WriteString("TODO item\n")
		} else {
			sb.WriteString("plain\n")
		}
	}
	text := strings.TrimSuffix(sb.String(), "\n")
	rows := []struct {
		name, body, want string
		chunk            int
	}{
		{"fully scanned: next is nil", `local ls, nxt = b:find_lines("TODO") T.spy(#ls, ls[1], ls[10], tostring(nxt))`, "10 7 907 nil", 0},
		{"time box reached: next is the first line not scanned", `local ls, nxt = b:find_lines("TODO") T.spy(#ls, ls[#ls], tostring(nxt))`, "3 207 300", 300},
		{"from continues", `local ls, nxt = b:find_lines("TODO", {from = 300}) T.spy(#ls, ls[1], tostring(nxt))`, "3 307 600", 300},
		{"to bounds the range", `local ls, nxt = b:find_lines("TODO", {from = 100, to = 300}) T.spy(#ls, ls[1], ls[2], tostring(nxt))`, "2 107 207 nil", 0},
		{"max reached before the end: next follows the last match", `local ls, nxt = b:find_lines("TODO", {max = 2}) T.spy(#ls, ls[2], tostring(nxt))`, "2 107 108", 0},
		{"plain text is not a regex", `local ls = b:find_lines("^TODO") T.spy(#ls)`, "0", 0},
		{"regex anchors", `local ls = b:find_lines("^TODO\\s", {regex = true}) T.spy(#ls)`, "10", 0},
		{"case sensitive", `local a = b:find_lines("todo") local c = b:find_lines("todo", {case_sensitive = true}) T.spy(#a, #c)`, "10 0", 0},
		{"empty range", `local ls, nxt = b:find_lines("TODO", {from = 50, to = 50}) T.spy(#ls, tostring(nxt))`, "0 nil", 0},
		{"range ending before from", `local ls, nxt = b:find_lines("TODO", {from = 50, to = 10}) T.spy(#ls, tostring(nxt))`, "0 nil", 0},
		{"empty pattern", `local ls, e = b:find_lines("") T.spy(tostring(ls), e)`, "nil invalid: empty pattern", 0},
		{"bad option", `T.spy((pcall(b.find_lines, b, "x", {from = "a"})))`, "false", 0},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			host := plugintest.NewFakeHost()
			host.AddBuffer("a.txt", text)
			host.SetFindLinesChunk(r.chunk)
			h, _ := bufRun(t, "", r.body, plugintest.Options{Host: host})
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestFindLinesQueryReachesHost(t *testing.T) {
	const def = 20 * time.Millisecond
	rows := []struct {
		name, body string
		want       pluginapi.FindLinesQuery
	}{
		{"defaults", `b:find_lines("x")`, pluginapi.FindLinesQuery{Pattern: "x", Max: 10_000, Budget: def}},
		{"all options", `b:find_lines("x", {regex = true, case_sensitive = true, from = 3, to = 9, max = 5, budget_ms = 60})`,
			pluginapi.FindLinesQuery{Pattern: "x", Regex: true, CaseSensitive: true, From: 3, To: 9, Max: 5, Budget: 60 * time.Millisecond}},
		{"budget is clamped to 100 ms", `b:find_lines("x", {budget_ms = 100000})`, pluginapi.FindLinesQuery{Pattern: "x", Max: 10_000, Budget: 100 * time.Millisecond}},
		{"non-positive budget is the default", `b:find_lines("x", {budget_ms = 0})`, pluginapi.FindLinesQuery{Pattern: "x", Max: 10_000, Budget: def}},
		{"negative from is 0", `b:find_lines("x", {from = -4})`, pluginapi.FindLinesQuery{Pattern: "x", Max: 10_000, Budget: def}},
		{"max is capped", `b:find_lines("x", {max = 99999999})`, pluginapi.FindLinesQuery{Pattern: "x", Max: 200_000, Budget: def}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h, host := bufRun(t, "x", r.body)
			var got []pluginapi.FindLinesQuery
			for _, c := range host.Calls() {
				if c.Method == "FindLines" {
					got = append(got, c.Args[1].(pluginapi.FindLinesQuery))
				}
			}
			if len(got) != 1 || got[0] != r.want {
				t.Fatalf("FindLines queries = %+v, want %+v (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestFindLinesChunkedLoop(t *testing.T) {
	var sb strings.Builder
	want, last := 0, 0
	for i := 0; i < 5000; i++ {
		if i%97 == 0 {
			sb.WriteString("<<<<<<< ours\n")
			want++
			last = i
		} else {
			sb.WriteString("code\n")
		}
	}
	host := plugintest.NewFakeHost()
	host.AddBuffer("big.txt", strings.TrimSuffix(sb.String(), "\n"))
	host.SetFindLinesChunk(700)
	h, _ := bufRun(t, "", `
local found, calls, from = {}, 0, 0
repeat
  local ls, nxt = b:find_lines("^<<<<<<<", {regex = true, from = from})
  calls = calls + 1
  for _, l in ipairs(ls) do found[#found + 1] = l end
  from = nxt
until not nxt
T.spy(#found, calls, found[1], found[#found])`, plugintest.Options{Host: host})
	if got := spyText(h); got != fmt.Sprintf("%d 8 0 %d", want, last) {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
	if n := host.CallCount("Lines"); n != 0 {
		t.Fatalf("Host.Lines called %d times: the scan must not read lines into Lua", n)
	}
}

func TestBufTabsAndState(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("a.go", "a")
	host.AddBuffer("b.go", "b")
	host.AddBuffer("c.go", "c")
	h, _ := bufRun(t, "", `
local a, bb, c = gvim.buf.get(1), gvim.buf.get(2), gvim.buf.get(3)
local function names() local out = {} for _, x in ipairs(gvim.buf.list()) do out[#out + 1] = x:name() end return table.concat(out, ",") end
T.spy(bb:show(), tostring(gvim.buf.current()))
T.spy(a:move_tab(1), names())
T.spy(a:move_tab(10), names())
T.spy(c:move_tab(-1), names())
T.spy(a:set_language("rust"), a:language())
a:insert(gvim.pos(0, 0), "x")
local ok, e = a:close()
T.spy(tostring(ok), gvim.err_code(e), a:valid())
T.spy(a:close{force = true}, a:valid(), names())
T.spy(bb:close(), names())
T.spy((pcall(bb.close, bb, 5)))
T.spy((pcall(bb.move_tab, bb, "x")))`, plugintest.Options{Host: host})
	want := []string{
		"true Buffer(2)",
		"true b.go,a.go,c.go",
		"true b.go,c.go,a.go",
		"true c.go,b.go,a.go",
		"true rust",
		"nil conflict true",
		"true false c.go,b.go",
		"true c.go",
		"false",
		"false",
	}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy =\n%q\nwant\n%q\nlog %v", got, want, h.LogText(""))
	}
}

func TestBufMethodsUnderPcall(t *testing.T) {
	h, _ := bufRun(t, "a\nb\nc", `
T.spy(pcall(b.line, b, 0))
T.spy(pcall(b.edit, b, {{range = {from = gvim.pos(0, 0), to = gvim.pos(0, 1)}, text = "X"}}))
T.spy(b:line(0))
T.spy(pcall(b.undo_group, b, function() b:insert(gvim.pos(1, 0), "Y") end))
T.spy(b:text())
local ok, e = pcall(b.line, b, "x")
T.spy(tostring(ok), type(e))`)
	want := []string{"true a", "true true", "X", "true", "X\nYb\nc", "false string"}
	if got := spy(h); !slices.Equal(got, want) {
		t.Fatalf("spy = %q want %q log %v", got, want, h.LogText(""))
	}
}

func TestBufLinesLargeFileUnknownCountSmallRange(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.AddBuffer("big.log", "l1\nl2\nl3")
	host.Buffers[0].Info.Large = true
	host.Buffers[0].Info.LineCount = -1
	h, _ := bufRun(t, "", `
local b = gvim.buf.get(1)
local v, e = b:lines()
T.spy(#v, tostring(e), v[3])
local w = b:lines(1, -1)
T.spy(#w, w[1])
T.spy(#b:lines(0, 2))`, plugintest.Options{Host: host})
	if got := spyText(h); got != "3 nil l3|2 l2|2" {
		t.Fatalf("spy = %q log %v", got, h.LogText(""))
	}
}
