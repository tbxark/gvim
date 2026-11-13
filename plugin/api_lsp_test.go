package plugin_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// lspHost is a fake host with the language server turned on and one buffer with
// diagnostics.
func lspHost(on bool) *plugintest.FakeHost {
	h := plugintest.NewFakeHost()
	h.AddBuffer("/w/a.go", "package a\nvar x int")
	l := h.LSP()
	l.Enabled = on
	l.Diags[1] = []pluginapi.DiagnosticInfo{
		{Range: pluginapi.Range{From: pluginapi.Pos{Line: 1, Col: 4}, To: pluginapi.Pos{Line: 1, Col: 5}}, Severity: "warning", Message: "unused", Source: "gopls"},
		{Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 0}, To: pluginapi.Pos{Line: 0, Col: 7}}, Severity: "error", Message: "bad"},
	}
	return h
}

func TestLSPDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		on   bool
		body string
		want string
	}{
		{"current buffer by default", true,
			`local d = gvim.lsp.diagnostics() T.spy(#d, show(d[1]), show(d[2]))`,
			`2 {message=unused,range={from={col=4,line=1},to={col=5,line=1}},severity=warning,source=gopls} {message=bad,range={from={col=0,line=0},to={col=7,line=0}},severity=error}`},
		{"explicit buffer", true,
			`T.spy(#gvim.lsp.diagnostics(gvim.buf.current()))`, "2"},
		{"no server is unsupported", false,
			`T.spy(gvim.lsp.diagnostics())`, "nil unsupported: no language server is configured"},
		{"has reports the server", true, `T.spy(gvim.has("lsp"))`, "true"},
		{"has without a server", false, `T.spy(gvim.has("lsp"))`, "false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := cmd(t, bufShow, tc.body, plugintest.Options{Host: lspHost(tc.on)})
			if got := spyText(h); got != tc.want {
				t.Fatalf("spy = %q\nwant  %q (log %v)", got, tc.want, h.LogText(""))
			}
		})
	}
}

func TestLSPDiagnosticsClosedBuffer(t *testing.T) {
	host := lspHost(true)
	h := cmd(t, bufShow, `
local b = gvim.buf.current()
b:close()
T.spy(gvim.lsp.diagnostics(b))`, plugintest.Options{Host: host})
	if got := spyText(h); !strings.HasPrefix(got, "nil closed:") {
		t.Fatalf("spy = %q, want a closed error (log %v)", got, h.LogText(""))
	}
}

func TestLSPRequest(t *testing.T) {
	host := lspHost(true)
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		switch c.Method {
		case "textDocument/hover":
			return map[string]any{"contents": map[string]any{"value": "doc"}, "n": 3.0, "list": []any{"a", "b"}}, ""
		case "textDocument/definition":
			return nil, ""
		}
		return nil, "timeout: gopls did not answer"
	}
	h := cmd(t, bufShow, `
local r, err = gvim.lsp.request("textDocument/hover", {textDocument = {uri = "file:///w/a.go"}, position = {line = 1, character = 4}})
T.spy("hover", show(r), tostring(err))
local r2, err2 = gvim.lsp.request("textDocument/definition", {}, {buf = gvim.buf.current()})
T.spy("def", tostring(r2), tostring(err2))
local r3, err3 = gvim.lsp.request("textDocument/references")
T.spy("refs", tostring(r3), err3, gvim.err_code(err3))`, plugintest.Options{Host: host})
	if got := len(host.LSP().Calls); got != 1 {
		t.Fatalf("calls after the first await = %d, want 1 (the task is parked on the first)", got)
	}
	for i := 0; i < 3; i++ {
		h.CompleteLSP()
	}
	want := "hover {contents={value=doc},list={1=a,2=b},n=3} nil" +
		"|def nil nil" +
		"|refs nil timeout: gopls did not answer timeout"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q\nwant  %q (log %v)", got, want, h.LogText(""))
	}
	calls := host.LSP().Calls
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	wantParams := map[string]any{
		"textDocument": map[string]any{"uri": "file:///w/a.go"},
		"position":     map[string]any{"line": 1.0, "character": 4.0},
	}
	if fmt.Sprint(calls[0].Params) != fmt.Sprint(wantParams) || calls[0].Buffer != 1 || calls[0].Method != "textDocument/hover" {
		t.Fatalf("first call = %+v, want buffer 1 hover with %v", calls[0], wantParams)
	}
	if calls[2].Params != nil {
		t.Fatalf("a request without params sent %v, want nil", calls[2].Params)
	}
}

func TestLSPRequestRefusals(t *testing.T) {
	tests := []struct {
		name string
		on   bool
		body string
		want string // prefix of the spy text
	}{
		{"initialize", true, `T.spy(gvim.lsp.request("initialize", {}))`, "nil invalid: request: initialize is not allowed"},
		{"executeCommand", true, `T.spy(gvim.lsp.request("workspace/executeCommand", {command = "x"}))`, "nil invalid: request: workspace/executeCommand is not allowed"},
		{"notification", true, `T.spy(gvim.lsp.request("textDocument/didChange", {}))`, "nil invalid: request: textDocument/didChange is not allowed"},
		{"cancel", true, `T.spy(gvim.lsp.request("$/cancelRequest", {}))`, "nil invalid: request: $/cancelRequest is not allowed"},
		{"notebook didOpen", true, `T.spy(gvim.lsp.request("notebookDocument/didOpen", {}))`, "nil invalid: request: notebookDocument/didOpen is not allowed"},
		{"notebook didChange", true, `T.spy(gvim.lsp.request("notebookDocument/didChange", {}))`, "nil invalid: request: notebookDocument/didChange is not allowed"},
		{"notebook didClose", true, `T.spy(gvim.lsp.request("notebookDocument/didClose", {}))`, "nil invalid: request: notebookDocument/didClose is not allowed"},
		{"willSave", true, `T.spy(gvim.lsp.request("textDocument/willSave", {}))`, "nil invalid: request: textDocument/willSave is not allowed"},
		{"willSaveWaitUntil", true, `T.spy(gvim.lsp.request("textDocument/willSaveWaitUntil", {}))`, "nil invalid: request: textDocument/willSaveWaitUntil is not allowed"},
		{"workspace edits", true, `T.spy(gvim.lsp.request("workspace/willRenameFiles", {}))`, "nil invalid: request: workspace/willRenameFiles is not allowed"},
		{"unknown method", true, `T.spy(gvim.lsp.request("custom/doAnything", {}))`, "nil invalid: request: custom/doAnything is not allowed"},
		{"bad characters", true, `T.spy(gvim.lsp.request("a b", {}))`, "nil invalid: request: bad method name"},
		{"empty", true, `T.spy(gvim.lsp.request("", {}))`, "nil invalid: request: the method name"},
		{"params must be json", true, `T.spy(gvim.lsp.request("textDocument/hover", {f = function() end}))`, "nil invalid: request: params"},
		{"no server", false, `T.spy(gvim.lsp.request("textDocument/hover", {}))`, "nil unsupported: no language server"},
		{"closed buffer", true, `local b = gvim.buf.current() b:close() T.spy(gvim.lsp.request("textDocument/hover", {}, {buf = b}))`, "nil closed:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host := lspHost(tc.on)
			h := cmd(t, bufShow, tc.body, plugintest.Options{Host: host})
			if got := spyText(h); !strings.HasPrefix(got, tc.want) {
				t.Fatalf("spy = %q, want prefix %q (log %v)", got, tc.want, h.LogText(""))
			}
			if n := len(host.LSP().Calls); n != 0 {
				t.Fatalf("a refused request reached the host %d times", n)
			}
		})
	}
}

func TestLSPRequestBadArguments(t *testing.T) {
	tests := []struct{ name, body string }{
		{"method not a string", `gvim.lsp.request({}, {})`},
		{"params not a table", `gvim.lsp.request("textDocument/hover", 5)`},
		{"opts not a table", `gvim.lsp.request("textDocument/hover", {}, 5)`},
		{"buf not a buffer", `gvim.lsp.request("textDocument/hover", {}, {buf = 5})`},
		{"window as buf", `gvim.lsp.request("textDocument/hover", {}, {buf = gvim.win.current()})`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host := lspHost(true)
			h := cmd(t, bufShow, `local ok, e = pcall(function() `+tc.body+` end) T.spy(ok, tostring(e))`, plugintest.Options{Host: host})
			got := spyText(h)
			if !strings.HasPrefix(got, "false ") || !strings.Contains(got, "bad argument") {
				t.Fatalf("spy = %q, want a bad argument error (log %v)", got, h.LogText(""))
			}
		})
	}
}

func TestLSPRequestUnloadCancelsTheWait(t *testing.T) {
	host := lspHost(true)
	before := host.Resources()
	h := cmd(t, bufShow, `gvim.lsp.request("textDocument/hover", {})`, plugintest.Options{Host: host})
	if n := len(host.LSP().Calls); n != 1 {
		t.Fatalf("host calls = %d, want 1", n)
	}
	if got := host.Resources(); len(got) <= len(before) {
		t.Fatalf("resources while parked = %v, before %v: the pending request should show", got, before)
	}
	h.Unload("t")
	if got := len(host.LSP().Cancelled); got != 1 {
		t.Fatalf("CancelWait calls = %d, want 1", got)
	}
	if got, want := fmt.Sprint(host.Resources()), fmt.Sprint(before); got != want {
		t.Fatalf("resources after unload = %v, want %v", got, want)
	}
}

// TestLSPRequestAllowedMethods lists the methods of the read allowlist: each one reaches
// the host.
func TestLSPRequestAllowedMethods(t *testing.T) {
	methods := []string{
		"textDocument/hover", "textDocument/definition", "textDocument/declaration",
		"textDocument/typeDefinition", "textDocument/implementation", "textDocument/references",
		"textDocument/documentSymbol", "textDocument/documentHighlight", "textDocument/codeAction",
		"textDocument/completion", "textDocument/signatureHelp", "textDocument/formatting",
		"textDocument/rangeFormatting", "textDocument/rename", "textDocument/prepareRename",
		"workspace/symbol", "completionItem/resolve", "codeAction/resolve",
		"callHierarchy/incomingCalls", "typeHierarchy/subtypes",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			host := lspHost(true)
			h := cmd(t, bufShow, fmt.Sprintf(`gvim.lsp.request(%q, {})`, m), plugintest.Options{Host: host})
			if n := len(host.LSP().Calls); n != 1 || host.LSP().Calls[0].Method != m {
				t.Fatalf("calls = %+v, want one %s (log %v)", host.LSP().Calls, m, h.LogText(""))
			}
		})
	}
}

// TestLSPRequestParamsKeepEmptyArrays: json.array({}) in the params reaches the host as an
// empty array and a plain empty table as an object.
func TestLSPRequestParamsKeepEmptyArrays(t *testing.T) {
	host := lspHost(true)
	cmd(t, bufShow, `gvim.lsp.request("textDocument/codeAction", {
  context = {diagnostics = json.array({}), only = json.array({"quickfix"})}, range = {}, list = json.array()})`,
		plugintest.Options{Host: host})
	calls := host.LSP().Calls
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	want := map[string]any{
		"context": map[string]any{"diagnostics": []any{}, "only": []any{"quickfix"}},
		"range":   map[string]any{},
		"list":    []any{},
	}
	if !reflect.DeepEqual(calls[0].Params, want) {
		t.Fatalf("params = %#v, want %#v", calls[0].Params, want)
	}
}

// TestLSPRequestResultKeepsNullsAndEmptyArrays: a null inside a result is json.null, so an
// array keeps its length, and an empty array stays an array when encoded again.
func TestLSPRequestResultKeepsNullsAndEmptyArrays(t *testing.T) {
	host := lspHost(true)
	host.LSP().Reply = func(plugintest.LSPCall) (any, string) {
		return map[string]any{"list": []any{1.0, nil, 3.0}, "none": []any{}, "o": map[string]any{"x": nil}}, ""
	}
	h := cmd(t, bufShow, `
local r = gvim.lsp.request("textDocument/hover", {})
T.spy(#r.list, r.list[2] == json.null, r.list[3], json.encode(r.none), r.o.x == json.null)`, plugintest.Options{Host: host})
	h.CompleteLSP()
	if got, want := spyText(h), "3 true 3 [] true"; got != want {
		t.Fatalf("spy = %q, want %q (log %v)", got, want, h.LogText(""))
	}
}
