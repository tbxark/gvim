package plugin_test

import (
	"bytes"
	"context"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func TestDiffLines(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"hunks", `local h = gvim.diff.lines({"a", "b", "c"}, {"a", "x", "c", "d"})
			T.spy(#h, h[1].kind, h[1].old_start, h[1].old_count, h[1].new_start, h[1].new_count, h[2].kind, h[2].new_start, h[2].new_count)`,
			"2 change 1 1 1 1 insert 3 1"},
		{"equal", `T.spy(#gvim.diff.lines({"a"}, {"a"}))`, "0"},
		{"delete", `local h = gvim.diff.lines({"a", "b"}, {"a"}) T.spy(h[1].kind, h[1].old_start, h[1].old_count, h[1].new_count)`, "delete 1 1 0"},
		{"numbers are lines", `T.spy(#gvim.diff.lines({1, 2}, {1, 3}))`, "1"},
		{"not a line", `T.spy(gvim.diff.lines({"a", {}}, {}))`, "nil invalid: diff.lines: line 2 is not a string"},
		{"not tables", `T.spy(pcall(gvim.diff.lines, "a", {}))`, "~two arrays of lines expected"},
		{"too many lines", `local big = {} for i = 1, 100001 do big[i] = "x" end T.spy(gvim.diff.lines(big, {}))`, "nil too_large: diff.lines: more than 100000 lines per side"},
	}), plugintest.Options{Timeout: 5 * time.Second})
}

func TestDiffText(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"normalised by default", `local h = gvim.diff.text("a\nb\n", "a\r\nc\n") T.spy(#h, h[1].old_start, h[1].old_count)`, "1 1 1"},
		{"eol kept on request", `local h = gvim.diff.text("a\nb\n", "a\r\nc\n", {ignore_eol = false}) T.spy(#h, h[1].old_start, h[1].old_count)`, "1 0 2"},
		{"identical", `T.spy(#gvim.diff.text("x", "x"))`, "0"},
		{"bad argument", `T.spy(pcall(gvim.diff.text, "x", 5))`, "~two strings expected"},
		{"too many lines", `T.spy(gvim.diff.text(string.rep("\n", 1000000), "x"))`, "nil too_large: diff.text: more than 1000000 lines per side"},
	}), plugintest.Options{Timeout: 5 * time.Second})
}

func TestDiffHunkNavigation(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"next", `local h = {{new_start = 2}, {new_start = 9}, {new_start = 5}}
			T.spy(gvim.diff.next_hunk(h, 2).new_start, gvim.diff.next_hunk(h, -1).new_start, gvim.diff.next_hunk(h, 9))`, "5 2 nil"},
		{"prev", `local h = {{new_start = 2}, {new_start = 9}, {new_start = 5}}
			T.spy(gvim.diff.prev_hunk(h, 5).new_start, gvim.diff.prev_hunk(h, 100).new_start, gvim.diff.prev_hunk(h, 2))`, "2 9 nil"},
		{"returns the element", `local x = {new_start = 4} T.spy(gvim.diff.next_hunk({x}, 0) == x)`, "true"},
		{"empty", `T.spy(gvim.diff.next_hunk({}, 0), gvim.diff.prev_hunk({}, 0))`, "nil nil"},
	}))
}

// bufHost is a host with the two buffer reads diff.buffer and diff.saved use (the FakeHost
// has the real ones): FullText and SavedLines.
type bufHost struct {
	*plugintest.FakeHost
	mu       sync.Mutex
	text     string
	ver      pluginapi.Version
	saved    []string
	savedErr error
	events   []string
	uiGID    uint64
}

func (b *bufHost) log(s string) {
	b.mu.Lock()
	b.events = append(b.events, s)
	b.mu.Unlock()
}

func (b *bufHost) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	b.log("FullText on ui=" + strconv.FormatBool(testGoid() == b.uiGID))
	return b.text, b.ver, nil
}

func (b *bufHost) SavedLines(id pluginapi.BufferID) ([]string, error) {
	return b.saved, b.savedErr
}

func testGoid() uint64 {
	var buf [64]byte
	s := bytes.TrimPrefix(buf[:runtime.Stack(buf[:], false)], []byte("goroutine "))
	n, _ := strconv.ParseUint(string(s[:bytes.IndexByte(s, ' ')]), 10, 64)
	return n
}

// recordingServices wraps FakeServices to see where DiffText runs and to simulate an edit
// that happens while the diff is computed.
type recordingServices struct {
	*plugintest.FakeServices
	host    *bufHost
	editing func()
}

func (s recordingServices) DiffText(a, b string, o pluginapi.DiffOptions) []pluginapi.Hunk {
	s.host.log("DiffText on ui=" + strconv.FormatBool(testGoid() == s.host.uiGID))
	if s.editing != nil {
		s.editing()
	}
	return s.FakeServices.DiffText(a, b, o)
}

func (s recordingServices) Grep(ctx context.Context, r pluginapi.GrepRequest) ([]pluginapi.GrepHit, error) {
	return s.FakeServices.Grep(ctx, r)
}

// startWith starts the harness with a host that is not the FakeHost.
func startWith(t *testing.T, files plugintest.Files, host pluginapi.Host, o plugintest.Options) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, files, o)
	h.RunWork(h.RT.Start(host))
	for _, s := range h.Sources {
		if err := h.RT.LoadPlugin(s); err != nil {
			t.Fatalf("LoadPlugin: %v", err)
		}
		h.Settle()
	}
	return h
}

const diffBufferSrc = `
local gvim = require "gvim"
T.command("go", function()
  local h, err = gvim.diff.buffer(T.buf(1), "a\nx\nc")
  if not h then T.spy("err", err) return end
  T.spy(#h, h[1].kind, h[1].old_start, h[1].new_start, h.version)
end)`

func TestDiffBufferNormalisesCRLF(t *testing.T) {
	// The buffer side has CRLF endings, the other side LF; only line 1 differs.
	host := &bufHost{FakeHost: plugintest.NewFakeHost(), text: "a\r\nb\r\nc", ver: pluginapi.Version{Gen: 2, Ver: 7}}
	host.AddBuffer("a.txt", "")
	h := startWith(t, plugintest.Files{"t.lua": diffBufferSrc}, host, plugintest.Options{})
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "1 change 1 1 2.7" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestDiffBufferIsAsync(t *testing.T) {
	// The buffer is snapshotted synchronously (on the ui goroutine), the diff runs in a Work
	// (another goroutine), and an edit made meanwhile does not change the result: it carries
	// the token of the snapshot, so the caller can compare.
	host := &bufHost{FakeHost: plugintest.NewFakeHost(), text: "a\nb\nc", ver: pluginapi.Version{Gen: 1, Ver: 3}}
	host.AddBuffer("a.txt", "")
	host.uiGID = testGoid()
	svc := recordingServices{FakeServices: &plugintest.FakeServices{}, host: host}
	svc.editing = func() {
		host.mu.Lock()
		host.text, host.ver = "changed", pluginapi.Version{Gen: 1, Ver: 4}
		host.mu.Unlock()
	}
	h := startWith(t, plugintest.Files{"t.lua": diffBufferSrc}, host, plugintest.Options{Services: svc})
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	events := append([]string(nil), host.events...)
	host.mu.Unlock()
	if len(events) != 2 || events[0] != "FullText on ui=true" || events[1] != "DiffText on ui=false" {
		t.Fatalf("events = %v", events)
	}
	if got := spyText(h); got != "1 change 1 1 1.3" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestDiffBufferErrors(t *testing.T) {
	host := &bufHost{FakeHost: plugintest.NewFakeHost()}
	host.AddBuffer("a.txt", "")
	host.text = ""
	big := make([]byte, 0, 17<<20)
	for len(big) < 17<<20 {
		big = append(big, "xxxxxxxxxxxxxxxx"...)
	}
	host.text = string(big)
	h := startWith(t, plugintest.Files{"t.lua": diffBufferSrc}, host, plugintest.Options{})
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "err too_large: diff.buffer: buffer larger than 16777216 bytes" {
		t.Fatalf("spy = %q", got)
	}
}

func TestDiffSaved(t *testing.T) {
	src := `
local gvim = require "gvim"
T.command("go", function()
  local h, err = gvim.diff.saved(T.buf(1))
  if not h then T.spy("err", err) return end
  T.spy(#h, h[1] and h[1].kind, h[1] and h[1].new_start)
end)`
	host := &bufHost{FakeHost: plugintest.NewFakeHost(), saved: []string{"l1", "x", "l3"}}
	host.AddBuffer("a.txt", "l1\nl2\nl3")
	h := startWith(t, plugintest.Files{"t.lua": src}, host, plugintest.Options{})
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "1 change 1" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}

	host.savedErr = pluginapi.Errf(pluginapi.CodeNotFound, "buffer has no saved text")
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spy(h)[1]; got != "err not_found: buffer has no saved text" {
		t.Fatalf("spy = %v", spy(h))
	}
}
