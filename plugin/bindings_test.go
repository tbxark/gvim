package plugin_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// luaString quotes s as a Lua 5.1 string literal (decimal escapes, no \x).
func luaString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			fmt.Fprintf(&sb, "\\%03d", c)
			continue
		}
		sb.WriteByte(c)
	}
	sb.WriteByte('"')
	return sb.String()
}

func TestBindingChordChecks(t *testing.T) {
	rows := []struct {
		chord string
		want  string // the normalised chord, or "invalid"
	}{
		{"ctrl+alt+b", "ctrl+alt+b"},
		{"  ctrl+k   ctrl+c ", "ctrl+k ctrl+c"},
		{"F10", "F10"},
		{"g g", "g g"},
		{"+", "+"},
		{"", "invalid"},
		{"   ", "invalid"},
		{"a b c d", "a b c d"},
		{"a b c d e", "invalid"},
		{"ctrl+" + strings.Repeat("x", 40), "invalid"},
		{"ctrl+a\x00", "invalid"},
		{"ctrl+\x1b", "invalid"},
		{"ctrl+\xff", "invalid"},
	}
	var lua strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&lua, "do local c, err = B.check(%s) T.spy(c or gvim.err_code(err)) end\n", luaString(r.chord))
	}
	h := start(t, gv+lua.String())
	got := spy(h)
	if len(got) != len(rows) {
		t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
	}
	for i, r := range rows {
		if got[i] != r.want {
			t.Errorf("checkChord(%q) = %q, want %q", r.chord, got[i], r.want)
		}
	}
}

func TestBindingSet(t *testing.T) {
	t.Run("set replaces by chord ignoring case and publishes the whole set", func(t *testing.T) {
		h := start(t, `
T.command("a", function() end)
T.command("b", function() end)
T.spy(B.set("ctrl+a", "t.a"), B.set("ctrl+b", "t.a"))
T.spy(B.set("Ctrl+A", "t.b"))
T.spy(B.del("ctrl+b"), B.del("ctrl+b"))`)
		if got := spyText(h); got != "true true|true|true false" {
			t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
		}
		want := []pluginapi.Binding{{Chord: "Ctrl+A", Action: "t.b", Plugin: "t"}}
		if got := h.Host.Bindings["t"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("bindings = %+v, want %+v", got, want)
		}
		// one SetBindings per change that did something (three sets, one effective del)
		if got := h.Host.CallCount("SetBindings"); got != 4 {
			t.Fatalf("SetBindings called %d times, want 4", got)
		}
	})

	t.Run("invalid chords never reach the host", func(t *testing.T) {
		h := start(t, gv+`
for _, c in ipairs{ "", "a b c d e" } do
  local ok, err = B.set(c, "t.a")
  T.spy(ok, gvim.err_code(err))
end`)
		if got := spyText(h); got != "nil invalid|nil invalid" {
			t.Fatalf("spy = %q", got)
		}
		if n := h.Host.CallCount("SetBindings"); n != 0 {
			t.Fatalf("SetBindings called %d times", n)
		}
	})

	t.Run("a function becomes a hidden command that goes with its binding", func(t *testing.T) {
		h := start(t, gv+`
T.spy(B.fn("ctrl+f", function() T.spy("fn ran") end))
T.spy(#gvim.command.list{ plugin = "t" })   -- hidden commands are not listed`)
		if got := h.Host.Bindings["t"]; len(got) != 1 || !strings.HasPrefix(got[0].Action, "t.__fn") {
			t.Fatalf("bindings = %+v", got)
		}
		id := h.Host.Bindings["t"][0].Action
		hidden := false
		for _, c := range h.Host.Commands["t"] {
			if c.ID == id && c.Hidden {
				hidden = true
			}
		}
		if !hidden {
			t.Fatalf("hidden command %s missing from %+v", id, h.Host.Commands["t"])
		}
		if err := h.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		if got := spyText(h); got != "true|0|fn ran" {
			t.Fatalf("spy = %q", got)
		}
		// Rebinding the chord, or deleting it, takes the hidden command with it.
		for _, step := range []string{`B.del("ctrl+f")`, `B.set("ctrl+f", "t.other")`} {
			h2 := start(t, gv+`B.fn("ctrl+f", function() end)`+"\n"+step)
			if n := len(h2.Host.Commands["t"]); n != 0 {
				t.Fatalf("after %s: %d commands left: %+v", step, n, h2.Host.Commands["t"])
			}
		}
	})

	t.Run("a hidden command cannot be unregistered or registered by name", func(t *testing.T) {
		h := start(t, gv+`
B.fn("ctrl+f", function() end)
T.spy(gvim.err_code(select(2, gvim.command.unregister("__fn1"))))
T.spy(gvim.err_code(select(2, gvim.command.register{ name = "__fn1", run = function() end })))`)
		if got := spyText(h); got != "not_found|invalid" {
			t.Fatalf("spy = %q", got)
		}
		if len(h.Host.Commands["t"]) != 1 {
			t.Fatalf("commands = %+v", h.Host.Commands["t"])
		}
	})

	t.Run("quota", func(t *testing.T) {
		h := start(t, gv+`
T.spy(B.fill(4096))
local ok, err = B.set("one more", "t.x")
T.spy(ok, gvim.err_code(err))
-- replacing an existing chord needs no room
T.spy(B.set("K1", "t.y"))
local over, err2 = B.fill(4097)
T.spy(over, gvim.err_code(err2))`)
		if got := spyText(h); got != "true|nil limit|true|nil limit" {
			t.Fatalf("spy = %q", got)
		}
	})

	t.Run("state is per plugin and gone with the plugin", func(t *testing.T) {
		h := startFiles(t, plugintest.Files{
			"a.lua": `B.set("ctrl+a", "a.x")`,
			"b.lua": `B.set("ctrl+a", "b.x")`,
		})
		if len(h.Host.Bindings["a"]) != 1 || len(h.Host.Bindings["b"]) != 1 || h.Host.Bindings["b"][0].Action != "b.x" {
			t.Fatalf("bindings = %+v", h.Host.Bindings)
		}
		h.Unload("a")
		if len(h.Host.Bindings["a"]) != 0 || len(h.Host.Bindings["b"]) != 1 {
			t.Fatalf("bindings after unload = %+v", h.Host.Bindings)
		}
	})
}
