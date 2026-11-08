package plugin_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

// run moves the manual clock forward by d in steps of one millisecond, the finest a timer
// can ask for, so that a loop on sleep(0) gets one turn per step as it does on a real clock.
func run(h *plugintest.Harness, d time.Duration) {
	for range d / time.Millisecond {
		h.Advance(time.Millisecond)
	}
}

// A plugin that loops on gvim.timer.sleep(0) never trips the time budget of a slice, but it
// burns a core. The pace budget throttles it and says so once. The throttle floor leaves it
// idle between its turns, so once throttled it is judged by its CPU share, which is small: it is
// not struck (a busy loop that keeps the core busy through the floor is, see TestBusyLoopIsStruck).
func TestSleepZeroLoopIsThrottledNotStruck(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local n = 0
T.command("spin", function() while true do n = n + 1 gvim.timer.sleep(0) end end)
T.command("count", function() T.spy("n=" .. n) end)`)
	if err := h.Run("t.spin", nil); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		before := len(spy(h))
		if err := h.Run("t.count", nil); err != nil {
			t.Fatal(err)
		}
		got := spy(h)[before:]
		if len(got) != 1 {
			t.Fatalf("count spy = %v", got)
		}
		n, err := strconv.Atoi(strings.TrimPrefix(got[0], "n="))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	// the first windows run free: nothing is known yet
	run(h, 1500*time.Millisecond)
	if noticeHas(h, "re-arm faster") {
		t.Fatalf("throttled before two hot windows: %v", h.NoticeTexts())
	}
	run(h, time.Second)
	if !noticeHas(h, "re-arm faster") {
		t.Fatalf("no notice after two hot seconds: notices %v log %v", h.NoticeTexts(), h.LogText("t"))
	}
	if !logHas(h, "t", "timer pace") {
		t.Fatalf("no log line: %v", h.LogText("t"))
	}
	// throttled: about ten turns a second
	at := count()
	run(h, time.Second)
	if got := count() - at; got > 15 {
		t.Fatalf("%d turns in the throttled second, want about 10", got)
	}
	// the warning is given once
	warns := 0
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, "re-arm faster") {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("%d warnings, want 1", warns)
	}
	// still looping, but idle between its turns: throttled, not struck
	for range 60 {
		run(h, time.Second)
	}
	if got := status(h, "t"); got != "loaded" {
		t.Fatalf("status %q after a minute of looping, want loaded (a throttled loop uses no core), log %v", got, h.LogText("t"))
	}
	at = count()
	run(h, time.Second)
	if got := count() - at; got > 15 {
		t.Fatalf("%d turns in a throttled second after a minute, want about 10", got)
	}
}

// A task that does 2 ms of work and then sleep(0), again and again, is a cooperative job, not a
// runaway: for 20 s of virtual time it is throttled at most, and it is never suspended.
func TestCooperativeJobIsNotSuspended(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("work", function()
  while true do
    local t = os.clock()
    while os.clock() - t < 0.002 do end
    gvim.timer.sleep(0)
  end
end)`)
	if err := h.Run("t.work", nil); err != nil {
		t.Fatal(err)
	}
	run(h, 20*time.Second)
	if got := status(h, "t"); got != "loaded" {
		t.Fatalf("the cooperative job was %q after 20 s, log %v", got, h.LogText("t"))
	}
}

// A repeating timer that keeps its callback busy is throttled the same way.
func TestBusyTimerIsThrottled(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("go", function()
  gvim.timer.every(50, function()
    local stop = os.clock() + 0.03 -- os.clock is the real clock: busy for 30 ms
    while os.clock() < stop do end
    T.spy("tick")
  end)
end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		run(h, 100*time.Millisecond)
		if noticeHas(h, "re-arm faster") {
			return
		}
	}
	t.Fatalf("a busy timer was not throttled: notices %v log %v", h.NoticeTexts(), h.LogText("t"))
}

// A plugin that waits for real intervals is left alone.
func TestSlowTimersAreNotThrottled(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("go", function() gvim.timer.every(10, function() T.spy("tick") end) end)`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		run(h, 100*time.Millisecond)
	}
	if noticeHas(h, "re-arm faster") || status(h, "t") != "loaded" {
		t.Fatalf("a 10 ms timer was throttled: %v %q", h.NoticeTexts(), status(h, "t"))
	}
}

// A timer whose callback keeps the core busy through the throttle is struck: the floor leaves
// it 60 ms of work in every 240 ms, a quarter of a core, which is still hot.
func TestBusyLoopIsStruck(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("burn", function()
  gvim.timer.every(10, function()
    local t = os.clock()
    while os.clock() - t < 0.06 do end
  end)
end)`)
	if err := h.Run("t.burn", nil); err != nil {
		t.Fatal(err)
	}
	run(h, 60*time.Second)
	if got := status(h, "t"); got != "suspended" {
		t.Fatalf("status %q after a minute of a busy timer, want suspended; log %v", got, h.LogText("t"))
	}
}
