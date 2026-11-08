package lsphost

import (
	"errors"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
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
	port := newPort(lspPlan{}, true, nil)
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
	want := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 5}, To: buffer.Pos{Line: 0, Col: 8}, Text: "bar"}
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
	port := newPort(lspPlan{}, true, nil)
	label := "organize"
	if !port.enqueueEdit("go", protocol.ApplyWorkspaceEditParams{Label: &label, Edit: protocol.WorkspaceEdit{}}) {
		t.Fatal("the queue refused an edit")
	}
	edits := port.WorkspaceEdits()
	if len(edits) != 1 || edits[0].Label != "organize" {
		t.Fatalf("drained = %+v, want the labeled edit", edits)
	}
	if again := port.WorkspaceEdits(); len(again) != 0 {
		t.Fatalf("the queue kept %d edits after a drain", len(again))
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
