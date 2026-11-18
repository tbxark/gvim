package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// seamsPlugin exercises the tier-1c-C seams: split labels, window state and the split
// wrappers.
const seamsPlugin = `
local gvim = require 'gvim'
local win2 = nil
gvim.command.register{ name = "setup", run = function()
  local wins = gvim.win.list()
  win2 = wins[2]
end }
gvim.command.register{ name = "labels", run = function()
  gvim.notify("set:" .. tostring(gvim.win.set_split_label(win2, "log")))
  gvim.notify("found:" .. tostring(gvim.win.get_split_by_label("log") ~= nil))
  gvim.notify("clear:" .. tostring(gvim.win.clear_split_label(win2)))
  gvim.notify("gone:" .. tostring(gvim.win.get_split_by_label("log") == nil))
end }
gvim.command.register{ name = "state", run = function()
  local buf = gvim.buf.current()
  gvim.notify("setmode:" .. tostring(gvim.buf.set_window_state(buf, "mode", "diff")))
  gvim.notify("setline:" .. tostring(gvim.buf.set_window_state(buf, "line", 12)))
  gvim.notify("mode:" .. tostring(gvim.buf.get_window_state(buf, "mode")))
  gvim.notify("line:" .. tostring(gvim.buf.get_window_state(buf, "line")))
  gvim.notify("default:" .. tostring(gvim.buf.get_window_state(buf, "missing", "fallback")))
  gvim.notify("clear:" .. tostring(gvim.buf.set_window_state(buf, "mode", nil)))
  gvim.notify("cleared:" .. tostring(gvim.buf.get_window_state(buf, "mode", "none")))
end }
gvim.command.register{ name = "splits", run = function()
  gvim.notify("focus:" .. tostring(gvim.buf.focus_split(win2)))
  gvim.notify("close:" .. tostring(gvim.buf.close_split(win2)))
end }
`

// TestSeamsLuaSurface checks the split label, window state and split wrapper bindings.
func TestSeamsLuaSurface(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"s.lua": seamsPlugin}, plugintest.Options{})
	h.Start()
	h.Host.AddBuffer("/w/a.go", "package a\n")
	if _, err := h.Host.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("s.setup", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("s.labels", nil); err != nil {
		t.Fatal(err)
	}
	notices := strings.Join(h.NoticeTexts(), "|")
	for _, want := range []string{"set:true", "found:true", "clear:true", "gone:true"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("labels notices = %q, missing %q", notices, want)
		}
	}
	if got := h.Host.SplitLabel(2); got != "" {
		t.Fatalf("label after clear = %q", got)
	}

	if err := h.Run("s.state", nil); err != nil {
		t.Fatal(err)
	}
	notices = strings.Join(h.NoticeTexts(), "|")
	for _, want := range []string{"setmode:true", "setline:true", "mode:diff", "line:12", "default:fallback", "cleared:none"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("state notices = %q, missing %q", notices, want)
		}
	}
	if _, ok := h.Host.WindowStateValue("s", 1, "mode"); ok {
		t.Fatal("the cleared key is still set")
	}

	if err := h.Run("s.splits", nil); err != nil {
		t.Fatal(err)
	}
	notices = strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(notices, "focus:true") || !strings.Contains(notices, "close:true") {
		t.Fatalf("split wrapper notices = %q", notices)
	}
}

// TestOpenFileInSplitResult pins the result shape the docs describe: the Buffer, as
// gvim.buf.open returns it (not a leading true).
func TestOpenFileInSplitResult(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.SetFile("/w/x.go", "x")
	h := cmd(t, "", `
local b, err = gvim.buf.open_file_in_split("/w/x.go", {direction = "below"})
T.spy(tostring(b), tostring(err))`, plugintest.Options{Host: host})
	settleBuffers(h, host)
	got := spy(h)
	if len(got) != 1 || got[0] != "Buffer(1) nil" {
		t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
	}
}

func TestOpenFileInSplitDirectionsAndValidation(t *testing.T) {
	cases := []struct {
		name       string
		call       string
		wantAction string
		wantErr    bool
	}{
		{"below maps to split_horizontal", `gvim.buf.open_file_in_split("/w/x.go", {direction = "below"})`, "split_horizontal", false},
		{"right maps to split_vertical", `gvim.buf.open_file_in_split("/w/x.go", {direction = "right"})`, "split_vertical", false},
		{"invalid direction rejected pre-split", `gvim.buf.open_file_in_split("/w/x.go", {direction = "diagonal"})`, "", true},
		{"empty path rejected pre-split", `gvim.buf.open_file_in_split("", {direction = "right"})`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := plugintest.NewFakeHost()
			host.SetFile("/w/x.go", "x")
			h := cmd(t, "", `local ok, b, err = pcall(function() return `+tc.call+` end) T.spy(tostring(ok), tostring(b ~= nil), tostring(err))`, plugintest.Options{Host: host})
			settleBuffers(h, host)
			var splitActions []string
			for _, c := range host.Cmd().Calls {
				if strings.HasPrefix(c.Action, "split_") {
					splitActions = append(splitActions, c.Action)
				}
			}
			got := spy(h)
			if !tc.wantErr {
				if len(splitActions) != 1 || splitActions[0] != tc.wantAction {
					t.Fatalf("split actions = %v, want [%q] (spy=%v log=%v)", splitActions, tc.wantAction, got, h.LogText(""))
				}
				if len(got) != 1 || got[0] != "true true nil" {
					t.Fatalf("spy = %q, want true true nil", got)
				}
				return
			}
			if len(splitActions) != 0 {
				t.Fatalf("split called on invalid input: actions=%v", splitActions)
			}
			if len(got) != 1 || (!strings.HasPrefix(got[0], "false ") && !strings.HasPrefix(got[0], "true false ")) {
				t.Fatalf("spy = %q, want validation error", got)
			}
		})
	}
}
