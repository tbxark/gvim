package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
)

func TestSyntaxErrorsNameFileLineAndColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src, want string
	}{
		{"stray end", "local x = 1\nreturn x +\nend\n", "p/a.lua:3:3: syntax error near 'end'"},
		{"double equals", "local x = = 1\n", "p/a.lua:1:11: syntax error near '='"},
		{"unfinished block", "if true then\n", "p/a.lua:1: syntax error near <eof>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := plugin.CompileSource("p/a.lua", tt.src)
			if err == nil {
				t.Fatal("no error")
			}
			if got := err.Error(); !strings.HasPrefix(got, tt.want) {
				t.Fatalf("error = %q, want it to start with %q", got, tt.want)
			}
			if strings.Contains(err.Error(), "line:") || strings.Contains(err.Error(), "column:") || strings.Contains(err.Error(), "  ") {
				t.Fatalf("error %q keeps gopher-lua's wording", err)
			}
		})
	}
}

// A call of a nil value names what was called, where gopher-lua's compiler recorded it.
func TestCallErrorsNameTheCallee(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, want string
	}{
		{"field", "local gvim = require 'gvim'\ngvim.notifyy('x')", "(field 'notifyy')"},
		{"global", "nosuch('x')", "(global 'nosuch')"},
		{"method", "local s = 'x'\ns:nosuch()", "(method 'nosuch')"},
		{"local", "local f\nf()", "(local 'f')"},
		{"upvalue", "local f\nlocal function g() f() end\ng()", "(upvalue 'f')"},
		{"two calls on the line", "local t = {}\nt.a(t.b())", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := start(t, "T.command('go', function()\n"+tt.body+"\nend)")
			_ = h.Run("t.go", nil)
			log := strings.Join(h.LogText("t"), "\n")
			if !strings.Contains(log, "attempt to call a non-function object") {
				t.Fatalf("no call error: %s", log)
			}
			if tt.want == "" {
				if strings.Contains(log, "non-function object (") {
					t.Fatalf("an ambiguous line was named: %s", log)
				}
				return
			}
			if !strings.Contains(log, tt.want) {
				t.Fatalf("log %q lacks %q", log, tt.want)
			}
			if !noticeHas(h, tt.want) {
				t.Fatalf("notice %v lacks %q", h.NoticeTexts(), tt.want)
			}
		})
	}
}

// The traceback of a runaway recursion is short, its position is in the message, and the
// anonymous function of a command is not called "main chunk".
func TestTracebacksAreBoundedAndLabelledRight(t *testing.T) {
	t.Parallel()
	h := start(t, `
T.command("rec", function() local function f(n) return f(n+1)+1 end f(1) end)
T.command("err", function() error("boom") end)`)
	_ = h.Run("t.rec", nil)
	_ = h.Run("t.err", nil)
	log := strings.Join(h.LogText("t"), "\n")
	if !strings.Contains(log, "t/init.lua:2: lua callstack overflow") {
		t.Errorf("the overflow has no position:\n%s", log)
	}
	if n := strings.Count(log, "in function 'f'"); n != 1 || !strings.Contains(log, "repeats") {
		t.Errorf("%d frames of f, want one with a repeat count:\n%s", n, log)
	}
	if lines := strings.Count(log, "\n"); lines > 12 {
		t.Errorf("%d lines of log for two errors:\n%s", lines, log)
	}
	if strings.Contains(log, "main chunk") {
		t.Errorf("a command's function is called the main chunk:\n%s", log)
	}
	if !strings.Contains(log, "in function <t/init.lua:3>") {
		t.Errorf("the frame of the command has no label:\n%s", log)
	}
}

func TestSyntaxErrorsNameFileAndLine(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"unexpected end", "return x +\nend", "t/t.lua:2:3: syntax error near 'end'"},
		{"stray equals", "local x = = 1", "t/t.lua:1:11: syntax error near '='"},
		{"unterminated string", "x = 'unfinished", "t/t.lua:1: unterminated string near <eof>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugin.CompileSource("t/t.lua", tt.src)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "line:") {
				t.Errorf("the gopher-lua format is still there: %v", err)
			}
		})
	}
}

func TestRuntimeErrorsAreReadable(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // in the first log line
	}{
		// the wording of the reference Lua: the kind of value, then what the name is
		{"a call of a missing field is named", "gvim.notifyy('x')", "attempt to call a non-function object (field 'notifyy')"},
		{"a call of a nil local is named", "local s = nil\ns()", "attempt to call a non-function object (local 's')"},
		{"a method call is named", "local t = {}\nt:go()", "attempt to call a non-function object (method 'go')"},
		{"a missing global is named", "nothing_here()", "attempt to call a non-function object (global 'nothing_here')"},
		{"an upvalue is named", "local up\nlocal function f() up() end\nf()", "attempt to call a non-function object (upvalue 'up')"},
		{"another type is told", "local n = 3\nn()", "attempt to call a non-function object (local 'n')"},
		{"the failed call is told apart on a line with several", "local a, b = print, nil\na(b())", "attempt to call a non-function object"},
		{"a tail call is named", "local t = {}\nreturn t.gone()", "attempt to call a non-function object (field 'gone')"},
		{"an error carries its position", "error('boom')", "t/init.lua:3: boom"},
		{"a stack overflow has a position", "local function f(n) return f(n+1)+1 end f(1)", "t/init.lua:3: lua callstack overflow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := start(t, "local gvim = require 'gvim'\nT.command('go', function()\n"+tt.body+"\nend)")
			_ = h.Run("t.go", nil)
			log := h.LogText("t")
			if len(log) == 0 || !strings.Contains(strings.SplitN(log[0], "\n", 2)[0], tt.want) {
				t.Fatalf("log = %q, want the first line to contain %q", log, tt.want)
			}
			if n := strings.Count(log[0], "\n"); n > 20 {
				t.Errorf("the traceback has %d lines", n)
			}
			if strings.Contains(log[0], `"main chunk"`) {
				t.Errorf("a command function is labelled as the main chunk:\n%s", log[0])
			}
		})
	}
}

func TestTracebackNamesTheCommand(t *testing.T) {
	h := start(t, "local gvim = require 'gvim'\nT.command('go', function()\nerror('boom')\nend)")
	_ = h.Run("t.go", nil)
	if got := strings.Join(h.LogText("t"), "\n"); !strings.Contains(got, "command t.go: ") {
		t.Errorf("log = %q, want the bottom frame to say which command it is", got)
	}
}

// A long traceback shows its ends and says how much it left out.
func TestLongTracebacksAreBounded(t *testing.T) {
	h := start(t, `local gvim = require 'gvim'
local function a(n) if n == 0 then error('deep') end return 1 + b(n - 1) end
function b(n) return 1 + a(n - 1) end
T.command("go", function() b(80) end)`)
	_ = h.Run("t.go", nil)
	log := strings.Join(h.LogText("t"), "\n")
	if n := strings.Count(log, "\n"); n > 25 {
		t.Errorf("traceback has %d lines:\n%s", n, log)
	}
	if !strings.Contains(log, "more frames") {
		t.Errorf("a cut traceback does not say so:\n%s", log)
	}
}
