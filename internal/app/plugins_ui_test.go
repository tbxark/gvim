package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
)

// uiPlugin exercises the prompts, pickers and popups of the plugin API through the real
// runtime, editor and ui.
const uiPlugin = `
local gvim = require "gvim"

local function put(text) gvim.buf.current():insert({ line = 0, col = 0 }, text) end

gvim.command.register{ name = "ask", title = "Ask Name", run = function()
  local name = gvim.ui.input{ prompt = "Name:", validate = function(s) if s == "no" then return "refused" end end }
  if name == nil then gvim.notify("cancelled input") return end
  put("[" .. name .. "] ")
end }

gvim.command.register{ name = "yesno", title = "Ask Yes No", run = function()
  gvim.notify("answer " .. tostring(gvim.ui.confirm("Proceed?")))
end }

gvim.command.register{ name = "pick", title = "Pick Fruit", run = function()
  local item, i = gvim.ui.select({ "apple", "banana", "cherry" }, { title = "Fruit" })
  if item == nil then gvim.notify("no fruit") return end
  put(item .. "#" .. i .. " ")
end }

gvim.command.register{ name = "live", title = "Pick Live", run = function()
  local p
  p = gvim.ui.picker{
    title = "Live", filter = "none",
    on_query = function(q, pk) pk:set_items({ "result for " .. q }) end,
    on_select = function(item) put(item .. " ") end,
    on_close = function() gvim.notify("live closed") end,
  }
  p:open()
end }

gvim.command.register{ name = "info", title = "Show Info", run = function()
  gvim.ui.popup{ title = "Info", lines = { "first line", "second line" }, on_close = function() gvim.notify("popup closed") end }
end }

gvim.command.register{ name = "menu", title = "Show Menu", run = function()
  gvim.ui.popup{ title = "Menu", items = { "one", "two", "three" }, on_select = function(it, i) put(it .. i .. " ") end }
end }
`

func newUISession(t *testing.T) *pluginSession {
	t.Helper()
	isolateConfig(t)
	writeUserFile(t, "plugins/uitest.lua", uiPlugin)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("uitest", "loaded")
	return s
}

func TestPluginInputPrompt(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Ask Name")
	s.until("the prompt", func() bool { return s.screenHas("Name:") })
	s.typeText("no")
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the validator's reason", func() bool { return s.screenHas("refused") })
	// The rejected text stays; an edit lifts the rejection and Enter submits.
	s.press(tea.KeyPressMsg{Code: tea.KeyBackspace})
	s.typeText("b")
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the inserted answer", func() bool { return s.text() == "[nb] world\n" })
	if s.screenHas("Name:") {
		t.Fatal("the prompt is still open")
	}
}

func TestPluginInputEscape(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Ask Name")
	s.until("the prompt", func() bool { return s.screenHas("Name:") })
	s.typeText("ab")
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.until("the cancelled notice", func() bool { return strings.Contains(s.statusRow(), "cancelled input") })
	if got := s.text(); got != "world\n" {
		t.Fatalf("text = %q, an Esc must change nothing", got)
	}
}

func TestPluginConfirm(t *testing.T) {
	for _, tt := range []struct{ name, key, want string }{
		{"yes", "y", "answer true"},
		{"no", "n", "answer false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newUISession(t)
			s.runCommand("Ask Yes No")
			s.until("the question", func() bool { return s.screenHas("Proceed?") })
			s.typeText(tt.key)
			s.until("the answer", func() bool { return strings.Contains(s.statusRow(), tt.want) })
		})
	}
}

func TestPluginSelectRanksAndInserts(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Pick Fruit")
	s.until("the picker", func() bool { return s.screenHas("Fruit") && s.screenHas("banana") })
	s.typeText("ban")
	// The ranking runs in a command; wait for the rows to follow.
	s.until("the ranked rows", func() bool { return s.screenHas("1/3") || !s.screenHas("cherry") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the chosen item with its 1-based index", func() bool { return s.text() == "banana#2 world\n" })
}

func TestPluginSelectEscape(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Pick Fruit")
	s.until("the picker", func() bool { return s.screenHas("apple") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.until("the notice", func() bool { return strings.Contains(s.statusRow(), "no fruit") })
}

func TestPluginLivePicker(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Pick Live")
	s.until("the first on_query", func() bool { return s.screenHas("result for") })
	s.typeText("xy")
	s.until("the items for the typed query (debounced)", func() bool { return s.screenHas("result for xy") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the selected item", func() bool { return strings.HasPrefix(s.text(), "result for xy ") })
}

func TestPluginLivePickerEscapeRunsOnClose(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Pick Live")
	s.until("the picker", func() bool { return s.screenHas("result for") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.until("the on_close notice", func() bool { return strings.Contains(s.statusRow(), "live closed") })
}

func TestPluginPopups(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Show Info")
	s.until("the popup", func() bool { return s.screenHas("second line") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.until("the on_close notice", func() bool { return strings.Contains(s.statusRow(), "popup closed") })

	s.runCommand("Show Menu")
	s.until("the menu", func() bool { return s.screenHas("three") })
	s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the chosen row", func() bool { return s.text() == "two2 world\n" })
}

// TestPluginOverlayClosesWhenItsPluginUnloads is the wait-ownership check against the real
// runtime: a prompt that a plugin has open when it is unloaded goes away with it, and the
// keys that follow reach the editor again.
func TestPluginOverlayClosesWhenItsPluginUnloads(t *testing.T) {
	s := newUISession(t)
	s.runCommand("Ask Name")
	s.until("the prompt", func() bool { return s.screenHas("Name:") })
	// Disabling unloads the plugin at once, on the update loop (this goroutine).
	if _, notices := s.plug.Control(pluginapi.Control{Op: "disable", Name: "uitest"}); len(notices) == 0 {
		t.Fatal("disable gave no notice")
	}
	s.until("the prompt to be gone", func() bool { return !s.screenHas("Name:") })
	s.typeText("z")
	s.settle(20 * time.Millisecond)
	if got := s.text(); got != "zworld\n" {
		t.Fatalf("text = %q, the key after the unload should be typed into the buffer", got)
	}
}

// TestPluginPickerUpdatedRightAfterOpen checks S13-1 through the real runtime: p:open()
// followed by set_items and set_title in the same slice must reach the screen.
func TestPluginPickerUpdatedRightAfterOpen(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/pk.lua", `
local gvim = require "gvim"
gvim.command.register{ name = "pk", title = "Pk", run = function()
  local p = gvim.ui.picker{ title = "First", items = { "a" } }
  p:open()
  local ok1, e1 = p:set_items({ "xray", "yankee" })
  local ok2, e2 = p:set_title("Second")
  if not (ok1 and ok2) then gvim.notify("update failed " .. tostring(e1) .. tostring(e2)) end
end }
`)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("pk", "loaded")
	s.runCommand("Pk")
	s.until("the updated picker", func() bool { return s.screenHas("Second") && s.screenHas("yankee") })
	if s.screenHas("First") || s.screenHas("update failed") {
		t.Fatal("the picker shows the stale spec or the update failed")
	}
}
