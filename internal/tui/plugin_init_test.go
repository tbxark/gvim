package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/yuin/gopher-lua/parse"

	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/pluginapi"
)

// parseLua fails the test when src is not valid Lua 5.1.
func parseLua(t *testing.T, name, src string) {
	t.Helper()
	if _, err := parse.Parse(strings.NewReader(src), name); err != nil {
		t.Fatalf("%s is not valid Lua: %v\n%s", name, err, src)
	}
}

// initExamplesMarker is the line of initTemplate after which every line is an example.
const initExamplesMarker = "-- Examples"

// TestInitTemplate checks that the file Edit init.lua creates is valid Lua as it is, that
// its examples are valid Lua once uncommented, and that they use names the runtime has.
func TestInitTemplate(t *testing.T) {
	t.Parallel()
	parseLua(t, "init.lua", initTemplate)
	_, rest, ok := strings.Cut(initTemplate, initExamplesMarker)
	if !ok {
		t.Fatalf("the template has no %q line", initExamplesMarker)
	}
	_, rest, _ = strings.Cut(rest, "\n")
	var code []string
	for _, l := range strings.Split(rest, "\n") {
		l = strings.TrimPrefix(l, "--")
		code = append(code, strings.TrimPrefix(l, " "))
	}
	src := strings.Join(code, "\n")
	parseLua(t, "examples", src)
	for _, name := range []string{"gvim.command.register", "gvim.on", "gvim.notify"} {
		if !strings.Contains(src, name) {
			t.Errorf("the examples do not use %s", name)
		}
	}
}

func TestEnsureInit(t *testing.T) {
	t.Parallel()
	t.Run("creates the directory and the file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "cfg", "init.lua")
		created, err := ensureInit(path)
		if err != nil || !created {
			t.Fatalf("created %v, err %v", created, err)
		}
		if got, _ := os.ReadFile(path); string(got) != initTemplate {
			t.Fatalf("file = %q, want the template", got)
		}
	})
	t.Run("never overwrites", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "init.lua")
		if err := os.WriteFile(path, []byte("print(1)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		created, err := ensureInit(path)
		if err != nil || created {
			t.Fatalf("created %v, err %v", created, err)
		}
		if got, _ := os.ReadFile(path); string(got) != "print(1)\n" {
			t.Fatalf("file = %q, the user's script was changed", got)
		}
	})
	t.Run("a directory in the way is an error", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "init.lua")
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := appendInit(path, "x", "-- block\n"); err == nil {
			t.Fatal("no error for a directory named init.lua")
		}
	})
}

// TestAppendInitDuplicateCheck checks that only a command init.lua registers counts as a
// duplicate: the commented example of the template does not.
func TestAppendInitDuplicateCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing string // "" means the file made from the template
		cmd      string
		wantDup  bool
	}{
		{"template example is a comment", "", "hello", false},
		{"registered command", "gvim.command.register{\n  name = \"mine\",\n}\n", "mine", true},
		{"commented out command", "  -- name = \"mine\",\n", "mine", false},
		{"another name", "gvim.command.register{ name = \"mine\" }\n", "other", false},
		{"saved twice", "", "twice", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "init.lua")
			if tt.existing == "" {
				if _, err := ensureInit(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(tt.existing), 0o644); err != nil {
				t.Fatal(err)
			}
			block := macroBlock(tt.cmd, []Command{{Action: "move_down"}}, false)
			if tt.name == "saved twice" {
				if err := appendInit(path, tt.cmd, block); err != nil {
					t.Fatal(err)
				}
			}
			err := appendInit(path, tt.cmd, block)
			if (err != nil) != tt.wantDup {
				t.Fatalf("err = %v, want a duplicate error: %v", err, tt.wantDup)
			}
			if !tt.wantDup {
				got, _ := os.ReadFile(path)
				parseLua(t, "init.lua", string(got))
			}
		})
	}
}

// TestInitEdit checks the command: the file is created from the template when missing,
// opened in the editor either way, and a session without a config directory says so.
func TestInitEdit(t *testing.T) {
	t.Parallel()
	t.Run("missing file is created and opened", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "cfg", "init.lua")
		m, rt, ed := newMgrFixture(t)
		rt.initFile = path
		runPaletteAction(m, "init_edit")
		if len(ed.opened) != 1 || ed.opened[0] != path {
			t.Fatalf("opened %v, want %s", ed.opened, path)
		}
		if got, _ := os.ReadFile(path); string(got) != initTemplate {
			t.Fatalf("file = %q, want the template", got)
		}
		if m.notice != "Created init.lua from a template" {
			t.Fatalf("notice = %q", m.notice)
		}
	})
	t.Run("existing file is opened as it is", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "init.lua")
		if err := os.WriteFile(path, []byte("-- mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, rt, ed := newMgrFixture(t)
		rt.initFile = path
		runPaletteAction(m, "init_edit")
		if len(ed.opened) != 1 || ed.opened[0] != path {
			t.Fatalf("opened %v, want %s", ed.opened, path)
		}
		if got, _ := os.ReadFile(path); string(got) != "-- mine\n" {
			t.Fatalf("file = %q, the script was changed", got)
		}
	})
	t.Run("an unwritable location is the notice", func(t *testing.T) {
		t.Parallel()
		blocker := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocker, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		m, rt, ed := newMgrFixture(t)
		rt.initFile = filepath.Join(blocker, "init.lua") // the "directory" is a file
		runPaletteAction(m, "init_edit")
		if len(ed.opened) != 0 || !strings.HasPrefix(m.notice, "init.lua: ") {
			t.Fatalf("opened %v, notice %q", ed.opened, m.notice)
		}
	})
	t.Run("no config directory", func(t *testing.T) {
		t.Parallel()
		m, _, ed := newMgrFixture(t)
		runPaletteAction(m, "init_edit")
		if len(ed.opened) != 0 || !strings.Contains(m.notice, "no configuration directory") {
			t.Fatalf("opened %v, notice %q", ed.opened, m.notice)
		}
	})
	t.Run("a runtime without the optional port", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t)
		m, _ := newPluginModel(t, rt)
		runPaletteAction(m, "init_edit")
		if !strings.Contains(m.notice, "no configuration directory") {
			t.Fatalf("notice = %q", m.notice)
		}
	})
}

// macroSteps is a recorded macro with and without arguments.
func macroSteps() []Command {
	return []Command{
		{Action: "move_down"},
		{Action: "insert_char", Args: map[string]any{"text": `say "hi"` + "\n"}},
		{Action: "delete_line", Args: map[string]any{"count": 3, "keep": true, "end": 1.5}},
	}
}

// The exact text matters here: the plugin runtime has a test that runs these two blocks
// (TestMacroBlocksRunInTheRuntime), so a change to the shape must change both.
const (
	savedMacroBlock = `
-- Macro fix, recorded in gvim.
do
  local gvim = require "gvim"
  gvim.command.register{
    name = "fix",
    title = "Macro: fix",
    description = "Recorded macro (3 steps)",
    run = function()
      gvim.command.run_all{
        "move_down",
        {"insert_char", {text = "say \"hi\"\n"}},
        {"delete_line", {count = 3, ["end"] = 1.5, keep = true}},
      }
    end,
  }
end
`
	promotedMacroBlock = `
-- Macro fix, recorded in gvim.
do
  local gvim = require "gvim"
  gvim.command.register{
    name = "fix",
    title = "Macro: fix",
    description = "Recorded macro (3 steps)",
    run = function(ctx)
      -- This is plain Lua: change it freely.
      gvim.command.run("move_down")
      gvim.command.run("insert_char", {text = "say \"hi\"\n"})
      gvim.command.run("delete_line", {count = 3, ["end"] = 1.5, keep = true})
    end,
  }
end
`
)

func TestMacroBlock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		promote bool
		want    string
	}{
		{"saved", false, savedMacroBlock},
		{"promoted", true, promotedMacroBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := macroBlock("fix", macroSteps(), tt.promote)
			if got != tt.want {
				t.Fatalf("block =\n%s\nwant\n%s", got, tt.want)
			}
			parseLua(t, "block", got)
		})
	}
}

func TestLuaQuote(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"", `""`},
		{"plain", `"plain"`},
		{`a"b\c`, `"a\"b\\c"`},
		{"line\nbreak\ttab", `"line\nbreak\ttab"`},
		{"\x00\x1b[0m\x7f", `"\000\027[0m\127"`},
		{"héllo ✓", `"héllo ✓"`},
		{"bad\xffbyte", `"bad\255byte"`},
	}
	for _, tt := range tests {
		if got := luaQuote(tt.in); got != tt.want {
			t.Errorf("luaQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
		parseLua(t, "quote", "return "+luaQuote(tt.in))
	}
}

func TestLuaValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{true, "true"},
		{3, "3"},
		{int64(-4), "-4"},
		{2.5, "2.5"},
		{"s", `"s"`},
		{[]string{"a", "b"}, `{"a", "b"}`},
		{[]any{1, "x", false}, `{1, "x", false}`},
		{map[string]any{"b": 1, "a": map[string]any{"end": true, "has space": 2}}, `{a = {["end"] = true, ["has space"] = 2}, b = 1}`},
		{struct{ X int }{1}, `"{1}"`},
	}
	for _, tt := range tests {
		if got := luaValue(tt.in); got != tt.want {
			t.Errorf("luaValue(%#v) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// recordedMacro makes the root hold a macro of two steps.
func recordedMacro(m *Model) {
	m.root.macro.last = []macroStep{
		{cmd: Command{Action: "move_down"}},
		{cmd: Command{Action: "insert_char", Args: map[string]any{"text": "x"}}},
	}
}

// TestSaveMacroToInit drives the commands: the prompt asks for a name, and the answer
// appends the block to init.lua (creating it from the template), so the file stays valid
// Lua. A name that init.lua already defines is refused.
func TestSaveMacroToInit(t *testing.T) {
	t.Parallel()
	t.Run("saved form", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "cfg", "init.lua")
		m, rt, _ := newMgrFixture(t)
		rt.initFile = path
		recordedMacro(m)
		runPaletteAction(m, "prompt_save_macro_to_init")
		if len(m.overlays) != 1 {
			t.Fatalf("%d overlays, want the name prompt", len(m.overlays))
		}
		typeInto(m, "fix")
		feed(m, runKey(m, special(tea.KeyEnter)))
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(got)
		if !strings.HasPrefix(src, initTemplate) {
			t.Error("the file does not start with the template")
		}
		for _, want := range []string{`name = "fix"`, `gvim.command.run_all{`, `"move_down"`, `{"insert_char", {text = "x"}}`} {
			if !strings.Contains(src, want) {
				t.Errorf("init.lua does not contain %s:\n%s", want, src)
			}
		}
		parseLua(t, "init.lua", src)
		if want := `Macro saved as init.fix. Run "init: Reload init.lua" to use it.`; m.notice != want {
			t.Fatalf("notice = %q, want %q", m.notice, want)
		}

		// The same name again is refused and the file stays as it is.
		recordedMacro(m)
		runPaletteAction(m, "prompt_save_macro_to_init")
		typeInto(m, "fix")
		feed(m, runKey(m, special(tea.KeyEnter)))
		if again, _ := os.ReadFile(path); string(again) != src {
			t.Error("a duplicate name changed init.lua")
		}
		if !strings.Contains(m.notice, "already defines a command called \"fix\"") {
			t.Fatalf("notice = %q", m.notice)
		}
	})
	t.Run("promoted form appends after an unterminated last line", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "init.lua")
		if err := os.WriteFile(path, []byte("local x = 1"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, rt, _ := newMgrFixture(t)
		rt.initFile = path
		recordedMacro(m)
		runPaletteAction(m, "prompt_promote_macro")
		typeInto(m, "edit_me")
		feed(m, runKey(m, special(tea.KeyEnter)))
		got, _ := os.ReadFile(path)
		src := string(got)
		if !strings.HasPrefix(src, "local x = 1\n\n-- Macro edit_me") || !strings.Contains(src, `gvim.command.run("move_down")`) {
			t.Fatalf("init.lua =\n%s", src)
		}
		parseLua(t, "init.lua", src)
	})
	t.Run("no macro", func(t *testing.T) {
		t.Parallel()
		m, rt, _ := newMgrFixture(t)
		rt.initFile = filepath.Join(t.TempDir(), "init.lua")
		runPaletteAction(m, "prompt_save_macro_to_init")
		if m.notice != "No macro recorded" || len(m.overlays) != 0 {
			t.Fatalf("notice %q, %d overlays", m.notice, len(m.overlays))
		}
	})
	t.Run("esc writes nothing", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "init.lua")
		m, rt, _ := newMgrFixture(t)
		rt.initFile = path
		recordedMacro(m)
		runPaletteAction(m, "prompt_save_macro_to_init")
		feed(m, runKey(m, special(tea.KeyEscape)))
		if _, err := os.Stat(path); err == nil {
			t.Fatal("init.lua was created by a cancelled prompt")
		}
	})
	t.Run("no config directory", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newMgrFixture(t)
		recordedMacro(m)
		runPaletteAction(m, "prompt_save_macro_to_init")
		typeInto(m, "fix")
		feed(m, runKey(m, special(tea.KeyEnter)))
		if !strings.Contains(m.notice, "no configuration directory") {
			t.Fatalf("notice = %q", m.notice)
		}
	})
}

func TestValidateMacroName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"fix", true}, {"a1_b2", true}, {" padded ", true}, {strings.Repeat("a", 40), true},
		{"", false}, {"1abc", false}, {"Fix", false}, {"has space", false}, {"dash-ed", false},
		{strings.Repeat("a", 41), false}, {"é", false},
	}
	for _, tt := range tests {
		if err := validateMacroName(tt.in); (err == nil) != tt.ok {
			t.Errorf("validateMacroName(%q) = %v, want ok=%v", tt.in, err, tt.ok)
		}
	}
}

// TestPluginInitMessageIgnoresForeignMessages checks that the group does not take a prompt
// answer or a message that is not its own.
func TestPluginInitMessageIgnoresForeignMessages(t *testing.T) {
	t.Parallel()
	m, _, _ := newMgrFixture(t)
	for _, msg := range []tea.Msg{prompt.Done{Tag: "other"}, prompt.Done{}, pluginapi.Event{}, 42} {
		if _, ok := m.pluginManagerMessage(msg); ok {
			t.Errorf("the group took %T", msg)
		}
	}
}
