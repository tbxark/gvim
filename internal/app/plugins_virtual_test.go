package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/session"
)

// virtualPlugin makes an outline in a read-only virtual buffer with a buffer-local mode:
// Enter reports the line the entry under the cursor stands for, q closes the buffer.
const virtualPlugin = `
local gvim = require "gvim"

gvim.mode.define{ name = "outline", text = "swallow", bindings = {
  enter = function()
    local p = gvim.buf.current():props_at()
    gvim.notify("jump to " .. tostring(p and p.line))
  end,
  q = function() gvim.buf.current():close() end,
}}

gvim.command.register{ name = "outline", title = "Show Outline", run = function()
  local v = gvim.buf.create{ name = "Outline", read_only = true, focus = true, mode = "outline" }
  v:set_content({
    { text = "Heading A\n", style = { bold = true }, props = { line = 3 } },
    { text = "Heading B\n", props = { line = 9 } },
  })
  v:set_cursor(gvim.pos(1, 0))
end }
`

func newVirtualSession(t *testing.T) *pluginSession {
	t.Helper()
	isolateConfig(t)
	writeUserFile(t, "plugins/outline.lua", virtualPlugin)
	s := newSession(t, Options{})
	s.openFile("a.txt", "world\n")
	s.waitStatus("outline", "loaded")
	return s
}

// The plugin makes a virtual buffer through the real runtime, editor and ui; the tab, the
// text, the style, the buffer mode, the props and the close all work, and the buffer
// stays out of the session.
func TestPluginVirtualBufferEndToEnd(t *testing.T) {
	s := newVirtualSession(t)
	s.runCommand("Show Outline")
	s.until("the outline tab", func() bool { return s.screenHas("*Outline*") && s.screenHas("Heading B") })
	if got := s.text(); got != "Heading A\nHeading B\n" {
		t.Fatalf("text = %q", got)
	}
	doc := s.rt.ed.Active()
	if !doc.Virtual() || doc.Path != "" || doc.Modified() || !doc.ReadOnly() || doc.Mode() != "outline" {
		t.Fatalf("virtual=%v path=%q modified=%v read-only=%v mode=%q", doc.Virtual(), doc.Path, doc.Modified(), doc.ReadOnly(), doc.Mode())
	}

	// typing is swallowed by the mode and the read-only buffer would refuse it anyway
	s.typeText("x")
	if got := s.text(); got != "Heading A\nHeading B\n" {
		t.Fatalf("text after typing = %q", got)
	}
	// Enter runs the mode's binding, which reads the props at the cursor (the second entry)
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the props of the entry under the cursor", func() bool { return strings.Contains(s.statusRow(), "jump to 9") })

	// the session leaves it out
	sess := filepath.Join(t.TempDir(), "session")
	must(t, restore.SaveSession(s.rt.ed, sess, s.m.ExplorerState()))
	got, ok, err := session.Load(sess)
	must(t, err)
	if !ok || len(got.Files) != 1 || filepath.Base(got.Files[0].Path) != "a.txt" {
		t.Fatalf("session files = %+v, want only a.txt", got.Files)
	}

	// q closes the buffer, without a question, and the file is back
	s.typeText("q")
	s.until("the outline tab to go", func() bool { return !s.screenHas("*Outline*") })
	if got := s.text(); got != "world\n" {
		t.Fatalf("text after the close = %q, want the file", got)
	}
}

// Unloading the plugin closes the virtual buffers it made.
func TestPluginVirtualBufferGoesWithItsPlugin(t *testing.T) {
	s := newVirtualSession(t)
	s.runCommand("Show Outline")
	s.until("the outline tab", func() bool { return s.screenHas("*Outline*") })
	if _, notices := s.plug.Control(pluginapi.Control{Op: "disable", Name: "outline"}); len(notices) == 0 {
		t.Fatal("disable gave no notice")
	}
	s.until("the outline tab to go with its plugin", func() bool { return !s.screenHas("*Outline*") })
	if got := s.text(); got != "world\n" {
		t.Fatalf("text = %q, want the file", got)
	}
}
