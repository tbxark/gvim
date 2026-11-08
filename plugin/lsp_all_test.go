package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// lspPlugin reports the all-files diagnostics as a notice: "path=count" rows joined by a
// comma, plus the lsp events it subscribed to.
const lspPlugin = `
local gvim = require 'gvim'
gvim.command.register{ name = "report", run = function()
  local files = gvim.lsp.all_diagnostics()
  if files == nil then gvim.notify("nil") return end
  local parts = {}
  for _, f in ipairs(files) do parts[#parts + 1] = f.path .. "=" .. #f.diagnostics end
  gvim.notify(table.concat(parts, ","))
end }
gvim.on("lsp_status_clicked", function() gvim.notify("clicked") end)
gvim.on("lsp_server_error", function(ev) gvim.notify("error:" .. ev.error) end)
`

func TestLSPAllDiagnosticsAndEvents(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"l.lua": lspPlugin}, plugintest.Options{})
	h.Start()
	l := h.Host.LSP()
	l.Enabled = true
	h.Host.AddBuffer("/w/a.go", "package a")
	h.Host.AddBuffer("/w/b.go", "package b")
	l.Diags[1] = []pluginapi.DiagnosticInfo{{Severity: "error", Message: "bad"}, {Severity: "warning", Message: "meh"}}
	l.Diags[2] = []pluginapi.DiagnosticInfo{{Severity: "info", Message: "note"}}
	if err := h.Run("l.report", nil); err != nil {
		t.Fatal(err)
	}
	got := h.NoticeTexts()
	if len(got) != 1 || got[0] != "/w/a.go=2,/w/b.go=1" {
		t.Fatalf("notices = %q, want the per-path counts", got)
	}
	// The events reach the plugin handlers.
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "boom"}})
	all := h.NoticeTexts()
	if len(all) != 3 || all[1] != "clicked" || all[2] != "error:boom" {
		t.Fatalf("notices = %q", all)
	}
}
