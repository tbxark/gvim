package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// ticks is how many times the loop of a command ran; the command writes it with T.spy.
func ticks(h *plugintest.Harness) int {
	n := 0
	for _, l := range plugin.Spy(h.RT) {
		if l == "tick" {
			n++
		}
	}
	return n
}

// A plugin that loops on sleep(0) wakes up a thousand times a second. It is slowed down, told
// once, and given strikes if it goes on: three suspend it.
func TestHeatSleepZeroLoopIsThrottledNotStruck(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("spin", function()
  while true do gvim.timer.sleep(0) T.spy("tick") end
end)`)
	if err := h.Run("t.spin", nil); err != nil {
		t.Fatal(err)
	}
	for range 2500 {
		h.Advance(time.Millisecond)
	}
	before := ticks(h)
	if before < 500 {
		t.Fatalf("the loop ran only %d times in the first 2.5 s: the test clock is off", before)
	}
	// the throttle is in force by now: a second of the loop gives a few dozen turns at most
	for range 1000 {
		h.Advance(time.Millisecond)
	}
	if got := ticks(h) - before; got > 40 {
		t.Errorf("the loop still ran %d times in a second after it was found hot", got)
	}
	logged := strings.Join(h.LogText("t"), "\n")
	if n := strings.Count(logged, "timer pace: "); n != 1 {
		t.Errorf("log says the throttle %d times, want once:\n%s", n, logged)
	}
	notices := strings.Join(h.NoticeTexts(), "\n")
	if !strings.Contains(notices, "re-arm faster") {
		t.Errorf("no notice about the throttle: %q", notices)
	}
	// still looping, but idle between its turns: throttled, not struck (see timerpace_test.go)
	for range 1000 {
		h.Advance(50 * time.Millisecond)
	}
	if got := status(h, "t"); got != "loaded" {
		t.Fatalf("status = %q after a minute of the loop, want loaded; log:\n%s", got, strings.Join(h.LogText("t"), "\n"))
	}
}

// A timer that runs a lot of Lua each time (12 ms of every 10 ms wake-up in the manual clock) is found by the time it uses.
func TestHeatBusyTimerIsThrottled(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("burn", function()
  gvim.timer.every(10, function()
    local t = os.clock()
    while os.clock() - t < 0.012 do end
    T.spy("tick")
  end)
end)`)
	if err := h.Run("t.burn", nil); err != nil {
		t.Fatal(err)
	}
	for range 250 {
		h.Advance(10 * time.Millisecond)
	}
	if !strings.Contains(strings.Join(h.LogText("t"), "\n"), "timer pace: ") {
		t.Fatalf("a timer that burns 12 ms every 10 ms was not found; log: %v", h.LogText("t"))
	}
}

// A plugin whose timers are ordinary is left alone.
func TestOrdinaryTimersAreNotThrottled(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("tick", function()
  gvim.timer.every(10, function() T.spy("tick") end)
end)`)
	if err := h.Run("t.tick", nil); err != nil {
		t.Fatal(err)
	}
	for range 500 {
		h.Advance(10 * time.Millisecond)
	}
	if got := ticks(h); got < 450 {
		t.Errorf("an ordinary 10 ms timer ran %d times in 5 s, want about 500", got)
	}
	if strings.Contains(strings.Join(h.LogText("t"), "\n"), "too often") {
		t.Errorf("an ordinary timer was throttled: %v", h.LogText("t"))
	}
}

// A burst that ends is forgiven: the throttle lifts after a few calm seconds.
func TestThrottleLiftsWhenTheBurstEnds(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local n = 0
T.command("burst", function()
  for _ = 1, 1100 do gvim.timer.sleep(0) end
  while true do gvim.timer.sleep(500) T.spy("tick") end
end)`)
	if err := h.Run("t.burst", nil); err != nil {
		t.Fatal(err)
	}
	for range 1100 {
		h.Advance(time.Millisecond)
	}
	for range 240 {
		h.Advance(50 * time.Millisecond)
	}
	if got := status(h, "t"); got == "suspended" {
		t.Fatalf("a burst that ended got the plugin suspended; log %v", h.LogText("t"))
	}
	before := ticks(h)
	for range 10 {
		h.Advance(500 * time.Millisecond)
	}
	if got := ticks(h) - before; got != 10 {
		t.Errorf("after the burst the 500 ms loop ran %d times in 5 s, want 10", got)
	}
}
