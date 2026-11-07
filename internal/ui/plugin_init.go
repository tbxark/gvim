package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/prompt"
)

// The init.lua commands: Edit init.lua, and the two commands that write a recorded macro
// into it (FINAL X11). init.lua is the user's own script, loaded last as the plugin
// "init"; Check and Reload go to the runtime (pluginUIAction). Editing and writing it is
// disk work, so it runs in a tea.Cmd and its result comes back as a message; Update never
// touches the disk.

// initTemplate is the init.lua that Edit init.lua creates when there is none. It runs as
// it is (it only has comments), and the examples after the "Examples" line are Lua once
// the leading "-- " is taken off (TestInitTemplateExamplesParse).
const initTemplate = `-- init.lua: your own Lua script for gvim. It loads after every other plugin.
--
-- Edit it, save it, then run "init: Check init.lua" to look for syntax errors and
-- "init: Reload init.lua" to apply the change. "Plugin: Show Log" shows what it printed
-- and why it failed. Everything the editor offers is in the gvim table, which you get
-- with require.
--
-- Examples (remove the leading "-- " to try one):
--
-- local gvim = require "gvim"
--
-- gvim.command.register{
--   name = "hello",                  -- the command is "init.hello" in the palette
--   title = "Hello from init.lua",
--   run = function(ctx)
--     gvim.notify("Hello from init.lua")
--   end,
-- }
--
-- gvim.on("after_file_save", function(ev)
--   gvim.notify("Saved " .. (ev.path or "a buffer"))
-- end)
`

// initExamplesMarker is the line of initTemplate after which every line is an example.
const initExamplesMarker = "-- Examples"

// initFilePath is the path of init.lua, or "" when the runtime has none.
func (m *Model) initFilePath() string {
	f, ok := m.plug.rt.(pluginFiles)
	if !ok {
		return ""
	}
	return f.InitFile()
}

// initFile is initFilePath, with the notice that says why there is none.
func (m *Model) initFile() (string, bool) {
	path := m.initFilePath()
	if path == "" {
		m.notice = "There is no init.lua: gvim has no configuration directory"
	}
	return path, path != ""
}

// ensureInit creates init.lua from the template when it does not exist, and its
// directory with it. It reports whether it created the file. It never overwrites.
func ensureInit(path string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, werr := f.WriteString(initTemplate)
	return true, errors.Join(werr, f.Close())
}

// initEditMsg is the answer of the command that makes sure init.lua exists.
type initEditMsg struct {
	path    string
	created bool
	err     error
}

// initEdit opens init.lua in a tab, creating it from the template first when it is
// missing.
func (m *Model) initEdit() tea.Cmd {
	path, ok := m.initFile()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		created, err := ensureInit(path)
		return initEditMsg{path: path, created: created, err: err}
	}
}

// ---- macros

// macroInitTag tags the name prompt of Macro: Save to init.lua (promote false) and of
// Macro: Promote to command.
type macroInitTag struct{ promote bool }

// macroName is the form of a command name in a plugin: the runtime accepts
// [a-z][a-z0-9_]*, and 40 characters are plenty.
var macroName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func validateMacroName(text string) error {
	if !macroName.MatchString(strings.TrimSpace(text)) {
		return errors.New("a command name is a-z, 0-9 and _, starting with a letter (40 at most)")
	}
	return nil
}

// askMacroName asks what to call the command the last macro becomes. With no macro it
// says so instead.
func (m *Model) askMacroName(promote bool) {
	if len(m.root.macro.last) == 0 {
		m.notice = "No macro recorded"
		return
	}
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label:     "Command name for the macro:",
		Input:     "macro",
		SelectAll: true,
		Tag:       macroInitTag{promote: promote},
		Validate:  validateMacroName,
	})})
}

// initSavedMsg is the answer of the command that appended a macro to init.lua.
type initSavedMsg struct {
	name string
	err  error
}

// pluginInitMessage handles the messages of the init commands: the name a macro prompt
// answered, and the results of the commands that write init.lua. It reports whether msg
// was one of them.
func (m *Model) pluginInitMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case prompt.Done:
		tag, ok := msg.Tag.(macroInitTag)
		if !ok {
			return nil, false
		}
		if msg.Outcome != prompt.Submitted {
			return nil, true
		}
		return m.saveMacro(strings.TrimSpace(msg.Text), tag.promote), true
	case initEditMsg:
		if msg.err != nil {
			m.notice = "init.lua: " + msg.err.Error()
			return nil, true
		}
		m.openFile(msg.path, false)
		m.focusEditor()
		if msg.created {
			m.notice = "Created init.lua from a template"
		}
		return nil, true
	case initSavedMsg:
		if msg.err != nil {
			m.notice = "Saving the macro failed: " + msg.err.Error()
			return nil, true
		}
		m.notice = fmt.Sprintf(`Macro saved as init.%s. Run "init: Reload init.lua" to use it.`, msg.name)
		return nil, true
	}
	return nil, false
}

// saveMacro appends the last macro to init.lua as the command name. The text is made
// now, from the macro as it is; the disk work is a command.
func (m *Model) saveMacro(name string, promote bool) tea.Cmd {
	path, ok := m.initFile()
	if !ok {
		return nil
	}
	steps := make([]Command, len(m.root.macro.last))
	for i, s := range m.root.macro.last {
		steps[i] = s.cmd
	}
	block := macroBlock(name, steps, promote)
	return func() tea.Msg {
		return initSavedMsg{name: name, err: appendInit(path, name, block)}
	}
}

// appendInit appends block to init.lua, creating the file from the template if it is
// missing. It refuses when the file already registers a command called name, because
// registering one name twice fails the whole script. The check is a text search for
// `name = "<name>"` in the lines that are not comments.
func appendInit(path, name, block string) error {
	if _, err := ensureInit(path); err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if definesCommand(old, name) {
		return fmt.Errorf("init.lua already defines a command called %q", name)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		block = "\n" + block
	}
	_, werr := f.WriteString(block)
	return errors.Join(werr, f.Close())
}

// definesCommand reports whether src has a line `name = "<name>"` that is not a comment.
func definesCommand(src []byte, name string) bool {
	want := []byte(`name = "` + name + `"`)
	for line := range bytes.Lines(src) {
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("--")) && bytes.Contains(line, want) {
			return true
		}
	}
	return false
}

// macroBlock is the Lua that registers the macro as the command name. It is self
// contained (it requires gvim itself), so it works after any init.lua. Saved, the command
// calls run_all over the steps, which makes one undo group. Promoted, it is a function
// with one command.run per step, for the user to extend.
func macroBlock(name string, steps []Command, promote bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n-- Macro %s, recorded in gvim.\ndo\n  local gvim = require \"gvim\"\n  gvim.command.register{\n", name)
	fmt.Fprintf(&b, "    name = %s,\n    title = %s,\n    description = %s,\n",
		luaQuote(name), luaQuote("Macro: "+name), luaQuote(fmt.Sprintf("Recorded macro (%d steps)", len(steps))))
	if promote {
		b.WriteString("    run = function(ctx)\n      -- This is plain Lua: change it freely.\n")
		for _, s := range steps {
			if len(s.Args) == 0 {
				fmt.Fprintf(&b, "      gvim.command.run(%s)\n", luaQuote(s.Action))
			} else {
				fmt.Fprintf(&b, "      gvim.command.run(%s, %s)\n", luaQuote(s.Action), luaValue(s.Args))
			}
		}
		b.WriteString("    end,\n")
	} else {
		b.WriteString("    run = function()\n      gvim.command.run_all{\n")
		for _, s := range steps {
			if len(s.Args) == 0 {
				fmt.Fprintf(&b, "        %s,\n", luaQuote(s.Action))
			} else {
				fmt.Fprintf(&b, "        {%s, %s},\n", luaQuote(s.Action), luaValue(s.Args))
			}
		}
		b.WriteString("      }\n    end,\n")
	}
	b.WriteString("  }\nend\n")
	return b.String()
}

// luaQuote quotes s as a Lua 5.1 string literal. Lua 5.1 has decimal escapes (\ddd) and no
// \x or \u, so a control character, and a byte that is not valid UTF-8, are written as
// three digits.
func luaQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || (r == utf8.RuneError && size == 1):
			fmt.Fprintf(&b, `\%03d`, s[i])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

// luaKeywords are the words that cannot be a field name written bare.
var luaKeywords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true,
	"false": true, "for": true, "function": true, "goto": true, "if": true, "in": true,
	"local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true,
	"then": true, "true": true, "until": true, "while": true,
}

var luaIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// luaValue writes v as a Lua expression. The values of a command's args are strings,
// numbers, booleans and tables of them; anything else is written as its text.
func luaValue(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return luaQuote(fmt.Sprint(v))
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return luaQuote(v)
	case []string:
		items := make([]any, len(v))
		for i, s := range v {
			items[i] = s
		}
		return luaValue(items)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = luaValue(e)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			if luaIdent.MatchString(k) && !luaKeywords[k] {
				parts[i] = k + " = " + luaValue(v[k])
			} else {
				parts[i] = "[" + luaQuote(k) + "] = " + luaValue(v[k])
			}
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return luaQuote(fmt.Sprint(v))
}
