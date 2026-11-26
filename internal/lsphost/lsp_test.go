package lsphost

import (
	"context"
	"errors"
	"github.com/tbxark/gvim/document"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/manager"
	"github.com/tbxark/gvim/pluginapi"
)

func TestNewLSPPortWithoutServersIsNil(t *testing.T) {
	t.Parallel()
	for _, servers := range []map[string]config.LSPServer{nil, {}} {
		if port := New(servers); port != nil {
			t.Fatalf("New(%v) = %T, want nil so that no LSP feature is on", servers, port)
		}
	}
}

func TestLSPPortSupportsOnlyConfiguredLanguages(t *testing.T) {
	t.Parallel()
	port := New(map[string]config.LSPServer{"go": {Command: "gopls"}})
	tests := []struct {
		lang string
		want bool
	}{
		{"go", true},
		{"rust", false},
		{"Go", false}, // language IDs are lower case
		{"", false},
	}
	for _, tt := range tests {
		if got := port.Supports(tt.lang); got != tt.want {
			t.Errorf("Supports(%q) = %v, want %v", tt.lang, got, tt.want)
		}
	}
}

func TestLSPPositionsCountUTF16Units(t *testing.T) {
	t.Parallel()
	// The emoji is one rune and two UTF-16 units, so the column after it is 3 in
	// UTF-16 and 2 in runes.
	lines := newTextLines("a\U0001F600b\nxy")
	tests := []struct {
		name string
		rune buffer.Pos
		lsp  protocol.Position
	}{
		{"start", buffer.Pos{Line: 0, Col: 0}, protocol.Position{Line: 0, Character: 0}},
		{"before the emoji", buffer.Pos{Line: 0, Col: 1}, protocol.Position{Line: 0, Character: 1}},
		{"after the emoji", buffer.Pos{Line: 0, Col: 2}, protocol.Position{Line: 0, Character: 3}},
		{"second line", buffer.Pos{Line: 1, Col: 2}, protocol.Position{Line: 1, Character: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lines.toLSP(tt.rune); got != tt.lsp {
				t.Fatalf("toLSP(%+v) = %+v, want %+v", tt.rune, got, tt.lsp)
			}
			if got := lines.fromLSP(tt.lsp); got != tt.rune {
				t.Fatalf("fromLSP(%+v) = %+v, want %+v", tt.lsp, got, tt.rune)
			}
		})
	}
}

func TestLSPPortRequestsForUnopenedDocumentsAreNotOpen(t *testing.T) {
	t.Parallel()
	port := New(map[string]config.LSPServer{"go": {Command: "gopls"}})
	path := filepath.Join(t.TempDir(), "a.go")
	at := buffer.Pos{}

	if _, err := port.Change(path, "x"); !errors.Is(err, manager.ErrNotOpen) {
		t.Errorf("Change: err = %v, want ErrNotOpen", err)
	}
	if _, err := port.Definition(path, at); !errors.Is(err, manager.ErrNotOpen) {
		t.Errorf("Definition: err = %v, want ErrNotOpen", err)
	}
	if _, err := port.Rename(path, at, "y"); !errors.Is(err, manager.ErrNotOpen) {
		t.Errorf("Rename: err = %v, want ErrNotOpen", err)
	}
	if _, ok := port.Diagnostics(path); ok {
		t.Error("Diagnostics of an unopened document reported an event")
	}
}

func TestLSPPortRequestForAnUnopenedDocumentIsNotFound(t *testing.T) {
	t.Parallel()
	port := New(map[string]config.LSPServer{"go": {Command: "gopls"}})
	_, err := port.Request(filepath.Join(t.TempDir(), "a.go"), "textDocument/hover", map[string]any{})
	if pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("Request: err = %v, want a not_found error", err)
	}
}

func TestLSPPortOpenForUnconfiguredLanguageStartsNothing(t *testing.T) {
	t.Parallel()
	port := New(map[string]config.LSPServer{"go": {Command: "gopls"}})
	_, err := port.Open(filepath.Join(t.TempDir(), "a.rs"), "rust", "fn main() {}")
	if !errors.Is(err, manager.ErrNoServer) {
		t.Fatalf("Open: err = %v, want ErrNoServer", err)
	}
}

func TestLSPPortStopAndRestartWithNoServerAreNoOps(t *testing.T) {
	t.Parallel()
	port := New(map[string]config.LSPServer{"go": {Command: "gopls"}})
	if err := port.Restart(""); err != nil {
		t.Errorf("Restart with no server: %v", err)
	}
	if err := port.Stop(); err != nil {
		t.Errorf("Stop with no server: %v", err)
	}
}

func TestUIDiagnosticsUseRuneColumns(t *testing.T) {
	t.Parallel()
	text := "a\U0001F600b\nxy"
	items := uiDiagnostics(newTextLines(text), []protocol.Diagnostic{{
		Range:    protocol.Range{Start: protocol.Position{Line: 0, Character: 3}, End: protocol.Position{Line: 1, Character: 2}},
		Severity: protocol.DiagnosticSeverityWarning,
		Message:  protocol.String("odd"),
		Source:   protocol.NewOptional("gopls"),
	}})
	if len(items) != 1 {
		t.Fatalf("items = %+v, want one", items)
	}
	got := items[0]
	if got.Start != (buffer.Pos{Line: 0, Col: 2}) || got.End != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("range = %+v..%+v, want rune positions (0,2)..(1,2)", got.Start, got.End)
	}
	if got.Source != "gopls" {
		t.Fatalf("source = %q, want gopls", got.Source)
	}
	if got.Severity != 2 || got.Message != "odd" {
		t.Fatalf("severity and message = %d %q, want 2 odd", got.Severity, got.Message)
	}
}

func TestDiagnosticTextTakesPlainAndMarkupMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  protocol.InlayHintTooltip
		want string
	}{
		{"plain", protocol.String("unused"), "unused"},
		{"markdown", &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: "**bold**"}, "**bold**"},
		{"nil markup", (*protocol.MarkupContent)(nil), ""},
		{"absent", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diagnosticText(tt.msg); got != tt.want {
				t.Fatalf("diagnosticText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkspaceEditConvertsTextAndResourceOperations(t *testing.T) {
	t.Parallel()
	port := newPort(lspPlan{
		servers: map[string][]manager.ServerConfig{
			"go": {{Command: []string{"gopls"}}},
		},
	}, true, nil)
	text := "x := foo\n"
	port.docs["/p/b.go"] = lspDocRec{lang: "go", text: text, lines: newTextLines(text)}
	version := int32(3)
	uriOf := func(p string) uri.URI { return uri.URI(lsp.PathURI(p)) }
	edit := &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
		&protocol.CreateFile{Kind: "create", URI: uriOf("/p/new.go")},
		&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
				TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uriOf("/p/b.go")},
				Version:                &version,
			},
			Edits: []protocol.TextDocumentEditElement{&protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 5}, End: protocol.Position{Line: 0, Character: 8}},
				NewText: "bar",
			}},
		},
		&protocol.RenameFile{Kind: "rename", OldURI: uriOf("/p/a.go"), NewURI: uriOf("/p/c.go")},
		&protocol.DeleteFile{Kind: "delete", URI: uriOf("/p/d.go")},
	}}
	got, err := port.workspaceEdit(edit, "fix")
	if err != nil {
		t.Fatalf("workspaceEdit: %v", err)
	}
	if got.Label != "fix" || len(got.Ops) != 4 {
		t.Fatalf("edit = %+v, want label fix and four operations", got)
	}
	if op := got.Ops[0]; op.Kind != tui.WorkspaceOpCreate || op.Path != "/p/new.go" {
		t.Errorf("op 0 = %+v, want a create of /p/new.go", op)
	}
	op := got.Ops[1]
	want := document.LSPEdit{From: buffer.Pos{Line: 0, Col: 5}, To: buffer.Pos{Line: 0, Col: 8}, Text: "bar"}
	if op.Kind != tui.WorkspaceOpText || op.Path != "/p/b.go" || op.Version == nil || *op.Version != version ||
		len(op.Edits) != 1 || op.Edits[0] != want {
		t.Errorf("op 1 = %+v, want the text edit with its version", op)
	}
	if op := got.Ops[2]; op.Kind != tui.WorkspaceOpRename || op.Path != "/p/a.go" || op.NewPath != "/p/c.go" {
		t.Errorf("op 2 = %+v, want a rename of /p/a.go to /p/c.go", op)
	}
	if op := got.Ops[3]; op.Kind != tui.WorkspaceOpDelete || op.Path != "/p/d.go" {
		t.Errorf("op 3 = %+v, want a delete of /p/d.go", op)
	}
}

// TestServerApplyEditIsQueuedAndDrained checks the server-initiated flow: the
// workspace/applyEdit handler queues the edit without waiting for the editor, and
// the root drains it later.
func TestServerApplyEditIsQueuedAndDrained(t *testing.T) {
	t.Parallel()
	port := newPort(lspPlan{
		servers: map[string][]manager.ServerConfig{
			"go": {{Command: []string{"gopls"}}},
		},
	}, true, nil)
	label := "organize"
	if applied, reason := port.enqueueEdit("go", protocol.ApplyWorkspaceEditParams{Label: &label, Edit: protocol.WorkspaceEdit{}}); !applied {
		t.Fatalf("the queue refused an edit: %s", reason)
	}
	edits := port.WorkspaceEdits()
	if len(edits) != 1 || edits[0].Label != "organize" {
		t.Fatalf("drained = %+v, want the labeled edit", edits)
	}
	if again := port.WorkspaceEdits(); len(again) != 0 {
		t.Fatalf("the queue kept %d edits after a drain", len(again))
	}
}

// TestServerApplyEditRefusesAnUnconvertibleEdit checks a server-initiated edit the port
// cannot convert is answered as not applied instead of being queued and dropped.
func TestServerApplyEditRefusesAnUnconvertibleEdit(t *testing.T) {
	t.Parallel()
	port := newPort(lspPlan{
		servers: map[string][]manager.ServerConfig{
			"go": {{Command: []string{"gopls"}}},
		},
	}, true, nil)
	bad := protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
		&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: "file:///p/a.go"}},
			Edits:        []protocol.TextDocumentEditElement{&protocol.SnippetTextEdit{}},
		},
	}}
	if applied, reason := port.enqueueEdit("go", protocol.ApplyWorkspaceEditParams{Edit: bad}); applied || reason == "" {
		t.Fatalf("an unsupported edit: applied=%v reason=%q, want a refusal with a reason", applied, reason)
	}
	if edits := port.WorkspaceEdits(); len(edits) != 0 {
		t.Fatalf("drained = %+v, want nothing queued", edits)
	}
}

// TestRangesOverlapForCodeActionContext checks the diagnostic context filter: a
// code action at the caret gets the diagnostics whose ranges cover it, which is
// what servers that gate quick fixes on the diagnostic need.
func TestRangesOverlapForCodeActionContext(t *testing.T) {
	t.Parallel()
	rng := func(sl, sc, el, ec uint32) protocol.Range {
		return protocol.Range{Start: protocol.Position{Line: sl, Character: sc}, End: protocol.Position{Line: el, Character: ec}}
	}
	tests := []struct {
		name string
		a, b protocol.Range
		want bool
	}{
		{"a diagnostic covering the caret", rng(1, 0, 1, 5), rng(1, 3, 1, 3), true},
		{"a diagnostic before the caret", rng(0, 0, 0, 5), rng(1, 0, 1, 0), false},
		{"a diagnostic after the caret", rng(2, 0, 2, 5), rng(1, 0, 1, 0), false},
		{"a diagnostic ending at the caret", rng(1, 0, 1, 3), rng(1, 3, 1, 5), true},
		{"a diagnostic on the same line after the caret", rng(1, 4, 1, 9), rng(1, 3, 1, 3), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rangesOverlap(tt.a, tt.b); got != tt.want {
				t.Fatalf("rangesOverlap = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkspaceEditTracksRenameAndRefusesUnreadableUnopenedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "old.go")
	newFile := filepath.Join(dir, "new.go")
	if err := os.WriteFile(oldFile, []byte("package p\r\nvar x = \U0001F600 + old\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	port := newPort(lspPlan{
		servers: map[string][]manager.ServerConfig{
			"go": {{Command: []string{"gopls"}}},
		},
	}, true, nil)
	uriOf := func(p string) uri.URI { return uri.URI(lsp.PathURI(p)) }

	// Rename old.go -> new.go, then edit new.go; CRLF must normalize and rune cols must map from old's text.
	edit := &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
		&protocol.RenameFile{Kind: "rename", OldURI: uriOf(oldFile), NewURI: uriOf(newFile)},
		&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uriOf(newFile)}},
			Edits: []protocol.TextDocumentEditElement{&protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 1, Character: 13}, End: protocol.Position{Line: 1, Character: 16}},
				NewText: "renamed",
			}},
		},
	}}
	got, err := port.workspaceEdit(edit, "rename")
	if err != nil {
		t.Fatalf("workspaceEdit rename+edit: %v", err)
	}
	if len(got.Ops) != 2 || got.Ops[1].Edits[0].From != (buffer.Pos{Line: 1, Col: 12}) || got.Ops[1].Edits[0].To != (buffer.Pos{Line: 1, Col: 15}) {
		t.Fatalf("ops = %+v, want rune cols 12..15 after emoji", got.Ops)
	}

	// An unopened file that cannot be read must be refused rather than silently clamping columns to 0.
	missing := filepath.Join(dir, "missing.go")
	bad := &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
		&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uriOf(missing)}},
			Edits: []protocol.TextDocumentEditElement{&protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 4}, End: protocol.Position{Line: 0, Character: 7}},
				NewText: "y",
			}},
		},
	}}
	if _, err := port.workspaceEdit(bad, "bad"); err == nil {
		t.Fatal("workspaceEdit succeeded for unreadable unopened file, want refusal")
	}
}

func TestRemoteReadFileAndSetSpawnEnvAndServersPriority(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	localOnly := filepath.Join(dir, "local.go")
	if err := os.WriteFile(localOnly, []byte("from local disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	port := newPort(lspPlan{
		servers: map[string][]manager.ServerConfig{
			"go": {{Command: []string{"gopls"}}},
		},
	}, true, nil)
	port.SetRemoteReadFile(func(_ context.Context, p string) ([]byte, error) {
		if p == "/remote/a.go" {
			return []byte("line1\r\nline2\r\n"), nil
		}
		return nil, errors.New("not found")
	})
	if got := port.textOf("/remote/a.go"); got != "line1\nline2\n" {
		t.Fatalf("remote textOf = %q, want normalized LF", got)
	}
	if got := port.textOf(localOnly); got != "" {
		t.Fatalf("remote textOf read local file %q: %q", localOnly, got)
	}

	// SetSpawnEnv propagates to already-created managers.
	mgr := port.managerFor(dir)
	var called int
	port.SetSpawnEnv(func() []string { called++; return []string{"FROM_PORT=1"} })
	mgr.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		if !strings.Contains(strings.Join(cfg.Env, ","), "FROM_PORT=1") {
			t.Errorf("live manager env = %v, want FROM_PORT=1", cfg.Env)
		}
		return nil, errors.New("stop")
	})
	_ = mgr.StartLanguage("go")
	if called == 0 {
		t.Fatal("updated spawnEnv was not propagated to live manager")
	}
}

func TestServerStatePriorityPrefersRunningOverStopped(t *testing.T) {
	t.Parallel()
	if serverStatePriority(manager.ServerRunning) <= serverStatePriority(manager.ServerStopped) {
		t.Fatal("ServerRunning priority must exceed ServerStopped")
	}
	if serverStatePriority(manager.ServerRunning) <= serverStatePriority(manager.ServerError) {
		t.Fatal("ServerRunning priority must exceed ServerError")
	}
	if serverStatePriority(manager.ServerStarting) <= serverStatePriority(manager.ServerError) {
		t.Fatal("ServerStarting priority must exceed ServerError")
	}
}

func TestResetReopenedDocClearsCachesAndSyncsVersion(t *testing.T) {
	t.Parallel()
	port := newPort(lspPlan{
		servers: map[string][]manager.ServerConfig{
			"go": {{Command: []string{"gopls"}}},
		},
	}, true, nil)
	mgr := port.managerFor("/w")
	port.docs["/w/a.go"] = lspDocRec{root: "/w", lang: "go", text: "package a\n", version: 7}
	port.sem["/w/a.go"] = semanticCache{version: 7, resultID: "stale", data: []uint32{1, 2, 3}}
	port.pulled["/w/a.go"] = pulledDiagnostics{version: 7}

	rec := port.resetReopenedDoc("/w/a.go", mgr)
	if rec.version != 1 {
		t.Fatalf("reopened version = %d, want 1", rec.version)
	}
	if _, ok := port.sem["/w/a.go"]; ok {
		t.Fatal("stale semantic cache survived reopen")
	}
	if _, ok := port.pulled["/w/a.go"]; ok {
		t.Fatal("stale pulled diagnostics cache survived reopen")
	}
}
