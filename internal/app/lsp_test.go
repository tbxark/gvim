package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/lsp"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestNewLSPPortWithoutServersIsNil(t *testing.T) {
	t.Parallel()
	for _, servers := range []map[string]config.LSPServer{nil, {}} {
		if port := newLSPPort(servers); port != nil {
			t.Fatalf("newLSPPort(%v) = %T, want nil so that no LSP feature is on", servers, port)
		}
	}
}

func TestLSPPortSupportsOnlyConfiguredLanguages(t *testing.T) {
	t.Parallel()
	port := newLSPPort(map[string]config.LSPServer{"go": {Command: "gopls"}})
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
	port := newLSPPort(map[string]config.LSPServer{"go": {Command: "gopls"}})
	path := filepath.Join(t.TempDir(), "a.go")
	at := buffer.Pos{}

	if err := port.Change(path, "x"); !errors.Is(err, lsp.ErrNotOpen) {
		t.Errorf("Change: err = %v, want ErrNotOpen", err)
	}
	if _, err := port.Definition(path, at); !errors.Is(err, lsp.ErrNotOpen) {
		t.Errorf("Definition: err = %v, want ErrNotOpen", err)
	}
	if _, err := port.Rename(path, at, "y"); !errors.Is(err, lsp.ErrNotOpen) {
		t.Errorf("Rename: err = %v, want ErrNotOpen", err)
	}
	if _, ok := port.Diagnostics(path); ok {
		t.Error("Diagnostics of an unopened document reported an event")
	}
}

func TestLSPPortRequestForAnUnopenedDocumentIsNotFound(t *testing.T) {
	t.Parallel()
	port := newLSPPort(map[string]config.LSPServer{"go": {Command: "gopls"}})
	_, err := port.Request(filepath.Join(t.TempDir(), "a.go"), "textDocument/hover", map[string]any{})
	if pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("Request: err = %v, want a not_found error", err)
	}
}

func TestLSPPortOpenForUnconfiguredLanguageStartsNothing(t *testing.T) {
	t.Parallel()
	port := newLSPPort(map[string]config.LSPServer{"go": {Command: "gopls"}})
	err := port.Open(filepath.Join(t.TempDir(), "a.rs"), "rust", "fn main() {}")
	if !errors.Is(err, lsp.ErrNoServer) {
		t.Fatalf("Open: err = %v, want ErrNoServer", err)
	}
}

func TestLSPPortStopAndRestartWithNoServerAreNoOps(t *testing.T) {
	t.Parallel()
	port := newLSPPort(map[string]config.LSPServer{"go": {Command: "gopls"}})
	if err := port.Restart(); err != nil {
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

func TestFileEditsGroupByPathAndConvertRanges(t *testing.T) {
	t.Parallel()
	texts := map[string]string{
		"/p/a.go": "foo()\n",
		"/p/b.go": "x := foo\n",
	}
	edits := map[string][]protocol.TextEdit{
		lsp.PathURI("/p/b.go"): {{
			Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 5}, End: protocol.Position{Line: 0, Character: 8}},
			NewText: "bar",
		}},
		lsp.PathURI("/p/a.go"): {{
			Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 3}},
			NewText: "bar",
		}},
	}
	got := fileEdits(edits, func(path string) string { return texts[path] })
	want := []struct {
		path string
		edit editor.LSPEdit
	}{
		{"/p/a.go", editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 3}, Text: "bar"}},
		{"/p/b.go", editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 5}, To: buffer.Pos{Line: 0, Col: 8}, Text: "bar"}},
	}
	if len(got) != len(want) {
		t.Fatalf("file edits = %+v, want %d files", got, len(want))
	}
	for i, w := range want {
		if got[i].Path != w.path || len(got[i].Edits) != 1 || got[i].Edits[0] != w.edit {
			t.Fatalf("file edit %d = %+v, want path %s with %+v", i, got[i], w.path, w.edit)
		}
	}
}

func TestApplyLSPEditsThroughTheEditorAdapter(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a := editorAdapter{ed: ed}
	edit := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 6}, To: buffer.Pos{Line: 0, Col: 11}, Text: "gvim"}
	if err := a.ApplyLSPEdits(path, []editor.LSPEdit{edit}); err != nil {
		t.Fatalf("ApplyLSPEdits: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "hello gvim\n" {
		t.Fatalf("text = %q, want the edit applied", got)
	}
}
