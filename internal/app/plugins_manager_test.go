package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	mgrHello  = "local gvim = require \"gvim\"\ngvim.command.register{ name = \"hi\", title = \"Hello One\", run = function() gvim.notify(\"one\") end }\n"
	mgrHello2 = "local gvim = require \"gvim\"\ngvim.command.register{ name = \"hi\", title = \"Hello Two\", run = function() gvim.notify(\"two\") end }\n"
	mgrBroken = "error(\"kaboom\")\n"
)

// newManagerSession starts a session whose user folder holds a working plugin, a broken
// one, a plugin the configuration switches off, and a JavaScript file of the old host.
func newManagerSession(t *testing.T) *pluginSession {
	t.Helper()
	isolateConfig(t)
	writeUserFile(t, "plugins/hello.lua", mgrHello)
	writeUserFile(t, "plugins/broken.lua", mgrBroken)
	writeUserFile(t, "plugins/locked_one.lua", mgrHello)
	writeUserFile(t, "plugins/old.js", "gvim.registerCommand('x', 'X', function() {})\n")
	writeUserFile(t, "config.json", `{"plugins": {"locked_one": {"enabled": false}}}`)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("hello", "loaded")
	s.waitStatus("broken", "failed")
	return s
}

// openManager runs Plugin: Manager and waits for the rows. The list shows only the first
// rows of a long list (the bundled plugins come first), so the wait is for a bundled row.
func (s *pluginSession) openManager() {
	s.t.Helper()
	s.runCommand("Plugin: Manager")
	s.until("the manager", func() bool { return s.screenHas("Plugins") && s.screenHas("git_blame") })
}

// TestPluginManagerShowsEveryKindOfRow checks the real runtime's rows as the manager draws
// them: a loaded plugin with its counts, a failed one with its error, a plugin the
// configuration locks off, and the ignored JavaScript file as a legacy row.
func TestPluginManagerShowsEveryKindOfRow(t *testing.T) {
	s := newManagerSession(t)
	s.openManager()
	// The list scrolls: collect what is painted while the selection walks down every row.
	var seen []string
	for range len(s.plug.Plugins()) + 1 {
		seen = append(seen, s.rows()...)
		s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	rows := strings.Join(seen, "\n")
	for _, want := range []string{
		"legacy", "JavaScript plugins are not supported",
		"locked: set in config", "kaboom", "(1 cmds, ",
		"goto_with_selection", "bundled",
	} {
		if !strings.Contains(rows, want) {
			t.Errorf("the manager does not show %q:\n%s", want, rows)
		}
	}
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.screenHas("Plugins") {
		t.Fatal("Esc left the manager open")
	}
}

// choosePluginAction opens the manager, filters to name, opens the actions of its row and
// chooses the action whose label is label.
func (s *pluginSession) choosePluginAction(name, label string, downs int) {
	s.t.Helper()
	s.choosePluginActionAfter(name, label, downs, "git_blame")
}

// choosePluginActionAfter is choosePluginAction that waits for the filter to have removed
// the row showing gone.
func (s *pluginSession) choosePluginActionAfter(name, label string, downs int, gone string) {
	s.t.Helper()
	s.openManager()
	s.typeText(name)
	// The fuzzy filter keeps rows that merely contain the letters; the row showing gone has
	// none of them, so it disappears once the filter has been applied.
	s.until("the filtered row", func() bool { return s.screenHas(name) && !s.screenHas(gone) })
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the actions of "+name, func() bool { return s.screenHas(label) })
	for range downs {
		s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.settle(20 * time.Millisecond)
}

// TestPluginManagerDisableAndEnable toggles a plugin through the manager and checks the
// runtime follows and the choice is persisted for the next start.
func TestPluginManagerDisableAndEnable(t *testing.T) {
	s := newManagerSession(t)
	s.choosePluginAction("hello", "Disable", 1) // Reload, Disable
	s.waitStatus("hello", "disabled")
	state := filepath.Join(pluginConfigDir(), "plugin-state.json")
	s.until("plugin-state.json", func() bool {
		data, err := os.ReadFile(state)
		return err == nil && strings.Contains(string(data), "hello")
	})

	s.choosePluginAction("hello", "Enable", 0)
	s.waitStatus("hello", "loaded")
}

// TestPluginManagerCannotToggleAConfigLockedPlugin checks that the runtime's refusal is
// shown.
func TestPluginManagerCannotToggleAConfigLockedPlugin(t *testing.T) {
	s := newManagerSession(t)
	s.choosePluginAction("locked_one", "Enable", 0)
	// The message is long, so it can wrap onto the notice band above the status row.
	s.until("the refusal", func() bool { return strings.Contains(s.noticeText(), "plugin 'locked_one' is set in config") })
	if got := s.status("locked_one"); got != "disabled" {
		t.Fatalf("status = %q, want it unchanged", got)
	}
}

// TestPluginReloadPicksUpTheNewSource edits a plugin on disk and reloads it through
// Plugin: Reload....
func TestPluginReloadPicksUpTheNewSource(t *testing.T) {
	s := newManagerSession(t)
	writeUserFile(t, "plugins/hello.lua", mgrHello2)
	s.runCommand("Plugin: Reload...")
	s.until("the reload picker", func() bool { return s.screenHas("Reload plugin") })
	s.typeText("hello")
	s.until("the filtered row", func() bool { return !s.screenHas("broken") && s.screenHas("hello") })
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the reloaded command", func() bool {
		s.runCommand("Hello Two")
		return strings.Contains(s.statusRow(), "two")
	})
}

// TestPluginReloadAll reloads every plugin from disk.
func TestPluginReloadAll(t *testing.T) {
	s := newManagerSession(t)
	writeUserFile(t, "plugins/hello.lua", mgrHello2)
	s.runCommand("Plugin: Reload All")
	s.until("the new command", func() bool {
		s.runCommand("Hello Two")
		return strings.Contains(s.statusRow(), "two")
	})
}

// TestPluginShowLogOpensTheLog checks that the log of the real runtime, with the error of
// the broken plugin, opens in a new buffer.
func TestPluginShowLogOpensTheLog(t *testing.T) {
	s := newManagerSession(t)
	s.runCommand("Plugin: Show Log")
	s.until("the log buffer", func() bool { return strings.Contains(s.text(), "kaboom") })
	if got := s.text(); !strings.Contains(got, "broken: ") {
		t.Fatalf("log =\n%s\nwant a line for the broken plugin", got)
	}

	// From the manager the log is limited to the plugin.
	s.choosePluginAction("broken", "Show Log", 2) // Reload, Disable, Show Log
	s.until("the plugin's log", func() bool {
		return strings.Contains(s.text(), "kaboom") && !strings.Contains(s.text(), "gvim: ")
	})
}

// TestInitLuaCommands creates init.lua from the template, checks it, adds a command to it
// and reloads it, all through the palette.
func TestInitLuaCommands(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	initPath := filepath.Join(pluginConfigDir(), "init.lua")

	s.runCommand("init: Edit init.lua")
	s.until("init.lua", func() bool { return strings.HasPrefix(s.text(), "-- init.lua: your own Lua script") })
	if _, err := os.Stat(initPath); err != nil {
		t.Fatalf("init.lua was not created: %v", err)
	}

	s.runCommand("init: Check init.lua")
	s.until("the check", func() bool { return strings.Contains(s.noticeText(), "syntax OK") })

	must(t, os.WriteFile(initPath, []byte(mgrHello), 0o644))
	s.runCommand("init: Reload init.lua")
	s.waitStatus("init", "loaded")
	s.runCommand("Hello One")
	s.until("the command of init.lua", func() bool { return strings.Contains(s.statusRow(), "one") })

	must(t, os.WriteFile(initPath, []byte("this is not lua"), 0o644))
	s.runCommand("init: Check init.lua")
	s.until("the syntax error", func() bool { return strings.Contains(s.noticeText(), "init.lua:") })
}

// TestLoadPluginFromBuffer loads the active buffer as a plugin.
func TestLoadPluginFromBuffer(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.openFile("bufplug.lua", mgrHello2)
	s.runCommand("Load Plugin from Buffer")
	s.waitStatus("bufplug", "loaded")
	s.runCommand("Hello Two")
	s.until("the command of the buffer", func() bool { return strings.Contains(s.statusRow(), "two") })
}

// TestPluginManagerOpenSource opens the source of the plugins as the real runtime reports
// them: a single file, a directory with init.lua next to a helper of the same name, a
// directory with a manifest main, and a single file that fails to load.
func TestPluginManagerOpenSource(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"single file", map[string]string{"plugins/bar.lua": "-- BAR\n"}, "-- BAR"},
		{"failed single file", map[string]string{"plugins/bar.lua": "error('x') -- BAR\n"}, "-- BAR"},
		{"directory entry beats a helper of the same name", map[string]string{
			"plugins/bar/init.lua": "-- ENTRY\n", "plugins/bar/bar.lua": "-- HELPER\n"}, "-- ENTRY"},
		{"directory with a manifest main", map[string]string{
			"plugins/bar/plugin.json":   `{"main": "src/start.lua"}`,
			"plugins/bar/src/start.lua": "-- START\n", "plugins/bar/init.lua": "-- INIT\n", "plugins/bar/bar.lua": "-- HELPER\n"}, "-- START"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			for rel, text := range tt.files {
				writeUserFile(t, rel, text)
			}
			s := newSession(t, Options{})
			s.openFile("a.txt", "world\n")
			s.until("the plugin", func() bool { return s.status("bar") != "" })
			s.choosePluginActionAfter("bar", "Open Source", 3, "goto_with_selection") // Reload, Disable, Show Log, Open Source
			s.until("the source", func() bool { return strings.Contains(s.text(), tt.want) })
			if strings.Contains(s.text(), "HELPER") && !strings.Contains(tt.want, "HELPER") {
				t.Fatalf("opened the helper:\n%s", s.text())
			}
		})
	}
}
