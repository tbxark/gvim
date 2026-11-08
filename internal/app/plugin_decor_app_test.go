package app

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// TestDocumentAdapterNamesItsBuffer checks the id the plugins know a document by, which the
// decorations of the document are filed under.
func TestDocumentAdapterNamesItsBuffer(t *testing.T) {
	ed := editor.New(editor.Options{})
	ed.Do("new", nil)
	deps := uiadapter.NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil)
	adapter := deps.Editor.(*uiadapter.Editor)
	for i := range ed.Tabs() {
		d, ok := adapter.DocAt(i).(interface{ BufferID() pluginapi.BufferID })
		if !ok {
			t.Fatalf("tab %d: DocAt is %T, want a bufferIdentified document", i, adapter.DocAt(i))
		}
		if want := pluginapi.BufferID(ed.DocAt(i).ID()); d.BufferID() != want {
			t.Errorf("tab %d: BufferID = %v, want %d", i, d.BufferID(), want)
		}
	}
}

// decorPlugin marks a line of the active buffer through the plugin API.
const decorPlugin = `
local gvim = require "gvim"
local ns = gvim.decor.namespace("note")

gvim.command.register{
  name = "mark", title = "Decor Mark",
  run = function()
    local b = gvim.buf.current()
    local ok, err = ns:set(b, {
      {kind = "eol", line = 1, text = " <-- note", style = {fg = "#00ff00"}},
      {kind = "highlight", range = gvim.range(1, 1, 1, 4), style = {bg = "#0000ff"}},
      {kind = "replace", pos = gvim.pos(1, 0), text = "W", style = {fg = "#ff0000"}},
    }, {version = b:version()})
    gvim.notify(ok and "marked" or ("failed: " .. tostring(err)))
  end,
}

gvim.command.register{
  name = "shift", title = "Decor Shift",
  run = function()
    gvim.buf.current():insert(gvim.pos(0, 0), "new\n")
  end,
}
`

// TestDecorationsEndToEnd runs a plugin through the real runtime, editor and ui: it puts
// decorations on a line, an edit moves them with the text, and the frame shows them.
func TestDecorationsEndToEnd(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/decor.lua", decorPlugin)
	s := newSession(t, Options{})
	s.openFile("a.txt", "hello\nworld\nlast\n")
	s.waitStatus("decor", "loaded")

	s.runCommand("Decor Mark")
	if got := s.statusRow(); !strings.Contains(got, "marked") {
		t.Fatalf("status row = %q, want marked", got)
	}
	rowOf := func(sub string) string {
		for _, r := range s.rows() {
			if strings.Contains(r, sub) {
				return r
			}
		}
		t.Fatalf("no row has %q:\n%s", sub, strings.Join(s.rows(), "\n"))
		return ""
	}
	// The replacement draws a W over the w of "world"; the note follows the line.
	if r := rowOf("World"); !strings.Contains(r, "World <-- note") {
		t.Fatalf("row of world = %q", r)
	}
	if r := rowOf("hello"); strings.Contains(r, "note") {
		t.Fatalf("the note is on the wrong line: %q", r)
	}

	// A plugin edit above the line moves the decorations with it, before any event runs.
	s.runCommand("Decor Shift")
	if got := s.text(); got != "new\nhello\nworld\nlast\n" {
		t.Fatalf("text = %q", got)
	}
	if r := rowOf("World"); !strings.Contains(r, "<-- note") {
		t.Fatalf("the note did not follow the line: %q", r)
	}
	// The same through typing: a newline at the start of the first line.
	s.rt.ed.Active().Buffer.Insert(buffer.Pos{}, "\n")
	if r := rowOf("World"); !strings.Contains(r, "<-- note") {
		t.Fatalf("typing above lost the note: %q", r)
	}
}
