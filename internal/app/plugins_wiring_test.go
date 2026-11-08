package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

const helloPlugin = `
local gvim = require "gvim"

gvim.command.register{
  name = "hello",
  title = "Hello Plugin",
  run = function()
    local b = gvim.buf.current()
    -- Two edits: they must be two calls and still undo as one step each, so the command
    -- groups them.
    b:undo_group(function()
      b:insert({ line = 0, col = 0 }, "hello ")
      b:insert({ line = 0, col = 6 }, "plugin ")
    end)
    gvim.notify("hello ran")
  end,
}

gvim.on("before_file_save", function(ev)
  gvim.notify("saving " .. ev.buf:name())
end)
`

// TestUserPluginWorksEndToEnd is the M1 check: a plugin file in the user's plugin folder
// registers a palette command that edits the buffer and notifies, and handles an event,
// all through the real editor, runtime and ui.
func TestUserPluginWorksEndToEnd(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/hello.lua", helloPlugin)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("hello", "loaded")

	s.runCommand("Hello Plugin")
	if got := s.text(); got != "hello plugin world\n" {
		t.Fatalf("text after the command = %q", got)
	}
	if got := s.statusRow(); !strings.Contains(got, "hello ran") {
		t.Fatalf("status row = %q, want the notice of the command", got)
	}

	// The two inserts are one undo step.
	s.press(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := s.text(); got != "world\n" {
		t.Fatalf("text after one undo = %q, want the text before the command", got)
	}

	// A save raises before_file_save, which the plugin handles.
	s.press(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	s.until("the save handler's notice", func() bool { return s.screenHas("saving a.txt") })
}

// TestBundledPluginsAreInThePalette checks that the plugins embedded in the binary load
// without any user file, and that their commands run.
func TestBundledPluginsAreInThePalette(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.waitStatus("tab_actions", "loaded")
	s.waitStatus("goto_with_selection", "loaded")
	for _, a := range []string{"a.txt", "b.txt", "c.txt"} {
		s.openFile(a, "text of "+a+"\n")
	}
	tabs := func() []string {
		var names []string
		for _, tb := range s.rt.ed.Tabs() {
			names = append(names, tb.Name)
		}
		return names
	}
	s.rt.ed.ActivateTab(1)
	s.runCommand("Close Other Tabs")
	if got := tabs(); !slices.Equal(got, []string{"b.txt"}) {
		t.Fatalf("tabs after Close Other Tabs = %v", got)
	}
	if got := s.statusRow(); !strings.Contains(got, "Closed") {
		t.Fatalf("status row = %q, want the count of closed tabs", got)
	}

	// Go to Line with Selection sets the mark and opens the Go to Line prompt, so the
	// line typed there ends a selection that starts at the caret.
	s.openFile("d.txt", "l1\nl2\nl3\nl4\n")
	s.runCommand("Go to Line with Selection")
	s.until("the Go to Line prompt", func() bool { return s.screenHas("Go to line") || s.screenHas("Go to Line") })
	s.typeText("3")
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.settle(20 * time.Millisecond)
	d := s.rt.ed.Active()
	if sel := d.Sel; sel == nil || sel.Anchor.Line != 0 || sel.Head.Line != 2 {
		t.Fatalf("selection after the goto = %+v (cursor %v), want line 0 to line 2", sel, d.Cursor)
	}
}

// paletteHas opens the palette on query and reports whether a row shows it; it closes the
// palette again.
func (s *pluginSession) paletteHas(query string) bool {
	s.t.Helper()
	s.press(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	s.typeText(query)
	s.settle(60 * time.Millisecond)
	// The query row is the last one; a row that shows the query elsewhere is a command.
	rows := s.rows()
	found := false
	for _, r := range rows[:len(rows)-1] {
		if strings.Contains(r, query) {
			found = true
		}
	}
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.settle(10 * time.Millisecond)
	return found
}

func TestPluginSourcesAndFlags(t *testing.T) {
	user := func(t *testing.T) {
		writeUserFile(t, "plugins/hello.lua", helloPlugin)
	}
	extra := func(t *testing.T) string {
		return writeUserFile(t, "extra/other.lua", `
local gvim = require "gvim"
gvim.command.register{ name = "other", title = "Other Plugin", run = function() end }
`)
	}
	tests := []struct {
		name  string
		setup func(t *testing.T) Options
		// loaded and absent are plugin names; commands maps a palette title to whether it is listed.
		loaded, absent []string
		commands       map[string]bool
		// owns maps an action id to whether the runtime runs it.
		owns map[string]bool
	}{
		{
			name:     "default: bundled and user plugins",
			setup:    func(t *testing.T) Options { user(t); return Options{} },
			loaded:   []string{"hello", "tab_actions", "goto_with_selection"},
			commands: map[string]bool{"Hello Plugin": true, "Close All Tabs": true},
			owns:     map[string]bool{"hello.hello": true, "tab_actions.close_all": true},
		},
		{
			name:     "--no-plugins disables everything",
			setup:    func(t *testing.T) Options { user(t); return Options{NoPlugins: true} },
			commands: map[string]bool{"Hello Plugin": false, "Close All Tabs": false},
		},
		{
			name:     "--plugin loads one extra file",
			setup:    func(t *testing.T) Options { user(t); return Options{PluginPaths: []string{extra(t)}} },
			loaded:   []string{"hello", "tab_actions", "other"},
			commands: map[string]bool{"Other Plugin": true, "Hello Plugin": true},
		},
		{
			name:     "--no-plugins keeps --plugin",
			setup:    func(t *testing.T) Options { user(t); return Options{NoPlugins: true, PluginPaths: []string{extra(t)}} },
			loaded:   []string{"other"},
			absent:   []string{"hello", "tab_actions", "goto_with_selection"},
			commands: map[string]bool{"Other Plugin": true, "Hello Plugin": false, "Close All Tabs": false},
			owns:     map[string]bool{"other.other": true, "hello.hello": false, "tab_actions.close_all": false},
		},
		{
			name: "config enabled=false for a user and a bundled plugin",
			setup: func(t *testing.T) Options {
				user(t)
				writeUserFile(t, "config.json", `{"plugins": {"hello": {"enabled": false}, "tab_actions": {"enabled": false}}}`)
				return Options{}
			},
			loaded:   []string{"goto_with_selection"},
			absent:   []string{"hello", "tab_actions"},
			commands: map[string]bool{"Hello Plugin": false, "Close All Tabs": false, "Go to Line with Selection": true},
			owns:     map[string]bool{"hello.hello": false, "tab_actions.close_all": false, "goto_with_selection.goto_line_with_selection": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			s := newSession(t, tt.setup(t))
			if s.plug == nil {
				t.Fatalf("the model holds no runtime")
			}
			if tt.loaded == nil {
				s.settle(30 * time.Millisecond)
				if ps := s.plug.Plugins(); len(ps) != 0 {
					t.Fatalf("plugins are off, but %d are listed", len(ps))
				}
			}
			for _, n := range tt.loaded {
				s.waitStatus(n, "loaded")
			}
			if len(tt.loaded) > 0 {
				s.settle(30 * time.Millisecond)
			}
			for _, n := range tt.absent {
				if st := s.status(n); st == "loaded" {
					t.Errorf("plugin %s is loaded, want it absent", n)
				}
			}
			for action, want := range tt.owns {
				if got := s.plug.Owns(action); got != want {
					t.Errorf("runtime owns %q = %v, want %v", action, got, want)
				}
			}
			for title, want := range tt.commands {
				if got := s.paletteHas(title); got != want {
					t.Errorf("palette lists %q = %v, want %v", title, got, want)
				}
			}
		})
	}
}

// TestBrokenPluginDoesNotBreakTheOthers: a syntax error and a runtime error in two plugins
// each show a notice (and stay in the warnings log), and the plugins next to them load.
func TestBrokenPluginDoesNotBreakTheOthers(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/hello.lua", helloPlugin)
	writeUserFile(t, "plugins/broken_syntax.lua", "local x = = 1\n")
	writeUserFile(t, "plugins/broken_run.lua", `error("boom at load")`)
	writeUserFile(t, "plugins/old.js", "// a JavaScript plugin")
	s := newSession(t, Options{})
	s.waitStatus("hello", "loaded")
	s.waitStatus("broken_syntax", "failed")
	s.waitStatus("broken_run", "failed")
	s.waitStatus("tab_actions", "loaded")
	s.settle(30 * time.Millisecond)

	var broken, run, old bool
	for _, e := range s.plug.Log() {
		switch {
		case strings.Contains(e.Msg, "broken_syntax"):
			broken = true
		case strings.Contains(e.Msg, "boom at load"):
			run = true
		case strings.Contains(e.Msg, "JavaScript plugin"):
			old = true
		}
	}
	if !broken || !run || !old {
		t.Errorf("log lacks the problems (syntax %v, runtime %v, legacy js %v): %v", broken, run, old, s.plug.Log())
	}
	// Every problem reached the warnings log of the ui, not only the last one.
	s.runCommand("Show Warnings")
	for _, want := range []string{"broken_syntax", "boom at load", "JavaScript plugin"} {
		if !s.screenHas(want) {
			t.Errorf("Show Warnings does not list %q", want)
		}
	}
	// The healthy plugin still works.
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.openFile("a.txt", "world\n")
	s.runCommand("Hello Plugin")
	if got := s.text(); got != "hello plugin world\n" {
		t.Fatalf("text = %q after the command of a healthy plugin", got)
	}
}

// TestLoopingPluginLeavesUpdateInsideItsBudget: a command that never returns is cut off at
// plugin_timeout_ms, the editor goes on, and the plugin is not lost.
func TestLoopingPluginLeavesUpdateInsideItsBudget(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugin_timeout_ms": 60}`)
	writeUserFile(t, "plugins/spin.lua", `
local gvim = require "gvim"
gvim.command.register{ name = "spin", title = "Spin Forever", run = function() while true do end end }
gvim.command.register{ name = "ok", title = "Still Works", run = function() gvim.notify("still works") end }
`)
	s := newSession(t, Options{})
	s.waitStatus("spin", "loaded")
	s.openFile("a.txt", "x\n")

	s.press(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	s.typeText("Spin Forever")
	s.until("the row", func() bool { return s.screenHas("Spin Forever") })
	start := time.Now()
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	took := time.Since(start)
	if took > 2*time.Second {
		t.Fatalf("Update with a looping plugin took %v, want about the 60 ms budget", took)
	}
	s.settle(30 * time.Millisecond)
	if got := s.statusRow(); !strings.Contains(got, "timed out") {
		t.Errorf("status row = %q, want the notice that the command timed out", got)
	}
	var logged bool
	for _, e := range s.plug.Log() {
		if e.Plugin == "spin" && strings.Contains(e.Msg, "timed out (60 ms)") {
			logged = true
		}
	}
	if !logged {
		t.Errorf("the plugin log does not record the timeout: %v", s.plug.Log())
	}
	s.runCommand("Still Works")
	if got := s.statusRow(); !strings.Contains(got, "still works") {
		t.Fatalf("status row = %q, want the plugin to go on working", got)
	}
}

// TestPluginUsesTheEditorAndWorkspaceThroughTheRealHost runs one command that reads the
// workspace and the settings, and opens, edits, saves and reverts a file, awaiting each
// step. It checks the values the real ui resolves the async calls with.
func TestPluginUsesTheEditorAndWorkspaceThroughTheRealHost(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "needle.txt"), []byte("xx needle\nsecond\n"), 0o644))
	result := filepath.Join(t.TempDir(), "result.txt")
	writeUserFile(t, "config.json", `{"tab_size": 3}`)
	writeUserFile(t, "plugins/probe.lua", `
local gvim = require "gvim"
gvim.command.register{ name = "probe", title = "Probe Host", run = function()
  local out = {}
  local function add(s) out[#out + 1] = s end
  add("root=" .. gvim.workspace.root())
  add("tab_size=" .. tostring(gvim.config.get("tab_size")))
  add("theme=" .. gvim.theme.name())
  local hits = gvim.workspace.grep("needle")
  add("grep=" .. #hits .. ":" .. hits[1].path .. ":" .. hits[1].line .. ":" .. hits[1].col)
  local b, err = gvim.buf.open("`+filepath.Join(root, "needle.txt")+`")
  if not b then add("open failed: " .. tostring(err)) else
    add("opened=" .. b:name() .. " lines=" .. b:line_count())
    b:insert({ line = 0, col = 0 }, "X")
    add("modified=" .. tostring(b:is_modified()))
    local ok, serr = b:save()
    add("saved=" .. tostring(ok) .. " modified=" .. tostring(b:is_modified()))
    b:insert({ line = 0, col = 0 }, "Y")
    local rok, rerr = b:revert()
    add("reverted=" .. tostring(rok) .. " text=" .. b:line(0) .. " modified=" .. tostring(b:is_modified()))
    local missing, merr = gvim.buf.open("`+filepath.Join(root, "absent", "none.txt")+`")
    add("missing=" .. tostring(missing ~= nil))
  end
  gvim.fs.write("`+result+`", table.concat(out, "\n"))
end }
`)
	s := newSession(t, Options{Root: root})
	s.waitStatus("probe", "loaded")
	s.runCommand("Probe Host")
	s.until("the result file", func() bool { _, err := os.Stat(result); return err == nil })
	s.pump(30 * time.Millisecond)
	data, err := os.ReadFile(result)
	must(t, err)
	wantRoot, _ := filepath.EvalSymlinks(root)
	got := strings.Split(string(data), "\n")
	want := []string{
		"root=" + wantRoot,
		"tab_size=3",
		"theme=dark",
		"grep=1:needle.txt:0:3",
		"opened=needle.txt lines=3",
		"modified=true",
		"saved=true modified=false",
		"reverted=true text=Xxx needle modified=false",
		"missing=true", // a path that does not exist opens as a new, empty buffer
	}
	if !slices.Equal(got, want) {
		t.Fatalf("result:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if disk, _ := os.ReadFile(filepath.Join(root, "needle.txt")); string(disk) != "Xxx needle\nsecond\n" {
		t.Fatalf("file on disk = %q, want the saved text without the reverted edit", disk)
	}
}

// TestPluginStateIsSeededBeforeTheMainChunk: the state a plugin wrote in one run is there
// when its main chunk starts in the next run, read through the real OS.
func TestPluginStateIsSeededBeforeTheMainChunk(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/counter.lua", `
local gvim = require "gvim"
gvim.state.set("runs", gvim.state.get("runs", 0) + 1)
`)
	for run := 1; run <= 3; run++ {
		s := newSession(t, Options{NoPlugins: false})
		s.waitStatus("counter", "loaded")
		s.rt.exit()
		data, err := os.ReadFile(filepath.Join(pluginConfigDir(), "plugin-data", "counter", "state.json"))
		must(t, err)
		var state map[string]any
		must(t, json.Unmarshal(data, &state))
		if state["runs"] != float64(run) {
			t.Fatalf("state after run %d = %s, want runs = %d", run, data, run)
		}
	}
}

// TestPluginsLoadedThenReady: a plugin sees plugins_loaded, then ready, although the first
// window size arrives while the plugins are still loading.
func TestPluginsLoadedThenReady(t *testing.T) {
	isolateConfig(t)
	result := filepath.Join(t.TempDir(), "order.txt")
	writeUserFile(t, "plugins/order.lua", `
local gvim = require "gvim"
local seen = {}
gvim.on("plugins_loaded", function() seen[#seen + 1] = "plugins_loaded" end)
gvim.on("ready", function()
  seen[#seen + 1] = "ready"
  gvim.fs.write("`+result+`", table.concat(seen, ","))
end)
`)
	s := newSession(t, Options{})
	s.until("the order file", func() bool { _, err := os.Stat(result); return err == nil })
	s.pump(30 * time.Millisecond)
	data, err := os.ReadFile(result)
	must(t, err)
	if got, want := string(data), "plugins_loaded,ready"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

// TestStartupProblemsAreNotHiddenByEachOther: the first load error is on the status row
// (the status row is cut, so it is the one that matters), and every problem, the note
// about ignored JavaScript files too, is in the warnings log.
func TestStartupProblemsAreNotHiddenByEachOther(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/old.js", "// a JavaScript plugin")
	writeUserFile(t, "plugins/p1.lua", `error("e1")`)
	writeUserFile(t, "plugins/p2.lua", `error("e2")`)
	s := newSession(t, Options{})
	s.waitStatus("p1", "failed")
	s.waitStatus("p2", "failed")
	s.settle(30 * time.Millisecond)
	if row := s.noticeText(); !strings.Contains(row, "plugin 'p1'") {
		t.Errorf("status row and notice band %q lack the first error", row)
	}
	s.runCommand("Show Warnings")
	for _, want := range []string{"plugin 'p1'", "plugin 'p2'", "JavaScript plugin(s) ignored"} {
		if !s.screenHas(want) {
			t.Errorf("Show Warnings does not list %q", want)
		}
	}
}
