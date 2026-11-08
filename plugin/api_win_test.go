package plugin_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// winHost is a fake host with two buffers: a.go with ten lines and b.go with one.
func winHost() *plugintest.FakeHost {
	h := plugintest.NewFakeHost()
	var lines []string
	for i := range 10 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	h.AddBuffer("/w/a.go", strings.Join(lines, "\n"))
	h.AddBuffer("/w/b.go", "only")
	return h
}

// winRun runs body as a command with w = the current window and returns the harness.
func winRun(t *testing.T, host *plugintest.FakeHost, body string) *plugintest.Harness {
	t.Helper()
	return cmd(t, bufShow, "local w = gvim.win.current()\n"+body, plugintest.Options{Host: host})
}

func winIDs(host *plugintest.FakeHost) []pluginapi.WinID {
	var out []pluginapi.WinID
	for _, w := range host.Windows() {
		out = append(out, w.ID)
	}
	return out
}

func TestWinRegistry(t *testing.T) {
	h := winRun(t, winHost(), `
local list = gvim.win.list()
T.spy(tostring(w), w.id, #list, list[1] == w, gvim.win.get(1) == w, tostring(gvim.win.get(99)), tostring(gvim.win.get(-1)))
T.spy(w:valid(), gvim.has("windows"), tostring(w:buffer()), w:buffer() == gvim.buf.current())
local n = w:split{}
T.spy(n.id, n ~= w, gvim.win.current() == w, #gvim.win.list(), gvim.win.list()[2] == n)
`)
	want := "Window(1) 1 1 true true nil nil|true true Buffer(1) true|2 true true 2 true"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant  %q (log %v)", got, want, h.LogText(""))
	}
}

func TestWinDescriptions(t *testing.T) {
	host := winHost()
	w := host.Window(1)
	w.X, w.Y, w.Width, w.Height, w.TopLine, w.LeftCol = 5, 2, 70, 20, 3, 4
	tests := []struct {
		name, body, want string
	}{
		{"size", `T.spy(show(w:size()))`, "{height=20,width=70,x=5,y=2}"},
		{"view counts rows, not lines", `T.spy(show(w:view()))`, "{height=20,left_col=4,top_line=3,width=70}"},
		{"buffer", `T.spy(tostring(w:buffer()))`, "Buffer(1)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := winRun(t, host, tc.body)
			if got := spyText(h); got != tc.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, tc.want, h.LogText(""))
			}
		})
	}
}

func TestWinSplitOptions(t *testing.T) {
	tests := []struct {
		name string
		opts string
		want pluginapi.SplitOptions
	}{
		{"defaults", ``, pluginapi.SplitOptions{Dir: "right"}},
		{"below with a ratio", `{dir="below", ratio=0.25}`, pluginapi.SplitOptions{Dir: "below", Ratio: 0.25}},
		{"before and focus", `{before=true, focus=true}`, pluginapi.SplitOptions{Dir: "right", Before: true, Focus: true}},
		{"a buffer", `{buf=gvim.buf.get(2)}`, pluginapi.SplitOptions{Dir: "right", Buffer: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host := winHost()
			h := winRun(t, host, `T.spy(tostring(w:split(`+tc.opts+`)))`)
			if got := spyText(h); got != "Window(2)" {
				t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
			}
			var got pluginapi.SplitOptions
			for _, c := range host.Calls() {
				if c.Method == "SplitWindow" {
					got = c.Args[1].(pluginapi.SplitOptions)
				}
			}
			if got != tc.want {
				t.Fatalf("host got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWinSplitRefusals(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"unknown dir", `T.spy(w:split{dir="left"})`, `nil invalid: split dir must be "right" or "below", got "left"`},
		{"ratio of one", `T.spy(w:split{ratio=1})`, "nil invalid: split ratio must be between 0 and 1"},
		{"ratio of zero", `T.spy(w:split{ratio=0})`, "nil invalid: split ratio must be between 0 and 1"},
		{"closed buffer", `local b = gvim.buf.get(2) b:close{force = true} T.spy(w:split{buf=b})`, "nil closed: buffer 2 is closed"},
		{"no room", `
local cur, err = w
for _ = 1, 30 do
  local n, e = cur:split{dir="below"}
  if not n then err = e break end
  cur = n
end
T.spy(err)`, "limit: Not enough room to split"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := winRun(t, winHost(), tc.body)
			if got := spyText(h); got != tc.want {
				t.Fatalf("spy = %q, want %q (log %v)", got, tc.want, h.LogText(""))
			}
		})
	}
}

func TestWinSplitRaisesOnWrongArgumentTypes(t *testing.T) {
	tests := []struct{ name, opts, want string }{
		{"buf is a number", `{buf=2}`, "option 'buf' must be a Buffer"},
		{"buf is a window", `{buf=w}`, "option 'buf' must be a Buffer"},
		{"dir is a number", `{dir=3}`, "option 'dir' must be a string"},
		{"ratio is a string", `{ratio="half"}`, "option 'ratio' must be a number"},
		{"focus is a string", `{focus="yes"}`, "option 'focus' must be a boolean"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host := winHost()
			h := winRun(t, host, `local ok, e = pcall(w.split, w, `+tc.opts+`) T.spy(ok, tostring(e):find(`+fmt.Sprintf("%q", tc.want)+`, 1, true) ~= nil)`)
			if got := spyText(h); got != "false true" {
				t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
			}
			if host.CallCount("SplitWindow") != 0 {
				t.Fatal("the host was called")
			}
		})
	}
}

func TestWinMethodsOnAClosedWindow(t *testing.T) {
	h := winRun(t, winHost(), `
local n = w:split{}
T.spy(n:close(), n:valid(), gvim.win.get(n.id) == nil)
local b = gvim.buf.get(1)
for _, f in ipairs{
  function() return n:buffer() end, function() return n:size() end, function() return n:view() end,
  function() return n:visible() end, function() return n:focus() end, function() return n:close() end,
  function() return n:split{} end, function() return n:scroll_to(1) end, function() return n:set_buffer(b) end,
} do T.spy(select(2, f())) end
`)
	want := "true false true" + strings.Repeat("|closed: window 2 is closed", 9)
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant  %q (log %v)", got, want, h.LogText(""))
	}
}

func TestWinCloseRefusesTheLastWindow(t *testing.T) {
	h := winRun(t, winHost(), `T.spy(w:close())`)
	if got := spyText(h); got != "nil conflict: the last window cannot be closed" {
		t.Fatalf("spy = %q", got)
	}
}

func TestWinFocusSetBufferScroll(t *testing.T) {
	host := winHost()
	host.Window(1).Height = 5
	h := winRun(t, host, `
local n = w:split{buf=gvim.buf.get(2)}
T.spy(tostring(gvim.win.current()), tostring(n:buffer()))
T.spy(n:focus(), tostring(gvim.win.current()), tostring(gvim.buf.current()))
T.spy(w:set_buffer(gvim.buf.get(2)), tostring(w:buffer()), tostring(n:buffer()))
T.spy(w:focus(), tostring(gvim.buf.current()))
T.spy(w:set_buffer(gvim.buf.get(1)), tostring(gvim.buf.current()))
T.spy(w:scroll_to(7), w:view().top_line, w:scroll_to(5, {center = true}), w:view().top_line)
T.spy(w:scroll_to(500), w:view().top_line, w:scroll_to(-4), w:view().top_line)
T.spy(gvim.win.equalize())
`)
	want := strings.Join([]string{
		"Window(1) Buffer(2)",
		"true Window(2) Buffer(2)",
		"true Buffer(2) Buffer(2)",
		"true Buffer(2)",
		"true Buffer(1)",
		"true 7 true 3",
		"true 9 true 0",
		"true",
	}, "|")
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant  %q (log %v)", got, want, h.LogText(""))
	}
	if host.Win().Equalized != 1 {
		t.Fatalf("Equalize calls = %d", host.Win().Equalized)
	}
}

func TestWinVisibleRows(t *testing.T) {
	host := winHost()
	host.SetVisible(1, []pluginapi.VisibleRow{
		{Line: 2, ColFrom: 0, ColTo: 8},
		{Line: 2, ColFrom: 8, ColTo: 12, Wrapped: true},
		{Line: 5, ColFrom: 0, ColTo: 0},
	})
	h := winRun(t, host, `for _, r in ipairs(w:visible()) do T.spy(show(r)) end`)
	want := "{col_from=0,col_to=8,line=2,wrapped=false}|{col_from=8,col_to=12,line=2,wrapped=true}|{col_from=0,col_to=0,line=5,wrapped=false}"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant  %q", got, want)
	}
}

func TestWinUnloadClosesTheWindowsThePluginOpened(t *testing.T) {
	host := winHost()
	foreign, err := host.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"}) // not the plugin's
	if err != nil {
		t.Fatal(err)
	}
	h := winRun(t, host, `
local a = gvim.win.get(1):split{dir="below"}
local b = a:split{dir="below"}
T.spy(a.id, b.id, b:close())
`)
	if got := spyText(h); got != "3 4 true" {
		t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
	}
	h.Unload("t")
	got := winIDs(host)
	slices.Sort(got)
	if want := []pluginapi.WinID{1, foreign}; !slices.Equal(got, want) {
		t.Fatalf("windows after unload = %v, want %v: only the plugin's open window goes", got, want)
	}
	if n := host.CallCount("CloseWindow"); n != 2 {
		t.Fatalf("CloseWindow calls = %d, want 2 (the plugin's own close, and the unload of the other)", n)
	}
}

// TestWinHandlePurgeAsksTheHostOnce checks that purging a full handle cache lists the
// windows once, not once for each cached window handle: a plugin that splits and closes
// windows fills the cache with dead handles, and the purge runs on the UI goroutine.
func TestWinHandlePurgeAsksTheHostOnce(t *testing.T) {
	host := winHost()
	const body = `local w = gvim.win.current()
for i = 1, 2000 do local x, err = w:split{dir = "below"}; assert(x, err); x:close() end
T.spy(#gvim.win.list())`
	h := cmd(t, bufShow, body, plugintest.Options{Host: host})
	before := host.Win().ListCalls
	for range 6 { // 12000 window handles in all, past the cap of the cache
		if err := h.Run("t.go", nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := plugin.HandleCacheSize(h.RT, "t"); n >= 10_000 {
		t.Fatalf("cache holds %d handles, the purge never ran", n)
	}
	if calls := host.Win().ListCalls - before; calls > 100 {
		t.Fatalf("Windows() was called %d times; a purge must take one snapshot", calls)
	}
	if got := spyText(h); !strings.HasSuffix(got, "1") {
		t.Fatalf("spy = %q, want the one window left at the end", got)
	}
}
