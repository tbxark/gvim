package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

const keysPlugin = `
local gvim = require "gvim"

gvim.keymap.set("ctrl+alt+j", function() gvim.notify("bound key ran") end, { desc = "Bound" })

gvim.mode.define{
  name = "nav",
  text = "swallow",
  status = "-- NAV --",
  bindings = { j = function() gvim.notify("j in nav") end },
}
gvim.command.register{ name = "enter_nav", title = "Enter Nav", run = function() gvim.mode.enter("nav") end }
gvim.command.register{ name = "ask", title = "Ask Key", run = function()
  local k = gvim.input.next_key{ timeout_ms = 5000 }
  gvim.notify("key " .. tostring(k and k.key))
end }
gvim.ui.statusbar.item{ id = "clock", text = "12:00", align = "right" }
`

// TestPluginKeysEndToEnd drives the key APIs through the real runtime, editor, keymap and
// ui: a binding reaches the keymap, a mode takes keys and shows its label, a key wait
// consumes a key, a status item is on the row, and the panic chord ends the mode.
func TestPluginKeysEndToEnd(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/keys.lua", keysPlugin)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("keys", "loaded")

	s.until("the status item", func() bool { return strings.Contains(s.statusRow(), "12:00") })

	s.press(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl | tea.ModAlt})
	s.until("the bound key's notice", func() bool { return strings.Contains(s.statusRow(), "bound key ran") })

	s.runCommand("Enter Nav")
	s.until("the mode label", func() bool { return strings.Contains(s.statusRow(), "-- NAV --") })
	s.press(tea.KeyPressMsg{Code: 'x', Text: "x"})
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "world\n" {
		t.Fatalf("text = %q: the swallowing mode let a letter through", got)
	}
	s.press(tea.KeyPressMsg{Code: 'j', Text: "j"})
	s.until("the mode binding's notice", func() bool { return strings.Contains(s.statusRow(), "j in nav") })

	// The panic chord ends the mode, whatever the plugin does.
	s.press(tea.KeyPressMsg{Code: '^', Mod: tea.ModCtrl})
	s.until("the mode to end", func() bool { return !strings.Contains(s.statusRow(), "-- NAV --") })
	s.press(tea.KeyPressMsg{Code: 'x', Text: "x"})
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "xworld\n" {
		t.Fatalf("text = %q: typing did not come back after the panic chord", got)
	}

	// A key wait takes the next key and the plugin sees it.
	s.runCommand("Ask Key")
	s.settle(20 * time.Millisecond)
	s.press(tea.KeyPressMsg{Code: 'q', Text: "q"})
	s.until("the key the plugin saw", func() bool { return strings.Contains(s.statusRow(), "key q") })
	if got := s.text(); got != "xworld\n" {
		t.Fatalf("text = %q: the captured key reached the editor", got)
	}
}

// TestCtrlShift6IsNotAPanicChordWithoutPluginInput: the ctrl+shift+6 spelling of the panic
// chord only counts while a plugin holds the keyboard. With no plugin input, and with
// --no-plugins, the key goes on to the keymap and shows no notice (S14-1).
func TestCtrlShift6IsNotAPanicChordWithoutPluginInput(t *testing.T) {
	for _, opts := range []Options{{NoPlugins: true}, {}} {
		isolateConfig(t)
		s := newSession(t, opts)
		s.openFile("a.txt", "world\n")
		s.press(tea.KeyPressMsg{Code: '6', Mod: tea.ModCtrl | tea.ModShift})
		s.settle(20 * time.Millisecond)
		if row := s.statusRow(); strings.Contains(row, "plugin input cleared") {
			t.Fatalf("NoPlugins=%v: status row %q", opts.NoPlugins, row)
		}
	}
}

const idlePluginSrc = `
local gvim = require "gvim"
local n = 0
local item = gvim.ui.statusbar.item{ id = "idle", text = "idle0", align = "right" }
gvim.on("idle", function(e) n = n + 1; item:set("idle" .. n) end, { after_ms = 100 })
`

// TestIdleFiresOncePerShortPause: a handler with a small after_ms runs once for every pause
// that reaches it, however the timers jitter (S14-3). A key and a pause of 150 ms, eight
// times, is eight runs.
func TestIdleFiresOncePerShortPause(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/idle.lua", idlePluginSrc)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("idle", "loaded")
	const pauses = 8
	for i := 1; i <= pauses; i++ {
		s.press(tea.KeyPressMsg{Code: tea.KeyRight})
		want := "idle" + strconv.Itoa(i)
		s.until(want, func() bool { return strings.Contains(s.statusRow(), want) })
	}
	s.pump(150 * time.Millisecond)
	if row := s.statusRow(); !strings.Contains(row, "idle"+strconv.Itoa(pauses)) || strings.Contains(row, "idle"+strconv.Itoa(pauses+1)) {
		t.Fatalf("status row %q, want exactly %d idle runs", row, pauses)
	}
}
