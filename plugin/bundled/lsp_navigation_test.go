package bundled_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The lsp_navigation port (Fresh's Go to LSP Symbol plus the gvim-addition location lists)
// over the fake host and a scripted LSP server. The Finder prompt and its preview highlight
// are driven through the fake picker completions.

const lspNavPlugin = "lsp_navigation"

func lspNavFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", lspNavPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+lspNavPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[lspNavPlugin+"/"+name] = string(data)
	}
	return files
}

// lspNavHost is a fake host with one Go file open and the language server on.
func lspNavHost(t *testing.T) (*plugintest.FakeHost, pluginapi.BufferID) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/work"
	buf := host.AddBuffer("/work/a.go", "package a\n\nfunc main() {\n\thelper()\n}\n\nfunc helper() {}\n")
	host.Buffers[0].Info.Language = "go"
	host.LSP().Enabled = true
	return host, buf
}

func startLSPNav(t *testing.T, host *plugintest.FakeHost) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, lspNavFiles(t), plugintest.Options{Host: host, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func navLastNotice(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

// navRun runs a command and answers the LSP request it parks on.
func navRun(t *testing.T, h *plugintest.Harness, command string) {
	t.Helper()
	if err := h.Run(lspNavPlugin+"."+command, nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteLSP()
}

func TestLSPNavigationManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/"+lspNavPlugin+"/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name           string `json:"name"`
		Description    string `json:"description"`
		API            int    `json:"api"`
		DefaultEnabled *bool  `json:"default_enabled"`
		Version        string `json:"version"`
	}
	if err := jsonUnmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != lspNavPlugin || m.API != 2 || m.Version == "" || strings.TrimSpace(m.Description) == "" {
		t.Errorf("manifest = %+v", m)
	}
	if m.DefaultEnabled == nil || *m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want false (idle-heap gate)", m.DefaultEnabled)
	}
	host, _ := lspNavHost(t)
	h := startLSPNav(t, host)
	if info, ok := h.Plugin(lspNavPlugin); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, h.LogText(""))
	}
	titles := map[string]string{}
	for _, c := range h.Host.Commands[lspNavPlugin] {
		if !c.Hidden {
			titles[c.ID] = c.Title
		}
	}
	want := map[string]string{
		lspNavPlugin + ".goto_lsp_symbol":       "Go to LSP Symbol",
		lspNavPlugin + ".show_definitions":      "LSP: Show Definitions",
		lspNavPlugin + ".show_implementations":  "LSP: Show Implementations",
		lspNavPlugin + ".show_type_definitions": "LSP: Show Type Definitions",
	}
	for id, title := range want {
		if titles[id] != title {
			t.Errorf("command %s = %q, want %q", id, titles[id], title)
		}
	}
	if bindings := h.Host.Bindings[lspNavPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none (Fresh binds none)", bindings)
	}
}

func TestLSPNavigationSymbols(t *testing.T) {
	t.Parallel()
	host, buf := lspNavHost(t)
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		if c.Method == "textDocument/documentSymbol" {
			return []any{
				map[string]any{"name": "main", "kind": 12.0, "location": map[string]any{"uri": "file:///work/a.go", "range": map[string]any{"start": map[string]any{"line": 2.0, "character": 5.0}, "end": map[string]any{"line": 5.0, "character": 0.0}}}},
				map[string]any{"name": "helper", "kind": 12.0, "location": map[string]any{"uri": "file:///work/a.go", "range": map[string]any{"start": map[string]any{"line": 6.0, "character": 5.0}, "end": map[string]any{"line": 6.0, "character": 15.0}}}},
			}, ""
		}
		return nil, "unexpected " + c.Method
	}
	h := startLSPNav(t, host)
	navRun(t, h, "goto_lsp_symbol")
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no symbol finder opened (log %v)", h.LogText(""))
	}
	if o.Picker.Title != "Go to symbol: " || len(o.Picker.Items) != 2 {
		t.Fatalf("finder = %+v", o.Picker)
	}
	if !slices.Contains([]string{o.Picker.Items[0].Text, o.Picker.Items[1].Text}, "[fn] main") {
		t.Errorf("rows = %+v", o.Picker.Items)
	}
	// Moving the selection moves the cursor over the symbol and highlights it.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{1}, More: true})
	cursors, _ := host.Cursors(buf)
	if len(cursors) == 0 || cursors[0].Pos.Line != 6 {
		t.Errorf("preview cursor = %+v, want line 6", cursors)
	}
	if items := host.Decor().Items(lspNavPlugin, "lsp_symbol_nav", buf); len(items) != 1 || items[0].Kind != pluginapi.DecorHighlight {
		t.Errorf("preview highlight = %+v", items)
	}
	// The query filters in Lua and re-sorts by line.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"help"}, More: true})
	h.Advance(70 * time.Millisecond)
	if last := o.Updates[len(o.Updates)-1]; last.Items == nil || len(*last.Items) != 1 || (*last.Items)[0].Text != "[fn] helper" {
		t.Fatalf("query rows = %+v", last.Items)
	}
	// Enter commits: no cursor restore, the highlight is cleared.
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	if items := host.Decor().Items(lspNavPlugin, "lsp_symbol_nav", buf); len(items) != 0 {
		t.Errorf("highlight after select = %+v", items)
	}
	if cursors, _ := host.Cursors(buf); len(cursors) == 0 || cursors[0].Pos.Line != 6 {
		t.Errorf("cursor after select = %+v, want the jumped line", cursors)
	}
}

func TestLSPNavigationCancelRestoresCursor(t *testing.T) {
	t.Parallel()
	host, buf := lspNavHost(t)
	if err := host.SetCursor(buf, pluginapi.Pos{Line: 0, Col: 0}, false, false, false); err != nil {
		t.Fatal(err)
	}
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		return []any{
			map[string]any{"name": "helper", "kind": 12.0, "location": map[string]any{"uri": "file:///work/a.go", "range": map[string]any{"start": map[string]any{"line": 6.0, "character": 5.0}, "end": map[string]any{"line": 6.0, "character": 15.0}}}},
		}, ""
	}
	h := startLSPNav(t, host)
	navRun(t, h, "goto_lsp_symbol")
	o := h.Host.UI().Last("picker")
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{0}, More: true})
	if cursors, _ := host.Cursors(buf); cursors[0].Pos.Line != 6 {
		t.Fatalf("preview cursor = %+v", cursors)
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{nil}})
	if cursors, _ := host.Cursors(buf); cursors[0].Pos.Line != 0 {
		t.Errorf("cursor after cancel = %+v, want 0", cursors)
	}
	if items := host.Decor().Items(lspNavPlugin, "lsp_symbol_nav", buf); len(items) != 0 {
		t.Errorf("highlight after cancel = %+v", items)
	}
}

func TestLSPNavigationGuards(t *testing.T) {
	t.Parallel()
	t.Run("no language", func(t *testing.T) {
		t.Parallel()
		host, _ := lspNavHost(t)
		host.Buffers[0].Info.Language = ""
		h := startLSPNav(t, host)
		if err := h.Run(lspNavPlugin+".goto_lsp_symbol", nil); err != nil {
			t.Fatal(err)
		}
		if got := navLastNotice(h); !strings.Contains(got, "no language") {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("no symbols", func(t *testing.T) {
		t.Parallel()
		host, _ := lspNavHost(t)
		host.LSP().Reply = func(c plugintest.LSPCall) (any, string) { return []any{}, "" }
		h := startLSPNav(t, host)
		navRun(t, h, "goto_lsp_symbol")
		if got := navLastNotice(h); got != "No symbols in this document" {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("server error", func(t *testing.T) {
		t.Parallel()
		host, _ := lspNavHost(t)
		host.LSP().Reply = func(c plugintest.LSPCall) (any, string) { return nil, "io: server crashed" }
		h := startLSPNav(t, host)
		navRun(t, h, "goto_lsp_symbol")
		if got := navLastNotice(h); !strings.Contains(got, "server crashed") {
			t.Errorf("notice = %q", got)
		}
	})
}

func TestLSPNavigationDefinitionsList(t *testing.T) {
	t.Parallel()
	host, _ := lspNavHost(t)
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		if c.Method == "textDocument/definition" {
			return []any{
				map[string]any{"uri": "file:///work/a.go", "range": map[string]any{"start": map[string]any{"line": 2.0, "character": 5.0}, "end": map[string]any{"line": 2.0, "character": 9.0}}},
				map[string]any{"uri": "file:///work/b.go", "range": map[string]any{"start": map[string]any{"line": 1.0, "character": 0.0}, "end": map[string]any{"line": 1.0, "character": 4.0}}},
			}, ""
		}
		return nil, ""
	}
	host.SetFile("/work/b.go", "one\ntwo\nthree\n")
	h := startLSPNav(t, host)
	h.OS.SetFile("/work/b.go", "one\ntwo\nthree\n")
	navRun(t, h, "show_definitions")
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no definition list opened (log %v)", h.LogText(""))
	}
	if o.Picker.Title != "definitions (2)" {
		t.Errorf("title = %q", o.Picker.Title)
	}
	if len(o.Picker.Items) != 2 || !strings.Contains(o.Picker.Items[1].Text, "b.go:2") {
		t.Errorf("rows = %+v", o.Picker.Items)
	}
	// Moving previews the location in a preview tab; Enter commits it (non-preview open).
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{1}, More: true})
	h.CompleteBuffers()
	if !slices.ContainsFunc(host.Calls(), func(c plugintest.Call) bool {
		if c.Method != "Open" {
			return false
		}
		req, ok := c.Args[1].(pluginapi.OpenRequest)
		return ok && req.Path == "/work/b.go" && req.Preview
	}) {
		t.Errorf("no preview open for the definition: %v", host.Calls())
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{1}})
	h.CompleteBuffers()
	if got := navLastNotice(h); !strings.Contains(got, "Jumped to") {
		t.Errorf("jump notice = %q", got)
	}
}

func TestLSPNavigationPreselectsEnclosingSymbol(t *testing.T) {
	t.Parallel()
	host, buf := lspNavHost(t)
	if err := host.SetCursor(buf, pluginapi.Pos{Line: 3, Col: 1}, false, false, false); err != nil {
		t.Fatal(err)
	}
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		if c.Method == "textDocument/documentSymbol" {
			return []any{
				map[string]any{"name": "outer", "kind": 12.0, "location": map[string]any{"uri": "file:///work/a.go",
					"range": map[string]any{"start": map[string]any{"line": 0.0, "character": 0.0}, "end": map[string]any{"line": 6.0, "character": 0.0}}}},
				map[string]any{"name": "inner", "kind": 12.0, "location": map[string]any{"uri": "file:///work/a.go",
					"range": map[string]any{"start": map[string]any{"line": 2.0, "character": 5.0}, "end": map[string]any{"line": 4.0, "character": 0.0}}}},
			}, ""
		}
		return nil, ""
	}
	h := startLSPNav(t, host)
	navRun(t, h, "goto_lsp_symbol")
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no symbol finder opened (log %v)", h.LogText(""))
	}
	if len(o.Picker.Items) != 2 || !strings.Contains(o.Picker.Items[0].Text, "[fn] inner") {
		t.Fatalf("preselected row is not first: %+v", o.Picker.Items)
	}
	if cursors, _ := host.Cursors(buf); len(cursors) == 0 || cursors[0].Pos.Line != 2 {
		t.Fatalf("cursor = %+v, want the enclosing symbol line 2", cursors)
	}
	if items := host.Decor().Items(lspNavPlugin, "lsp_symbol_nav", buf); len(items) != 1 || items[0].Kind != pluginapi.DecorHighlight {
		t.Fatalf("preselected highlight = %+v", items)
	}
}
