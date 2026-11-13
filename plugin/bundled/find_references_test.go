package bundled_test

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The find_references port over the fake host + a scripted LSP server: the references list,
// snippets, preview tabs, jump, the hidden close command and the empty/error states.

const findRefPlugin = "find_references"

func findRefFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", findRefPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+findRefPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[findRefPlugin+"/"+name] = string(data)
	}
	return files
}

func findRefHost(t *testing.T) (*plugintest.FakeHost, pluginapi.BufferID) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/work"
	buf := host.AddBuffer("/work/a.go", "package a\n\nfunc main() {\n\thelper()\n}\n")
	host.Buffers[0].Info.Language = "go"
	host.LSP().Enabled = true
	return host, buf
}

func startFindRef(t *testing.T, host *plugintest.FakeHost) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, findRefFiles(t), plugintest.Options{Host: host, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func findRefNotice(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

func refLocation(path string, line, character float64) map[string]any {
	return map[string]any{
		"uri": "file://" + path,
		"range": map[string]any{
			"start": map[string]any{"line": line, "character": character},
			"end":   map[string]any{"line": line, "character": character + 4},
		},
	}
}

func TestFindReferencesManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/"+findRefPlugin+"/plugin.json")
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
	if m.Name != findRefPlugin || m.API != 2 || m.Version == "" || strings.TrimSpace(m.Description) == "" {
		t.Errorf("manifest = %+v", m)
	}
	if m.DefaultEnabled == nil || *m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want false (idle-heap gate)", m.DefaultEnabled)
	}
	host, _ := findRefHost(t)
	h := startFindRef(t, host)
	if info, ok := h.Plugin(findRefPlugin); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, h.LogText(""))
	}
	var visible []string
	for _, c := range h.Host.Commands[findRefPlugin] {
		if c.Hidden {
			continue
		}
		visible = append(visible, c.ID+"="+c.Title)
	}
	if len(visible) != 1 || visible[0] != findRefPlugin+".show_references=Show References Panel" {
		t.Errorf("palette rows = %v", visible)
	}
	hidden := false
	for _, c := range h.Host.Commands[findRefPlugin] {
		if c.Hidden && c.ID == findRefPlugin+".hide_references" {
			hidden = true
		}
	}
	if !hidden {
		t.Error("the hide_references close command is not registered hidden")
	}
	if bindings := h.Host.Bindings[findRefPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestFindReferencesListAndJump(t *testing.T) {
	t.Parallel()
	host, buf := findRefHost(t)
	if err := host.SetCursor(buf, pluginapi.Pos{Line: 2, Col: 5}, false, false, false); err != nil {
		t.Fatal(err)
	}
	host.SetFile("/work/b.go", "one\ntwo\nthree\n")
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		if c.Method == "textDocument/references" {
			return []any{refLocation("/work/a.go", 2, 5), refLocation("/work/b.go", 1, 0)}, ""
		}
		return nil, ""
	}
	h := startFindRef(t, host)
	h.OS.SetFile("/work/b.go", "one\ntwo\nthree\n")
	if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteLSP()
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no references list opened (log %v)", h.LogText(""))
	}
	if o.Picker.Title != "References to main (2)" {
		t.Errorf("title = %q", o.Picker.Title)
	}
	if len(o.Picker.Items) != 2 || o.Picker.Items[0].Text != "a.go:3" {
		t.Fatalf("rows = %+v", o.Picker.Items)
	}
	if !strings.Contains(o.Picker.Items[0].Detail, "func main") {
		t.Errorf("snippet = %q, want the source line", o.Picker.Items[0].Detail)
	}
	if got := o.Picker.Footer; got != "[Up/Down] navigate  [RET] jump  [q/Esc] close" {
		t.Errorf("footer = %q", got)
	}
	// Moving opens the location in a preview tab.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{1}, More: true})
	h.CompleteBuffers()
	previewed := false
	for _, c := range host.Calls() {
		if c.Method != "Open" {
			continue
		}
		if req, ok := c.Args[1].(pluginapi.OpenRequest); ok && req.Path == "/work/b.go" && req.Preview {
			previewed = true
		}
	}
	if !previewed {
		t.Errorf("no preview open: %v", host.Calls())
	}
	// Enter commits the jump.
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{1}})
	h.CompleteBuffers()
	if got := findRefNotice(h); !strings.Contains(got, "Jumped to b.go:2") {
		t.Errorf("jump notice = %q", got)
	}
}

func TestFindReferencesHideCommand(t *testing.T) {
	t.Parallel()
	host, buf := findRefHost(t)
	if err := host.SetCursor(buf, pluginapi.Pos{Line: 2, Col: 5}, false, false, false); err != nil {
		t.Fatal(err)
	}
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		return []any{refLocation("/work/a.go", 2, 5)}, ""
	}
	h := startFindRef(t, host)
	if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteLSP()
	if h.Host.UI().Last("picker") == nil {
		t.Fatal("no references list opened")
	}
	if err := h.Run(findRefPlugin+".hide_references", nil); err != nil {
		t.Fatal(err)
	}
	if got := h.Host.CallCount("ClosePicker"); got == 0 {
		t.Errorf("hide_references did not close the picker (calls %v)", h.Host.Calls())
	}
	if got := findRefNotice(h); got != "References panel closed" {
		t.Errorf("close notice = %q", got)
	}
}

// Hiding the list discards the preview tab the last selection opened, like Esc does.
func TestFindReferencesHideClosesThePreviewTab(t *testing.T) {
	t.Parallel()
	host, buf := findRefHost(t)
	if err := host.SetCursor(buf, pluginapi.Pos{Line: 2, Col: 5}, false, false, false); err != nil {
		t.Fatal(err)
	}
	host.SetFile("/work/b.go", "one\ntwo\nthree\n")
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) {
		return []any{refLocation("/work/a.go", 2, 5), refLocation("/work/b.go", 1, 0)}, ""
	}
	h := startFindRef(t, host)
	h.OS.SetFile("/work/b.go", "one\ntwo\nthree\n")
	if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteLSP()
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no references list opened")
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{1}, More: true})
	h.CompleteBuffers()
	b := host.FindBuffer("/work/b.go")
	if b == 0 {
		t.Fatal("the preview did not open b.go")
	}
	for _, fb := range host.Buffers {
		if fb.Info.ID == b {
			fb.Info.Preview = true
		}
	}
	if err := h.Run(findRefPlugin+".hide_references", nil); err != nil {
		t.Fatal(err)
	}
	if host.FindBuffer("/work/b.go") != 0 {
		t.Errorf("the preview tab of b.go is still open after hide (calls %v)", host.Calls())
	}
}

func TestFindReferencesEmptyAndErrors(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		host, buf := findRefHost(t)
		if err := host.SetCursor(buf, pluginapi.Pos{Line: 2, Col: 5}, false, false, false); err != nil {
			t.Fatal(err)
		}
		host.LSP().Reply = func(c plugintest.LSPCall) (any, string) { return []any{}, "" }
		h := startFindRef(t, host)
		if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
			t.Fatal(err)
		}
		h.CompleteLSP()
		if got := findRefNotice(h); got != "No references found for 'main'" {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("server error", func(t *testing.T) {
		t.Parallel()
		host, buf := findRefHost(t)
		if err := host.SetCursor(buf, pluginapi.Pos{Line: 2, Col: 5}, false, false, false); err != nil {
			t.Fatal(err)
		}
		host.LSP().Reply = func(c plugintest.LSPCall) (any, string) { return nil, "io: server gone" }
		h := startFindRef(t, host)
		if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
			t.Fatal(err)
		}
		h.CompleteLSP()
		if got := findRefNotice(h); !strings.Contains(got, "server gone") {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("no word under the cursor", func(t *testing.T) {
		t.Parallel()
		host, buf := findRefHost(t)
		if err := host.SetCursor(buf, pluginapi.Pos{Line: 1, Col: 0}, false, false, false); err != nil {
			t.Fatal(err)
		}
		h := startFindRef(t, host)
		if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
			t.Fatal(err)
		}
		if got := findRefNotice(h); got != "No symbol under the cursor" {
			t.Errorf("notice = %q", got)
		}
	})
}

func TestFindReferencesRowCap(t *testing.T) {
	t.Parallel()
	host, _ := findRefHost(t)
	locs := make([]any, 0, 150)
	for i := 0; i < 150; i++ {
		locs = append(locs, refLocation("/work/a.go", float64(i%5), 0))
	}
	host.LSP().Reply = func(c plugintest.LSPCall) (any, string) { return locs, "" }
	h := startFindRef(t, host)
	if err := h.Run(findRefPlugin+".show_references", nil); err != nil {
		t.Fatal(err)
	}
	h.CompleteLSP()
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no references list opened")
	}
	if len(o.Picker.Items) != 100 {
		t.Errorf("rows = %d, want the 100-row cap", len(o.Picker.Items))
	}
	if !strings.Contains(o.Picker.Title, "limited to 100") {
		t.Errorf("title = %q, want the limited note", o.Picker.Title)
	}
}
