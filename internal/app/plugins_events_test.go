package app

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	expanel "github.com/tbxark/gvim/internal/ui/explorer"
)

// eventsPlugin logs the events of the editor as "EV <name> <fields>" lines in the plugin
// log, which the tests read back.
const eventsPlugin = `
local gvim = require "gvim"
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local function log(...) gvim.log.info(...) end

gvim.on("after_file_open", function(e) log("EV open %s", e.path) end)
gvim.on("buffer_activated", function(e) log("EV activated %s", e.buf:name()) end)
gvim.on("buffer_deactivated", function(e) log("EV deactivated %s", e.buf:name()) end)
gvim.on("buffer_closed", function(e) log("EV closed %s", e.path) end)
gvim.on("before_file_save", function(e) log("EV before_save %s %s", e.buf:name(), e.source) end)
gvim.on("after_file_save", function(e) log("EV after_save %s", e.path) end)
gvim.on("buffer_modified", function(e) log("EV modified %s %s", e.buf:name(), tostring(e.modified)) end)
gvim.on("cursor_moved", function(e) log("EV cursor %d:%d from %d:%d", e.pos.line, e.pos.col, e.previous.line, e.previous.col) end)
gvim.on("post_command", function(e) if e.action == "save" then log("EV post_command save ok=%s", tostring(e.ok)) end end)
gvim.on("buffer_changed", function(e)
  if e.overflow then log("CHG overflow") return end
  local parts = {}
  for _, d in ipairs(e.edits) do
    parts[#parts + 1] = string.format("%d:%d:%d:%d:%s:%s", d.range.from.line, d.range.from.col,
      d.range.to.line, d.range.to.col, hex(d.text), hex(d.removed))
  end
  log("CHG %s %s", tostring(e.undo), table.concat(parts, ";"))
end)

gvim.command.register{
  name = "double_edit", title = "Double Edit",
  run = function()
    local b = gvim.buf.current()
    b:undo_group(function()
      b:insert({ line = 0, col = 0 }, "é")
      b:insert({ line = 0, col = 2 }, "ü")
    end)
  end,
}
`

// pluginLines returns the log lines of the events plugin that start with prefix, without it.
func (s *pluginSession) pluginLines(prefix string) []string {
	var out []string
	for _, e := range s.plug.Log() {
		if e.Plugin == "events" && strings.HasPrefix(e.Msg, prefix) {
			out = append(out, strings.TrimPrefix(e.Msg, prefix))
		}
	}
	return out
}

func newEventsSession(t *testing.T) *pluginSession {
	t.Helper()
	isolateConfig(t)
	writeUserFile(t, "plugins/events.lua", eventsPlugin)
	s := newSession(t, Options{})
	s.waitStatus("events", "loaded")
	// The subscriptions exist now; the next message turns the change tracking on.
	s.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	return s
}

// TestPluginEventsEndToEnd runs the raise sites against the real editor and runtime: a
// plugin hears files open and close, tabs switch, text change, the caret move and a save.
func TestPluginEventsEndToEnd(t *testing.T) {
	s := newEventsSession(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("alpha\n"), 0o644))
	must(t, os.WriteFile(b, []byte("beta\n"), 0o644))

	s.send(expanel.OpenFileMsg{Path: a})
	s.send(expanel.OpenFileMsg{Path: b})
	s.settle(20 * time.Millisecond)
	if got, want := s.pluginLines("EV open "), []string{a, b}; !slices.Equal(got, want) {
		t.Fatalf("after_file_open = %v, want %v", got, want)
	}
	if got, want := s.pluginLines("EV activated "), []string{"a.txt", "b.txt"}; !slices.Equal(got, want) {
		t.Fatalf("buffer_activated = %v, want %v", got, want)
	}
	if got, want := s.pluginLines("EV deactivated "), []string{"a.txt"}; !slices.Equal(got, want) {
		t.Fatalf("buffer_deactivated = %v, want %v", got, want)
	}

	// Typing moves the caret and modifies the buffer; saving raises the pair of save events
	// and the buffer is no longer modified.
	s.typeText("x")
	s.settle(20 * time.Millisecond)
	if got := s.pluginLines("EV cursor "); len(got) == 0 {
		t.Fatal("no cursor_moved after typing")
	}
	if got, want := s.pluginLines("EV modified "), []string{"b.txt true"}; !slices.Equal(got, want) {
		t.Fatalf("buffer_modified = %v, want %v", got, want)
	}
	s.press(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	s.until("after_file_save", func() bool { return len(s.pluginLines("EV after_save ")) > 0 })
	s.settle(20 * time.Millisecond)
	if got, want := s.pluginLines("EV before_save "), []string{"b.txt manual"}; !slices.Equal(got, want) {
		t.Fatalf("before_file_save = %v, want %v", got, want)
	}
	if got, want := s.pluginLines("EV after_save "), []string{b}; !slices.Equal(got, want) {
		t.Fatalf("after_file_save = %v, want %v", got, want)
	}
	if got, want := s.pluginLines("EV modified "), []string{"b.txt true", "b.txt false"}; !slices.Equal(got, want) {
		t.Fatalf("buffer_modified = %v, want %v", got, want)
	}
	if got, want := s.pluginLines("EV post_command "), []string{"save ok=true"}; !slices.Equal(got, want) {
		t.Fatalf("post_command = %v, want %v", got, want)
	}

	// Closing the active tab closes b and activates a.
	s.press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	s.settle(20 * time.Millisecond)
	if got, want := s.pluginLines("EV closed "), []string{b}; !slices.Equal(got, want) {
		t.Fatalf("buffer_closed = %v, want %v", got, want)
	}
	if got, want := s.pluginLines("EV activated "), []string{"a.txt", "b.txt", "a.txt"}; !slices.Equal(got, want) {
		t.Fatalf("buffer_activated = %v, want %v", got, want)
	}
}

// changeApplier applies buffer_changed records, in order, to a mirror of the text.
type changeApplier struct {
	t      *testing.T
	mirror string
}

// offset converts a line and byte column of the mirror to a byte offset.
func (c *changeApplier) offset(line, col int) int {
	off := 0
	for range line {
		i := strings.IndexByte(c.mirror[off:], '\n')
		if i < 0 {
			c.t.Fatalf("line %d is past the end of %q", line, c.mirror)
		}
		off += i + 1
	}
	return off + col
}

// apply applies one CHG line: "undo l:c:l:c:text:removed;...".
func (c *changeApplier) apply(entry string) {
	c.t.Helper()
	_, recs, _ := strings.Cut(entry, " ")
	if recs == "" {
		return
	}
	for _, r := range strings.Split(recs, ";") {
		f := strings.Split(r, ":")
		if len(f) != 6 {
			c.t.Fatalf("bad record %q", r)
		}
		n := make([]int, 4)
		for i := range n {
			v, err := strconv.Atoi(f[i])
			if err != nil {
				c.t.Fatalf("bad record %q: %v", r, err)
			}
			n[i] = v
		}
		text, _ := hex.DecodeString(f[4])
		removed, _ := hex.DecodeString(f[5])
		from, to := c.offset(n[0], n[1]), c.offset(n[2], n[3])
		if from > to || to > len(c.mirror) {
			c.t.Fatalf("record %q addresses %d..%d of a mirror of %d bytes", r, from, to, len(c.mirror))
		}
		if len(removed) > 0 && c.mirror[from:to] != string(removed) {
			c.t.Fatalf("record %q removes %q but the mirror holds %q there", r, removed, c.mirror[from:to])
		}
		c.mirror = c.mirror[:from] + string(text) + c.mirror[to:]
	}
}

// TestBufferChangedSequentialEdits is AMENDMENTS C13 end to end: the records of
// buffer_changed, applied first to last to a mirror of the text, reproduce the buffer after
// typing, deleting, Enter, undo, redo and a command that edits twice, on multi-byte text.
func TestBufferChangedSequentialEdits(t *testing.T) {
	s := newEventsSession(t)
	path := filepath.Join(t.TempDir(), "m.txt")
	must(t, os.WriteFile(path, []byte("héllo\nwörld ✓\nlast\n"), 0o644))
	s.send(expanel.OpenFileMsg{Path: path})
	s.settle(20 * time.Millisecond)
	if len(s.pluginLines("CHG ")) != 0 {
		t.Fatalf("opening the file raised buffer_changed: %v", s.pluginLines("CHG "))
	}
	c := &changeApplier{t: t, mirror: s.text()}

	keys := []tea.KeyPressMsg{
		{Code: tea.KeyEnter}, {Code: tea.KeyBackspace}, {Code: tea.KeyDelete},
		{Code: 'z', Mod: tea.ModCtrl}, {Code: 'r', Mod: tea.ModCtrl}, {Code: tea.KeyLeft}, {Code: tea.KeyDown},
	}
	letters := []string{"a", "é", "✓", " ", "z", "ö"}
	rng := rand.New(rand.NewSource(7))
	seen := 0
	for i := range 60 {
		var op string
		switch n := rng.Intn(10); {
		case n < 4:
			l := letters[rng.Intn(len(letters))]
			s.typeText(l)
			op = "type " + l
		case n == 4 && i%3 == 0:
			s.runCommand("Double Edit")
			op = "double edit"
		default:
			k := keys[rng.Intn(len(keys))]
			s.press(k)
			op = fmt.Sprintf("key %v", k)
		}
		s.settle(2 * time.Millisecond)
		lines := s.pluginLines("CHG ")
		for _, l := range lines[seen:] {
			if l == "overflow" {
				t.Fatalf("step %d (%s): unexpected overflow", i, op)
			}
			c.apply(l)
		}
		seen = len(lines)
		if got := s.text(); c.mirror != got {
			t.Fatalf("step %d (%s): mirror %q != buffer %q", i, op, c.mirror, got)
		}
	}
	if seen < 20 {
		t.Fatalf("only %d buffer_changed events for 60 operations", seen)
	}
}

// TestBufferChangedConcatenatesOneUpdate checks that the two inserts of one command arrive
// as one event with two records, in order.
func TestBufferChangedConcatenatesOneUpdate(t *testing.T) {
	s := newEventsSession(t)
	path := filepath.Join(t.TempDir(), "d.txt")
	must(t, os.WriteFile(path, []byte("x\n"), 0o644))
	s.send(expanel.OpenFileMsg{Path: path})
	s.settle(20 * time.Millisecond)
	s.runCommand("Double Edit")
	s.settle(20 * time.Millisecond)
	got := s.pluginLines("CHG ")
	if len(got) != 1 {
		t.Fatalf("buffer_changed events = %v, want one for the whole command", got)
	}
	_, recs, _ := strings.Cut(got[0], " ")
	if n := len(strings.Split(recs, ";")); n != 2 {
		t.Fatalf("event %q has %d records, want 2", got[0], n)
	}
	if s.text() != "éüx\n" {
		t.Fatalf("text = %q", s.text())
	}
}
