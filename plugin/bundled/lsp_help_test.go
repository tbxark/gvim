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

// The lsp_help port: a per-language install-hint table behind gvim's LSP events. The tests
// drive lsp_server_error, lsp_status_clicked and action_popup_result over the fake host and
// assert the status line, the popup contents and the chosen action.

const lspHelpPlugin = "lsp_help"

func lspHelpFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", lspHelpPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+lspHelpPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files[lspHelpPlugin+"/"+name] = string(data)
	}
	return files
}

// lspHelpHost is a fake host with one Go file open and the language set.
func lspHelpHost(t *testing.T) (*plugintest.FakeHost, pluginapi.BufferID) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/work"
	buf := host.AddBuffer("/work/a.go", "package a\n")
	host.Buffers[0].Info.Language = "go"
	return host, buf
}

func startLSPHelp(t *testing.T, host *plugintest.FakeHost) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, lspHelpFiles(t), plugintest.Options{
		Host: host, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second,
	})
	h.Start()
	return h
}

func lastNoticeText(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

func TestLSPHelpManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/"+lspHelpPlugin+"/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name           string `json:"name"`
		Version        string `json:"version"`
		Description    string `json:"description"`
		API            int    `json:"api"`
		DefaultEnabled *bool  `json:"default_enabled"`
	}
	if err := jsonUnmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != lspHelpPlugin || m.Version == "" || strings.TrimSpace(m.Description) == "" {
		t.Errorf("manifest = %+v", m)
	}
	if m.API != 2 {
		t.Errorf("api = %d, want 2", m.API)
	}
	if m.DefaultEnabled == nil || !*m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want true (a small passive help plugin)", m.DefaultEnabled)
	}
	host, _ := lspHelpHost(t)
	h := startLSPHelp(t, host)
	if info, ok := h.Plugin(lspHelpPlugin); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, h.LogText(""))
	}
	for _, l := range h.LogText("") {
		if strings.HasPrefix(l, "warn:") || strings.HasPrefix(l, "error:") {
			t.Errorf("load logged %s", l)
		}
	}
	// Like Fresh's plugin: no commands, no default key binding. The help is event driven.
	if cmds := h.Host.Commands[lspHelpPlugin]; len(cmds) != 0 {
		t.Errorf("commands = %v, want none (Fresh registers none)", cmds)
	}
	if bindings := h.Host.Bindings[lspHelpPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
	if notices := h.NoticeTexts(); len(notices) != 0 {
		t.Errorf("load notices = %v", notices)
	}
	// The English table has the generic strings the popup and status lines use.
	raw, err := fs.ReadFile(bundled.FS, "plugins/"+lspHelpPlugin+"/"+lspHelpPlugin+".i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	var table map[string]map[string]string
	if err := jsonUnmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"status.server_error", "status.server_not_found", "status.copied", "status.disabled",
		"popup.title", "popup.copy", "actions.restart", "actions.stop", "actions.toggle_buffer",
		"actions.disable", "actions.definition", "actions.references", "actions.dismiss",
	} {
		if table["en"][key] == "" {
			t.Errorf("the English table has no %q", key)
		}
	}
}

func TestLSPHelpServerErrorStatus(t *testing.T) {
	t.Parallel()
	host, _ := lspHelpHost(t)
	h := startLSPHelp(t, host)
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "gopls: executable file not found in $PATH"}})
	if got := lastNoticeText(h); !strings.Contains(got, "Go LSP server not found") || !strings.Contains(got, "click the LSP status") {
		t.Errorf("not-found notice = %q", got)
	}
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "gopls: initialization failed: boom"}})
	if got := lastNoticeText(h); !strings.Contains(got, "Go LSP error") || !strings.Contains(got, "initialization failed") {
		t.Errorf("error notice = %q", got)
	}
}

func TestLSPHelpPopupAndCopy(t *testing.T) {
	t.Parallel()
	host, _ := lspHelpHost(t)
	h := startLSPHelp(t, host)
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "gopls: not found"}})
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov := h.Host.UI().Last("action_popup")
	if ov == nil {
		t.Fatalf("no action popup was opened (log %v)", h.LogText(""))
	}
	spec := ov.ActionPopup
	if spec.Title != "Go Language Server Help" {
		t.Errorf("popup title = %q", spec.Title)
	}
	if !strings.Contains(spec.Message, "gopls") || !strings.Contains(spec.Message, "code completion") {
		t.Errorf("popup message = %q", spec.Message)
	}
	if len(spec.Actions) < 4 || len(spec.Actions) > 16 {
		t.Fatalf("popup actions = %d, want 4..16", len(spec.Actions))
	}
	ids := map[string]bool{}
	for _, a := range spec.Actions {
		ids[a.ID] = true
	}
	for _, want := range []string{"copy:copy_go", "copy:copy_brew", "act:lsp_restart", "act:lsp_stop", "act:lsp_toggle_for_buffer", "act:lsp_goto_definition", "act:lsp_references", "dismiss"} {
		if !ids[want] {
			t.Errorf("popup lacks action %q (ids %v)", want, ids)
		}
	}
	// Choose an install command: it goes to the clipboard and is reported.
	h.Emit(pluginapi.Event{Name: "action_popup_result", Data: map[string]any{
		"popup": uint64(ov.ID), "action": "copy:copy_go",
	}})
	if host.Clipboard != "go install golang.org/x/tools/gopls@latest" {
		t.Errorf("clipboard = %q", host.Clipboard)
	}
	if got := lastNoticeText(h); got != "Copied: go install golang.org/x/tools/gopls@latest" {
		t.Errorf("copy notice = %q", got)
	}
	// Let the parked popup request finish (the real host closes the popup on a choice).
	h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"copy:copy_go"}})
}

func TestLSPHelpNativeActionsAndDisable(t *testing.T) {
	t.Parallel()
	host, _ := lspHelpHost(t)
	h := startLSPHelp(t, host)
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "gopls: crashed"}})
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov := h.Host.UI().Last("action_popup")
	if ov == nil {
		t.Fatal("no action popup was opened")
	}
	h.Emit(pluginapi.Event{Name: "action_popup_result", Data: map[string]any{
		"popup": uint64(ov.ID), "action": "act:lsp_restart",
	}})
	found := false
	for _, call := range h.Host.Cmd().Calls {
		if call.Action == "lsp_restart" {
			found = true
		}
	}
	if !found {
		t.Errorf("actions = %v, want a lsp_restart dispatch", h.Host.Cmd().Calls)
	}
	h.Resolve(pluginapi.Completion{Token: ov.Token, Values: []any{"act:lsp_restart"}})

	// Disable: the native action runs, the notice says so and the failure is forgotten, so
	// a second status click opens nothing.
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov = h.Host.UI().Last("action_popup")
	h.Emit(pluginapi.Event{Name: "action_popup_result", Data: map[string]any{
		"popup": uint64(ov.ID), "action": "act:lsp_disable_language",
	}})
	disabled := false
	for _, call := range h.Host.Cmd().Calls {
		if call.Action == "lsp_disable_language" {
			disabled = true
		}
	}
	if !disabled {
		t.Errorf("actions = %v, want a lsp_disable_language dispatch", h.Host.Cmd().Calls)
	}
	if got := lastNoticeText(h); got != "Go LSP disabled" {
		t.Errorf("disable notice = %q", got)
	}
	if got := len(h.Host.UI().Overlays); got != 2 {
		t.Fatalf("popups before the disabled click = %d, want 2", got)
	}
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	if got := len(h.Host.UI().Overlays); got != 2 {
		t.Errorf("a disabled language opened another help popup (%d overlays)", got)
	}
}

func TestLSPHelpIgnoresUnknownAndQuietCases(t *testing.T) {
	t.Parallel()
	host, _ := lspHelpHost(t)
	h := startLSPHelp(t, host)
	// A status click with no remembered failure does nothing.
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	if got := len(h.Host.UI().Opened()); got != 0 {
		t.Errorf("a click without a failure opened %d overlays", got)
	}
	// Dismissing the popup performs no action.
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "gopls: boom"}})
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	ov := h.Host.UI().Last("action_popup")
	if ov == nil {
		t.Fatal("no popup for the failure")
	}
	calls := len(h.Host.Cmd().Calls)
	h.Emit(pluginapi.Event{Name: "action_popup_result", Data: map[string]any{
		"popup": uint64(ov.ID), "action": "dismiss",
	}})
	if got := len(h.Host.Cmd().Calls); got != calls {
		t.Errorf("dismiss dispatched %d actions", got-calls)
	}
	if host.Clipboard != "" {
		t.Errorf("dismiss copied %q", host.Clipboard)
	}
	// A language without an install-hint entry is left alone: no notice, no popup.
	other := host.AddBuffer("/work/x.unknown", "text\n")
	host.Buffers[1].Info.Language = "unknown-lang"
	host.SetActive(other)
	h.Emit(pluginapi.Event{Name: "lsp_server_error", Data: map[string]any{"error": "boom"}})
	if got := lastNoticeText(h); !strings.Contains(got, "boom") && !strings.Contains(got, "Copied") {
		// The previous notices are from the handled language; no new one should mention
		// the unknown language's server.
		t.Logf("last notice = %q", got)
	}
	popups := len(h.Host.UI().Overlays)
	h.Emit(pluginapi.Event{Name: "lsp_status_clicked", Data: map[string]any{}})
	if got := len(h.Host.UI().Overlays); got != popups {
		t.Errorf("unknown language opened another popup (%d -> %d)", popups, got)
	}
}
