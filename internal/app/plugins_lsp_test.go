package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui"
)

// lspPlugin reads the language-server API of the plugin runtime against the real editor and
// the real ui: diagnostics_updated, gvim.lsp.diagnostics and gvim.lsp.request.
const lspPlugin = `
local gvim = require "gvim"

gvim.on("diagnostics_updated", function(ev)
  gvim.log.info("DIAG %s e=%d w=%d i=%d h=%d", ev.buf:name(), ev.counts.error, ev.counts.warning, ev.counts.info, ev.counts.hint)
end)

gvim.command.register{ name = "diags", title = "Lsp Diags", run = function()
  local list, err = gvim.lsp.diagnostics()
  if not list then gvim.notify("diags failed: " .. err) return end
  local d = list[1]
  gvim.notify(string.format("%d %s %d:%d-%d:%d %s %s", #list, d.severity, d.range.from.line, d.range.from.col,
    d.range.to.line, d.range.to.col, d.message, d.source or "-"))
end }

gvim.command.register{ name = "hover", title = "Lsp Hover", run = function()
  local res, err = gvim.lsp.request("textDocument/hover", { position = { line = 0, character = 1 } })
  if not res then gvim.notify("hover failed: " .. err) return end
  gvim.notify("hover " .. res.contents.value)
end }

gvim.command.register{ name = "denied", title = "Lsp Denied", run = function()
  local _, err = gvim.lsp.request("workspace/executeCommand", {})
  gvim.notify("denied " .. gvim.err_code(err))
end }
`

// appLSP is the language-server port of the test: it supports Go, answers diagnostics and
// requests from canned data, and ignores the rest. The embedded nil interface makes any
// method the root should not call panic.
type appLSP struct {
	tui.LSPSession
	mu       sync.Mutex
	diags    map[string]tui.DiagnosticsEvent
	requests []string
}

func (f *appLSP) SetRoot(string)            {}
func (f *appLSP) Supports(lang string) bool { return lang == "go" }
func (f *appLSP) Open(_, _, _ string) error { return nil }
func (f *appLSP) Change(_, _ string) error  { return nil }
func (f *appLSP) Close(string) error        { return nil }
func (f *appLSP) Stop() error               { return nil }
func (f *appLSP) Restart() error            { return nil }
func (f *appLSP) Diagnostics(path string) (tui.DiagnosticsEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ev, ok := f.diags[path]
	return ev, ok
}

func (f *appLSP) Request(path, method string, params any) (any, error) {
	f.mu.Lock()
	f.requests = append(f.requests, fmt.Sprintf("%s %s %v", filepath.Base(path), method, params))
	f.mu.Unlock()
	return map[string]any{"contents": map[string]any{"value": "int variable"}}, nil
}

func (f *appLSP) publish(path string, items ...tui.Diagnostic) {
	f.mu.Lock()
	f.diags[path] = tui.DiagnosticsEvent{Path: path, Items: items}
	f.mu.Unlock()
}

func TestPluginLSPEndToEnd(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/lsp.lua", lspPlugin)
	port := &appLSP{diags: map[string]tui.DiagnosticsEvent{}}
	prev := newUI
	newUI = func(d tui.Deps) *tui.Model {
		d.LSP = port
		return prev(d)
	}
	t.Cleanup(func() { newUI = prev })
	s := newSession(t, Options{})
	s.waitStatus("lsp", "loaded")
	logLines := func(prefix string) []string {
		var out []string
		for _, e := range s.plug.Log() {
			if e.Plugin == "lsp" && strings.HasPrefix(e.Msg, prefix) {
				out = append(out, strings.TrimPrefix(e.Msg, prefix))
			}
		}
		return out
	}

	// "héllo := 1": the diagnostic covers "llo" (runes 2 to 5), which is bytes 3 to 6.
	path := s.openFile("a.go", "héllo := 1\n")
	s.send(tea.WindowSizeMsg{Width: 120, Height: 30}) // the root syncs its documents with the servers
	port.publish(path, tui.Diagnostic{Start: buffer.Pos{Line: 0, Col: 2}, End: buffer.Pos{Line: 0, Col: 5}, Severity: 1, Message: "boom", Source: "gopls"})
	s.until("diagnostics_updated", func() bool { return len(logLines("DIAG ")) > 0 })
	if got := logLines("DIAG "); len(got) != 1 || got[0] != "a.go e=1 w=0 i=0 h=0" {
		t.Fatalf("diagnostics_updated = %q, want one event with one error", got)
	}
	s.runCommand("Lsp Diags")
	if got, want := s.statusRow(), "1 error 0:3-0:6 boom gopls"; !strings.Contains(got, want) {
		t.Fatalf("status row = %q, want it to contain %q", got, want)
	}

	// the same diagnostics again raise nothing; clearing them raises zero counts
	s.pump(700 * time.Millisecond)
	if got := logLines("DIAG "); len(got) != 1 {
		t.Fatalf("unchanged diagnostics raised %q", got)
	}
	port.publish(path)
	s.until("the clearing event", func() bool { return len(logLines("DIAG ")) == 2 })
	if got := logLines("DIAG ")[1]; got != "a.go e=0 w=0 i=0 h=0" {
		t.Fatalf("clearing event = %q", got)
	}

	// a request goes to the server that has the buffer open and returns the decoded result
	s.runCommand("Lsp Hover")
	s.until("the hover answer", func() bool { return strings.Contains(s.statusRow(), "hover int variable") })
	port.mu.Lock()
	got := append([]string(nil), port.requests...)
	port.mu.Unlock()
	if want := "a.go textDocument/hover map[position:map[character:1 line:0]]"; len(got) != 1 || got[0] != want {
		t.Fatalf("requests = %q, want [%q]", got, want)
	}

	// a method that is not a read never reaches the server
	s.runCommand("Lsp Denied")
	if row := s.statusRow(); !strings.Contains(row, "denied invalid") {
		t.Fatalf("status row = %q, want the refusal", row)
	}
	port.mu.Lock()
	n := len(port.requests)
	port.mu.Unlock()
	if n != 1 {
		t.Fatalf("a refused request reached the server: %d requests", n)
	}
}
