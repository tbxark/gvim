package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/pluginapi"
)

func TestGvimRoot(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"constants", `T.spy(gvim.api_version, gvim.version, gvim.platform)`, "2 test test"},
		{"plugin table", `T.spy(gvim.plugin.name, gvim.plugin.dir == "", gvim.plugin.source, gvim.plugin.version == "")`, "t true user true"},
		{"has unknown feature", `T.spy(gvim.has("nope"), gvim.has("also_nope"))`, "false false"},
		{"has a provided feature", `T.provides("decor") T.spy(gvim.has("decor"), gvim.has("nope"))`, "true false"},
		{"err_code", `T.spy(gvim.err_code("read_only: buffer"), tostring(gvim.err_code("whatever: x")), tostring(gvim.err_code("no code")), tostring(gvim.err_code(nil)), tostring(gvim.err_code(7)))`, "read_only nil nil nil nil"},
		{"err_code of every code", `
local n = 0
for _, c in ipairs({"read_only","closed","too_large","invalid","not_found","busy","timeout","cancelled","io","exec","unsupported","conflict","limit","stale"}) do
  if gvim.err_code(c .. ": msg") == c then n = n + 1 end
end
T.spy(n)`, "14"},
		{"pos", `local p = gvim.pos(3, 4) T.spy(p.line, p.col, gvim.pos(5).col)`, "3 4 0"},
		{"range", `local r = gvim.range(1, 2, 3, 4) T.spy(r.from.line, r.from.col, r.to.line, r.to.col)`, "1 2 3 4"},
		{"pos_cmp", `T.spy(gvim.pos_cmp(gvim.pos(1, 5), gvim.pos(2, 0)), gvim.pos_cmp(gvim.pos(2, 1), gvim.pos(2, 0)), gvim.pos_cmp({line = 1, col = 1}, {line = 1, col = 1}))`, "-1 1 0"},
		{"range_eq", `T.spy(gvim.range_eq(gvim.range(1, 2, 3, 4), gvim.range(1, 2, 3, 4)), gvim.range_eq(gvim.range(1, 2, 3, 4), gvim.range(1, 2, 3, 5)))`, "true false"},
		{"pos_cmp rejects non-positions", `T.spy((pcall(gvim.pos_cmp, 1, 2)))`, "false"},
		{"notify returns true", `T.spy(gvim.notify("x"), gvim.status("y"))`, "true true"},
		{"notify rejects a bad level", `T.spy((pcall(gvim.notify, "x", "loud")))`, "false"},
		{"notify needs a string", `T.spy((pcall(gvim.notify)))`, "false"},
		{"on_unload needs a function", `T.spy((pcall(gvim.plugin.on_unload, 1)))`, "false"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestNotifyAndLog(t *testing.T) {
	h := cmd(t, "", `
gvim.notify("info line")
gvim.notify("two\nlines\r\nhere", "info")
gvim.notify("watch out", "warn")
gvim.notify("broken", "error")
gvim.status("status line")
gvim.notify(string.rep("é", 400))
gvim.log.debug("debug %d", 1)
gvim.log.info("info %s %s", "a", "b")
gvim.log.info("plain % no args")
gvim.log.info(42, nil, true)
gvim.log.warn("warn %s", "w")
gvim.log.error("bad format %d", "not a number")
print("via print", 7)`)
	var got []string
	for _, n := range h.Notices() {
		if n.Plugin != "t" {
			t.Fatalf("notice from %q", n.Plugin)
		}
		got = append(got, string(n.Level)+":"+n.Msg)
	}
	long := got[5]
	got = got[:5]
	want := []string{"info:info line", "info:two lines here", "warn:watch out", "error:broken", "info:status line"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notices = %q", got)
	}
	if !strings.HasPrefix(long, "info:é") || len(long) > 512+len("info:") || strings.HasSuffix(long[:len(long)-3], "\xc3") {
		t.Fatalf("long notice not clipped on a rune boundary: %d bytes", len(long))
	}
	log := strings.Join(h.LogText("t"), "\n")
	for _, want := range []string{
		"debug: debug 1", "info: info a b", "info: plain % no args", "info: 42 nil true",
		"warn: warn w", "info: via print\t7",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if !strings.Contains(log, "error: bad format") {
		t.Errorf("a bad format must still log something:\n%s", log)
	}
	// warn and error also reach the user's warnings log through Notify; debug and info do not.
	var warns []string
	for _, n := range h.Notices() {
		if n.Level != pluginapi.LevelInfo {
			warns = append(warns, n.Msg)
		}
	}
	if len(warns) < 4 {
		t.Fatalf("warn/error notices = %v", warns)
	}
}

func TestLogIsABoundedRing(t *testing.T) {
	h := cmd(t, "", `for i = 1, 2500 do gvim.log.info("line %d", i) end`)
	log := h.Log()
	if len(log) != 2000 {
		t.Fatalf("ring holds %d entries, want 2000", len(log))
	}
	if !strings.HasSuffix(log[len(log)-1].Msg, "line 2500") || !strings.HasSuffix(log[0].Msg, "line 501") {
		t.Fatalf("ring order: first %q last %q", log[0].Msg, log[len(log)-1].Msg)
	}
	h2 := cmd(t, "", `gvim.log.info(string.rep("x", 10000))`)
	if n := len(h2.Log()[len(h2.Log())-1].Msg); n > 4096 {
		t.Fatalf("log message of %d bytes", n)
	}
}

func TestNoticeDedupe(t *testing.T) {
	h := start(t, `T.command("go", function() error("same failure") end)`)
	for i := 0; i < 4; i++ {
		_ = h.Run("t.go", nil)
	}
	count := 0
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, "same failure") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the same error produced %d notices within 10 s", count)
	}
	h.Advance(11 * time.Second)
	_ = h.Run("t.go", nil)
	count = 0
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, "same failure") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("after 10 s the error is announced again: %d notices", count)
	}
}

func TestCommandErrorNotice(t *testing.T) {
	h := start(t, `T.command("go", function() local x = nil; return x.field end)`)
	_ = h.Run("t.go", nil)
	// the position goes last, where a cut status row loses the least
	if !noticeHas(h, "t: attempt to index") || !noticeHas(h, "(t/init.lua:1)") {
		t.Fatalf("notices = %v", h.NoticeTexts())
	}
	if n := len(h.NoticeTexts()); n != 1 {
		t.Fatalf("notices = %v", h.NoticeTexts())
	}
	if !logHas(h, "t", "stack traceback") && !logHas(h, "t", "in main") && !logHas(h, "t", "t/init.lua") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	// an ordinary Lua error is not a strike
	if n := plugin.Strikes(h.RT, "t"); n != 0 {
		t.Fatalf("ordinary errors must not count as strikes: %d", n)
	}
}
