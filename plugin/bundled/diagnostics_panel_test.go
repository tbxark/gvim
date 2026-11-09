package bundled_test

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The first Fresh plugin port: refs/fresh/crates/fresh-editor/plugins/diagnostics_panel.ts
// (275 lines) on the live-panel part of plugins/lib/finder.ts (1862 lines), as a bundled
// Lua plugin on the API level 2 widget panel. The tests drive it over the fake host with the
// language servers on: open, rows grouped by file, Enter jump, diagnostics_updated refresh,
// empty state, the all-files toggle, the caps. No golden screens.

const diagPlugin = "diagnostics_panel"

// diagPanelFiles reads the plugin's files out of the embedded bundled tree in the shape
// plugintest groups into a directory plugin: init.lua plus the <name>.i18n.json table.
func diagPanelFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", diagPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+diagPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files[diagPlugin+"/"+name] = string(data)
	}
	return files
}

// startDiagPanel starts the bundled plugin over host. The host's workspace root defaults to
// /w, the directory the fixture buffers live in.
func startDiagPanel(t *testing.T, host *plugintest.FakeHost) *plugintest.Harness {
	t.Helper()
	if host.Workspace().RootDir == "" {
		host.Workspace().RootDir = "/w"
	}
	h := plugintest.New(t, diagPanelFiles(t), plugintest.Options{
		Host: host, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second,
	})
	h.Start()
	return h
}

// diag is one diagnostic fixture (0-based line and byte column, as gvim.lsp reports).
func diag(line, col int, severity, message, source string) pluginapi.DiagnosticInfo {
	return pluginapi.DiagnosticInfo{
		Range:    pluginapi.Range{From: pluginapi.Pos{Line: line, Col: col}, To: pluginapi.Pos{Line: line, Col: col + 1}},
		Severity: severity,
		Message:  message,
		Source:   source,
	}
}

// diagHost is a fake host with two files, /w/a.go active and /w/b.go open, and the language
// servers on.
func diagHost(t *testing.T) (*plugintest.FakeHost, pluginapi.BufferID, pluginapi.BufferID) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	a := host.AddBuffer("/w/a.go", "package a\n\nfunc main() {}\n")
	b := host.AddBuffer("/w/b.go", "package b\n")
	host.LSP().Enabled = true
	return host, a, b
}

// mountedPanel returns the one mounted panel, failing the test otherwise.
func mountedPanel(t *testing.T, h *plugintest.Harness) *plugintest.FakePanel {
	t.Helper()
	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 1 {
		t.Fatalf("mounted panels = %d, want one (log %v)", len(mounted), h.LogText(""))
	}
	return mounted[0]
}

// widgetByKey finds a node of a spec by its key (the root of a spec has path "0" internally,
// so the lookup is recursive).
func widgetByKey(w pluginapi.Widget, key string) (pluginapi.Widget, bool) {
	if w.Key == key {
		return w, true
	}
	for _, child := range w.Children {
		if got, ok := widgetByKey(child, key); ok {
			return got, true
		}
	}
	return pluginapi.Widget{}, false
}

func mustWidget(t *testing.T, root pluginapi.Widget, key string) pluginapi.Widget {
	t.Helper()
	w, ok := widgetByKey(root, key)
	if !ok {
		t.Fatalf("the panel has no widget with key %q", key)
	}
	return w
}

// emitWidget sends the widget_event the ui would send for a panel.
func emitWidget(h *plugintest.Harness, panel pluginapi.PanelID, data map[string]any) {
	data["panel"] = uint64(panel)
	h.Emit(pluginapi.Event{Name: "widget_event", Data: data})
}

// TestDiagnosticsPanelManifestAndLoad: the plugin ships a manifest for the API level 2, its
// translation table, both Fresh commands registered with Fresh's titles, and it loads without
// a warning, a notice or a default key binding.
func TestDiagnosticsPanelManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/"+diagPlugin+"/plugin.json")
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
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != diagPlugin || m.Version == "" || strings.TrimSpace(m.Description) == "" {
		t.Errorf("manifest = %+v", m)
	}
	if m.API != 2 {
		t.Errorf("api = %d, want 2 (the widget panel and all_diagnostics level)", m.API)
	}
	if m.DefaultEnabled == nil || !*m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want true (Fresh ships the plugin enabled)", m.DefaultEnabled)
	}
	for _, name := range []string{"init.lua", diagPlugin + ".i18n.json"} {
		if _, err := fs.Stat(bundled.FS, "plugins/"+diagPlugin+"/"+name); err != nil {
			t.Error(err)
		}
	}

	// The English table has the command titles Fresh's i18n file has, and the panel texts.
	raw, err := fs.ReadFile(bundled.FS, "plugins/"+diagPlugin+"/"+diagPlugin+".i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	var table map[string]map[string]string
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"cmd.show_diagnostics_panel", "cmd.show_diagnostics_panel_desc",
		"cmd.toggle_diagnostics_panel", "cmd.toggle_diagnostics_panel_desc",
		"panel.header", "panel.current_file", "panel.all_files", "panel.no_diagnostics",
		"status.closed", "status.jumped_to", "status.diagnostics_count",
	} {
		if table["en"][key] == "" {
			t.Errorf("the English table has no %q", key)
		}
	}

	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	h := startDiagPanel(t, host)
	info, ok := h.Plugin(diagPlugin)
	if !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v, want loaded (log %v)", info, h.LogText(""))
	}
	for _, l := range h.Log() {
		if l.Level == "warn" || l.Level == "error" {
			t.Errorf("load logged %s: %s", l.Level, l.Msg)
		}
	}
	if notices := h.NoticeTexts(); len(notices) != 0 {
		t.Errorf("load notices = %v, want none", notices)
	}
	var commands []string
	for _, c := range h.Host.Commands[diagPlugin] {
		commands = append(commands, c.ID+"="+c.Title)
	}
	want := []string{
		diagPlugin + ".show_diagnostics_panel=Show Diagnostics Panel",
		diagPlugin + ".toggle_diagnostics_panel=Toggle Diagnostics Panel",
	}
	if !slices.Equal(commands, want) {
		t.Errorf("commands = %v, want %v", commands, want)
	}
	if bindings := h.Host.Bindings[diagPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none (Fresh binds no key for this plugin)", bindings)
	}
}

// TestDiagnosticsPanelOpensOnCurrentFile: Fresh's default filter is the active file; the rows
// are grouped by file (a header) and sorted by line then severity, with Fresh's "[E] line:col
// message" text and the source in the detail column.
func TestDiagnosticsPanelOpensOnCurrentFile(t *testing.T) {
	t.Parallel()
	host, a, b := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{
		diag(2, 4, "error", "undefined: foo", "gopls"),
		diag(0, 0, "warning", "unused variable", "gopls"),
	}
	host.LSP().Diags[b] = []pluginapi.DiagnosticInfo{diag(1, 2, "hint", "consider this", "staticcheck")}
	h := startDiagPanel(t, host)

	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	if fp.Mount.Placement != pluginapi.PlacementFloat || !fp.Mount.Focus || !fp.Mount.Closable {
		t.Errorf("mount = %+v, want a focused closable float", fp.Mount)
	}
	if fp.Mount.Title != "Diagnostics (Current File)" {
		t.Errorf("title = %q", fp.Mount.Title)
	}

	tree := mustWidget(t, fp.Panel.Root, "rows")
	if tree.Kind != pluginapi.WidgetTree {
		t.Fatalf("rows widget = %s, want a tree", tree.Kind)
	}
	// One file header plus the active file's two diagnostics, warning (line 0) first.
	if len(tree.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (%+v)", len(tree.Nodes), tree.Nodes)
	}
	if tree.Nodes[0].Depth != 0 || tree.Nodes[0].Text != "a.go:" || tree.Nodes[0].Detail != "" {
		t.Errorf("file header = %+v", tree.Nodes[0])
	}
	if tree.Nodes[1].Depth != 1 || tree.Nodes[1].Text != "[W] 1:1 unused variable" || tree.Nodes[1].Detail != "gopls" {
		t.Errorf("first row = %+v", tree.Nodes[1])
	}
	if tree.Nodes[2].Depth != 1 || tree.Nodes[2].Text != "[E] 3:5 undefined: foo" || tree.Nodes[2].Detail != "gopls" {
		t.Errorf("second row = %+v", tree.Nodes[2])
	}
	if tree.Selected != 1 {
		t.Errorf("selected = %d, want the first diagnostic node (1)", tree.Selected)
	}
	for _, n := range tree.Nodes {
		if n.Key == "" || len(n.Key) > 128 {
			t.Errorf("node key = %q, want non-empty and at most 128 bytes", n.Key)
		}
	}

	// The header row carries Fresh's filter toggle and the count; the hints name the keys.
	scope := mustWidget(t, fp.Panel.Root, "scope")
	if scope.Kind != pluginapi.WidgetToggle || scope.Label != "All files" || scope.Checked {
		t.Errorf("scope toggle = %+v", scope)
	}
	if count := mustWidget(t, fp.Panel.Root, "count"); count.Text != "2 items" {
		t.Errorf("count = %q", count.Text)
	}
	hints := mustWidget(t, fp.Panel.Root, "hints")
	if len(hints.Runs) != 2 || hints.Runs[0].Text != "Enter: jump" || hints.Runs[1].Text != "Esc: close" {
		t.Errorf("hints = %+v", hints.Runs)
	}
	if got := h.NoticeTexts(); !slices.Equal(got, []string{"Diagnostics: 2 items"}) {
		t.Errorf("notices = %v", got)
	}
	// The current-file filter asks only the active document, never every open document.
	if n := h.Host.CallCount("AllDiagnostics"); n != 0 {
		t.Errorf("all_diagnostics called %d times, want 0 for the current-file filter", n)
	}
	for _, l := range h.Log() {
		if l.Level == "warn" || l.Level == "error" {
			t.Errorf("run logged %s: %s", l.Level, l.Msg)
		}
	}
}

// TestDiagnosticsPanelEnterJumps: activating a diagnostic row opens its file at the byte
// position gvim reported, focuses it, closes the panel (the widget panel holds the keyboard)
// and reports the jump like Fresh's status.jumped_to.
func TestDiagnosticsPanelEnterJumps(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{diag(2, 4, "error", "undefined: foo", "gopls")}
	h := startDiagPanel(t, host)

	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	// The ui reports the key of the activated row, not the tree's key.
	emitWidget(h, fp.ID, map[string]any{"event": "activate", "widget": tree.Nodes[1].Key, "index": 2})

	// The panel closes at once so the keyboard goes back to the editor.
	if got := h.Host.Panels().Mounted(); len(got) != 0 {
		t.Fatalf("mounted after activate = %d, want 0", len(got))
	}
	var req pluginapi.OpenRequest
	opened := false
	for _, c := range h.Host.Calls() {
		if c.Method == "Open" {
			req, opened = c.Args[1].(pluginapi.OpenRequest), true
		}
	}
	if !opened {
		t.Fatalf("no open request (calls %v)", h.Host.Calls())
	}
	if req.Path != "/w/a.go" || !req.Focus {
		t.Errorf("open request = %+v, want /w/a.go focused", req)
	}
	if req.Pos == nil || req.Pos.Line != 2 || req.Pos.Col != 4 {
		t.Errorf("open pos = %+v, want line 2 col 4 (0-based)", req.Pos)
	}

	h.CompleteBuffers()
	cursors, err := h.Host.Cursors(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(cursors) != 1 || cursors[0].Pos.Line != 2 || cursors[0].Pos.Col != 4 {
		t.Errorf("cursors = %+v, want line 2 col 4", cursors)
	}
	if got := h.NoticeTexts(); !slices.Equal(got, []string{"Diagnostics: 1 items", "Jumped to /w/a.go:3"}) {
		t.Errorf("notices = %v", got)
	}
}

// TestDiagnosticsPanelEnterOnFileHeaderDoesNothing: the grouped file header is a row of the
// tree but not a location; activating it opens nothing and leaves the panel up.
func TestDiagnosticsPanelEnterOnFileHeaderDoesNothing(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{diag(0, 0, "error", "boom", "gopls")}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	emitWidget(h, fp.ID, map[string]any{"event": "activate", "widget": tree.Nodes[0].Key, "index": 1})
	if got := h.Host.CallCount("Open"); got != 0 {
		t.Errorf("open calls = %d, want 0 for a file header", got)
	}
	if got := h.Host.Panels().Mounted(); len(got) != 1 {
		t.Fatalf("mounted = %d, want the panel still up", len(got))
	}
}

// TestDiagnosticsPanelRefreshesOnDiagnosticsUpdated: the editor's diagnostics_updated event
// rebuilds the open panel, and showing an open panel refreshes it instead of mounting two.
func TestDiagnosticsPanelRefreshesOnDiagnosticsUpdated(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{diag(0, 0, "error", "first", "gopls")}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	if got := len(mustWidget(t, fp.Panel.Root, "rows").Nodes); got != 2 {
		t.Fatalf("nodes = %d, want one header and one row", got)
	}

	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{
		diag(0, 0, "error", "first", "gopls"),
		diag(4, 1, "warning", "second", "gopls"),
	}
	h.Emit(pluginapi.Event{Name: "diagnostics_updated", Data: map[string]any{
		"buf": a, "path": "/w/a.go", "counts": map[string]any{"error": 1, "warning": 1},
	}})

	if got := h.Host.Panels().Mounted(); len(got) != 1 || got[0].ID != fp.ID {
		t.Fatalf("panels = %+v, want the same panel refreshed", got)
	}
	if n := h.Host.CallCount("UpdateWidgetPanel"); n != 1 {
		t.Errorf("updates = %d, want 1", n)
	}
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 3 {
		t.Fatalf("nodes after the refresh = %d, want 3 (%+v)", len(tree.Nodes), tree.Nodes)
	}
	if tree.Nodes[2].Text != "[W] 5:2 second" {
		t.Errorf("new row = %q", tree.Nodes[2].Text)
	}
	if count := mustWidget(t, fp.Panel.Root, "count"); count.Text != "2 items" {
		t.Errorf("count = %q", count.Text)
	}

	// Showing the open panel again refreshes it: no second mount.
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	if n := h.Host.CallCount("MountWidgetPanel"); n != 1 {
		t.Errorf("mounts = %d, want 1", n)
	}
	if n := h.Host.CallCount("UpdateWidgetPanel"); n != 2 {
		t.Errorf("updates = %d, want 2", n)
	}
}

// TestDiagnosticsPanelEmptyState: with no diagnostics (a server that has published nothing,
// or no server at all) the panel still opens and says so; it never calls all_diagnostics for
// the current-file view.
func TestDiagnosticsPanelEmptyState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		lsp  bool
	}{{"server with nothing to report", true}, {"no language server", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, _, _ := diagHost(t)
			host.LSP().Enabled = tc.lsp
			h := startDiagPanel(t, host)
			if err := h.Run(diagPlugin+".toggle_diagnostics_panel", nil); err != nil {
				t.Fatal(err)
			}
			fp := mountedPanel(t, h)
			if _, ok := widgetByKey(fp.Panel.Root, "rows"); ok {
				t.Fatal("the empty state has a result tree")
			}
			empty := mustWidget(t, fp.Panel.Root, "empty")
			if empty.Kind != pluginapi.WidgetLabel || empty.Text != "No diagnostics" {
				t.Errorf("empty state = %+v", empty)
			}
			if count := mustWidget(t, fp.Panel.Root, "count"); count.Text != "0 items" {
				t.Errorf("count = %q", count.Text)
			}
			if fp.Panel.FocusKey != "scope" {
				t.Errorf("focus_key = %q, want the toggle", fp.Panel.FocusKey)
			}
			if got := h.NoticeTexts(); !slices.Equal(got, []string{"Diagnostics: 0 items"}) {
				t.Errorf("notices = %v", got)
			}
			if n := h.Host.CallCount("AllDiagnostics"); n != 0 {
				t.Errorf("all_diagnostics called %d times", n)
			}
		})
	}
}

// TestDiagnosticsPanelAllFilesToggle: the panel's toggle is Fresh's 'a' key — it switches the
// filter to every file and back, the title follows, and toggling while the panel is up does
// not leave a second panel mounted.
func TestDiagnosticsPanelAllFilesToggle(t *testing.T) {
	t.Parallel()
	host, a, b := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{diag(0, 0, "error", "in a", "gopls")}
	host.LSP().Diags[b] = []pluginapi.DiagnosticInfo{diag(1, 0, "warning", "in b", "gopls")}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	emitWidget(h, fp.ID, map[string]any{"event": "toggle", "widget": "scope", "checked": true})

	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 1 {
		t.Fatalf("mounted = %d, want one panel after the toggle", len(mounted))
	}
	fp = mounted[0]
	if fp.ID == 0 || fp.Mount.Title != "Diagnostics (All Files)" {
		t.Errorf("title = %q, want the all-files title", fp.Mount.Title)
	}
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 4 {
		t.Fatalf("nodes = %d, want two headers and two rows (%+v)", len(tree.Nodes), tree.Nodes)
	}
	if tree.Nodes[0].Text != "a.go:" || tree.Nodes[2].Text != "b.go:" {
		t.Errorf("headers = %q, %q", tree.Nodes[0].Text, tree.Nodes[2].Text)
	}
	if scope := mustWidget(t, fp.Panel.Root, "scope"); !scope.Checked {
		t.Error("the scope toggle is not checked in the all-files view")
	}
	if n := h.Host.CallCount("AllDiagnostics"); n == 0 {
		t.Error("the all-files view never called all_diagnostics")
	}
	if got := h.NoticeTexts(); got[len(got)-1] != "Showing: All Files" {
		t.Errorf("last notice = %q", got[len(got)-1])
	}

	// And back to the current file.
	emitWidget(h, fp.ID, map[string]any{"event": "toggle", "widget": "scope", "checked": false})
	mounted = h.Host.Panels().Mounted()
	if len(mounted) != 1 || mounted[0].Mount.Title != "Diagnostics (Current File)" {
		t.Fatalf("mounted after the second toggle = %+v", mounted)
	}
	if tree := mustWidget(t, mounted[0].Panel.Root, "rows"); len(tree.Nodes) != 2 {
		t.Errorf("nodes = %d, want only the active file", len(tree.Nodes))
	}
}

// TestDiagnosticsPanelCancelClearsState: the host's Escape path unmounts the panel and sends
// "cancel"; the plugin must forget its id so the next Show mounts a fresh panel.
func TestDiagnosticsPanelCancelClearsState(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{diag(0, 0, "error", "boom", "gopls")}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	// The ui unmounts a closable panel and then raises cancel on the plugin.
	h.Host.UnmountWidgetPanel(fp.ID)
	emitWidget(h, fp.ID, map[string]any{"event": "cancel"})
	if got := h.NoticeTexts(); got[len(got)-1] != "Diagnostics panel closed" {
		t.Fatalf("last notice = %q", got[len(got)-1])
	}
	if got := h.Host.Panels().Mounted(); len(got) != 0 {
		t.Fatalf("mounted = %d, want 0", len(got))
	}
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	if n := h.Host.CallCount("MountWidgetPanel"); n != 2 {
		t.Errorf("mounts = %d, want a fresh mount after the cancel", n)
	}
}

// TestDiagnosticsPanelCollapsesDuplicates: Fresh's Finder keeps one entry per id; a server
// that publishes the same diagnostic twice lists it once and the count agrees.
func TestDiagnosticsPanelCollapsesDuplicates(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	same := diag(1, 2, "error", "duplicate", "gopls")
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{same, same, diag(0, 0, "hint", "other", "gopls")}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 3 {
		t.Fatalf("nodes = %d, want a header and the two distinct rows (%+v)", len(tree.Nodes), tree.Nodes)
	}
	if tree.Nodes[1].Text != "[H] 1:1 other" || tree.Nodes[2].Text != "[E] 2:3 duplicate" {
		t.Errorf("rows = %q, %q", tree.Nodes[1].Text, tree.Nodes[2].Text)
	}
	if count := mustWidget(t, fp.Panel.Root, "count"); count.Text != "2 items" {
		t.Errorf("count = %q, want the deduped count", count.Text)
	}
}

// TestDiagnosticsPanelRowsAreBounded: a file with more diagnostics than the row cap lists
// exactly MAX_ROWS rows, and a huge or multi-line message and a long source are cut (one
// line, display-cell budgets), so no unbounded string reaches the panel.
func TestDiagnosticsPanelRowsAreBounded(t *testing.T) {
	t.Parallel()
	const total = 1200 // more than the plugin's 500-row cap
	host, a, b := diagHost(t)
	diags := make([]pluginapi.DiagnosticInfo, 0, total)
	diags = append(diags, diag(0, 0, "error", strings.Repeat("m", 1000)+"\nsecond line", strings.Repeat("s", 200)))
	for i := 1; i < total; i++ {
		diags = append(diags, diag(i, 0, "error", "problem "+strings.Repeat("x", i%7), "gopls"))
	}
	host.LSP().Diags[a] = diags
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 501 {
		t.Fatalf("nodes = %d, want 500 rows plus one file header", len(tree.Nodes))
	}
	first := tree.Nodes[1]
	if strings.Contains(first.Text, "\n") || strings.Contains(first.Text, "second line") {
		t.Errorf("row text carries more than the first message line: %q", first.Text)
	}
	if len(first.Text) > 200 {
		t.Errorf("row text is %d bytes, want the message cut", len(first.Text))
	}
	if len(first.Detail) > 40 {
		t.Errorf("source detail is %d bytes, want it cut to 40", len(first.Detail))
	}
	if count := mustWidget(t, fp.Panel.Root, "count"); count.Text != "500 items" {
		t.Errorf("count = %q, want the capped count", count.Text)
	}

	// The all-files filter is capped the same way and stops reading the list of files once
	// the rows are full: the second file is never reached.
	host.LSP().Diags[b] = []pluginapi.DiagnosticInfo{diag(0, 0, "hint", "in b", "gopls")}
	emitWidget(h, fp.ID, map[string]any{"event": "toggle", "widget": "scope", "checked": true})
	mounted := h.Host.Panels().Mounted()
	if len(mounted) != 1 {
		t.Fatalf("mounted = %d, want one panel", len(mounted))
	}
	tree = mustWidget(t, mounted[0].Panel.Root, "rows")
	if len(tree.Nodes) != 501 {
		t.Fatalf("all-files nodes = %d, want the 500-row cap plus one header", len(tree.Nodes))
	}
	for _, n := range tree.Nodes {
		if strings.Contains(n.Text, "in b") {
			t.Fatalf("the all-files list read past the row cap: %q", n.Text)
		}
	}
}

func TestDiagnosticsPanelSyncsSelectionToEditorCursor(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{
		diag(2, 4, "error", "undefined: foo", "gopls"),
	}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want header + diagnostic", tree.Nodes)
	}
	// Move the editor cursor onto the diagnostic's exact line. Fresh's syncWithEditor
	// reveals/selects that panel row without changing focus.
	if err := host.SetCursor(a, pluginapi.Pos{Line: 2, Col: 4}, false, false, false); err != nil {
		t.Fatal(err)
	}
	h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
		"buf": a, "pos": map[string]any{"line": 2, "col": 4},
	}})
	if len(fp.Mutations) != 1 {
		t.Fatalf("mutations = %+v, want one selected-index mutation", fp.Mutations)
	}
	mu := fp.Mutations[0]
	if mu.Kind != "setSelectedIndex" || mu.WidgetKey != "rows" || mu.Index != 1 {
		t.Fatalf("mutation = %+v, want rows index 1 (the diagnostic node, 0-based)", mu)
	}
	// A line with no diagnostic leaves the selection alone.
	h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
		"buf": a, "pos": map[string]any{"line": 1, "col": 0},
	}})
	if len(fp.Mutations) != 1 {
		t.Fatalf("mutations after a non-diagnostic line = %+v, want unchanged", fp.Mutations)
	}
}

func TestDiagnosticsPanelPreviewsPanelSelection(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{
		diag(2, 4, "error", "undefined: foo", "gopls"),
		diag(0, 0, "warning", "unused variable", "gopls"),
	}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want header + two diagnostics", tree.Nodes)
	}
	// Two rapid selection moves: Fresh debounces and previews only the latest location as a
	// background preview tab, without changing focus.
	emitWidget(h, fp.ID, map[string]any{"event": "select", "widget": "rows", "index": 2})
	h.Settle()
	emitWidget(h, fp.ID, map[string]any{"event": "select", "widget": "rows", "index": 3})
	h.Settle()
	h.Advance(100 * time.Millisecond)
	h.CompleteBuffers()

	var opens []pluginapi.OpenRequest
	for _, c := range h.Host.Calls() {
		if c.Method == "Open" {
			if req, ok := c.Args[1].(pluginapi.OpenRequest); ok {
				opens = append(opens, req)
			}
		}
	}
	if len(opens) != 1 {
		t.Fatalf("open requests = %+v, want exactly one debounced preview", opens)
	}
	req := opens[0]
	if req.Path != "/w/a.go" || !req.Preview || !req.Background || req.Focus {
		t.Fatalf("preview request = %+v, want a background non-focused preview of /w/a.go", req)
	}
	if req.Pos == nil || req.Pos.Line != 2 || req.Pos.Col != 4 {
		t.Fatalf("preview pos = %+v, want line 2 col 4 (the latest selection)", req.Pos)
	}
	if got := h.Host.Panels().Mounted(); len(got) != 1 {
		t.Fatalf("panel mounted after preview = %d, want 1", len(got))
	}
	h.CompleteBuffers()
}

func TestDiagnosticsPanelRefreshCancelsPendingPreview(t *testing.T) {
	t.Parallel()
	host, a, _ := diagHost(t)
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{
		diag(0, 0, "warning", "first", "gopls"),
		diag(2, 4, "error", "selected", "gopls"),
	}
	h := startDiagPanel(t, host)
	if err := h.Run(diagPlugin+".show_diagnostics_panel", nil); err != nil {
		t.Fatal(err)
	}
	fp := mountedPanel(t, h)
	tree := mustWidget(t, fp.Panel.Root, "rows")
	if len(tree.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want header + two rows", tree.Nodes)
	}

	// Select the pending row (line 2); the preview timer is now armed for it.
	emitWidget(h, fp.ID, map[string]any{"event": "select", "widget": "rows", "index": 3})
	h.Settle()

	// Refresh inside the debounce window and remove the selected diagnostic. The old row
	// table is no longer in the rebuilt map, so the timer must be cancelled rather than open
	// its stale line.
	host.LSP().Diags[a] = []pluginapi.DiagnosticInfo{diag(0, 0, "warning", "first", "gopls")}
	h.Emit(pluginapi.Event{Name: "diagnostics_updated", Data: map[string]any{
		"buf": a, "path": "/w/a.go", "counts": map[string]any{"warning": 1},
	}})
	h.Settle()
	if n := h.Host.CallCount("UpdateWidgetPanel"); n != 1 {
		t.Fatalf("updates = %d, want the refresh to have run", n)
	}
	h.Advance(100 * time.Millisecond)
	h.CompleteBuffers()
	if n := h.Host.CallCount("Open"); n != 0 {
		t.Fatalf("opens = %d, want none: refresh must cancel the stale pending preview", n)
	}

	// A pending preview is also dropped when the selection moves to the file header: the
	// timer may remain armed, but its row is no longer the current selection.
	emitWidget(h, fp.ID, map[string]any{"event": "select", "widget": "rows", "index": 2})
	h.Settle()
	emitWidget(h, fp.ID, map[string]any{"event": "select", "widget": "rows", "index": 1})
	h.Settle()
	h.Advance(100 * time.Millisecond)
	h.CompleteBuffers()
	if n := h.Host.CallCount("Open"); n != 0 {
		t.Fatalf("opens = %d, want none: only the current selected row may preview", n)
	}
}
