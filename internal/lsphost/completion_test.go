package lsphost

import (
	"testing"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
)

// TestUICompletionConvertsAdditionalEditsFromUTF16 checks the ranges of an
// auto-import edit are converted from the protocol's UTF-16 units to rune columns
// with the same document text the main range uses (the emoji is two units, one rune).
func TestUICompletionConvertsAdditionalEditsFromUTF16(t *testing.T) {
	t.Parallel()
	// a<emoji>b on line 0, xy on line 1.
	rec := lspDocRec{lines: newTextLines("a\U0001F600b\nxy")}
	p := &lspPort{}
	before := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 1}}
	after := protocol.Range{Start: protocol.Position{Line: 0, Character: 3}, End: protocol.Position{Line: 0, Character: 4}}
	item := lsp.CompletionItem{
		Label: "b", InsertText: "b",
		AdditionalTextEdits: []protocol.TextEdit{{Range: before, NewText: "A"}, {Range: after, NewText: "B"}},
	}
	got := p.uiCompletion("/p/a.go", rec, nil, lsp.Capabilities{}, item, false)
	if len(got.Additional) != 2 {
		t.Fatalf("additional edits = %+v, want two", got.Additional)
	}
	want := []tui.CompletionEdit{
		{Range: tui.LSPRange{Start: buffer.Pos{Line: 0, Col: 0}, End: buffer.Pos{Line: 0, Col: 1}}, Text: "A"},
		{Range: tui.LSPRange{Start: buffer.Pos{Line: 0, Col: 2}, End: buffer.Pos{Line: 0, Col: 3}}, Text: "B"},
	}
	for i := range want {
		if got.Additional[i] != want[i] {
			t.Fatalf("additional[%d] = %+v, want %+v", i, got.Additional[i], want[i])
		}
	}
	if got.Snippet || got.CursorOffset != -1 {
		t.Fatalf("plain item = snippet %v cursor %d, want plain", got.Snippet, got.CursorOffset)
	}
}

// TestUICompletionExpandsSnippetFormat checks snippet-format insert text is expanded
// with its final caret stop, and that the rune offset counts CJK text as runes.
func TestUICompletionExpandsSnippetFormat(t *testing.T) {
	t.Parallel()
	rec := lspDocRec{lines: newTextLines("x")}
	p := &lspPort{}
	item := lsp.CompletionItem{
		Label: "call", InsertText: "x($" + "{1:参数}$0)", Snippet: true,
		InsertTextFormat: protocol.InsertTextFormatSnippet,
	}
	got := p.uiCompletion("/p/a.go", rec, nil, lsp.Capabilities{}, item, false)
	if !got.Snippet {
		t.Fatalf("snippet marker = false, want true")
	}
	if got.Insert != "x(参数)" {
		t.Fatalf("insert = %q, want the expanded snippet", got.Insert)
	}
	if got.CursorOffset != 4 {
		t.Fatalf("cursor offset = %d, want the final stop at rune 4", got.CursorOffset)
	}
}

// TestUICompletionResolveToken checks only an item with data on a resolve-capable
// server, and no eager additional edits, gets a token; a token is asked for by its
// own number and an unknown one is refused.
func TestUICompletionResolveToken(t *testing.T) {
	t.Parallel()
	rec := lspDocRec{lines: newTextLines("x")}
	p := &lspPort{completionTokens: map[int]completionResolve{}}
	item := lsp.CompletionItem{Label: "fmt", InsertText: "fmt", HasData: true}
	resolvable := lsp.Capabilities{CompletionResolve: true}

	got := p.uiCompletion("/p/a.go", rec, nil, resolvable, item, true)
	if got.ResolveToken == 0 {
		t.Fatal("a resolvable auto-import item got no resolve token")
	}
	if got := p.uiCompletion("/p/a.go", rec, nil, lsp.Capabilities{}, item, true); got.ResolveToken != 0 {
		t.Fatalf("token = %d on a server without resolveProvider, want none", got.ResolveToken)
	}
	eager := item
	eager.AdditionalTextEdits = []protocol.TextEdit{{Range: protocol.Range{}, NewText: "import \"fmt\"\n"}}
	if got := p.uiCompletion("/p/a.go", rec, nil, resolvable, eager, true); got.ResolveToken != 0 {
		t.Fatalf("token = %d for an item with eager additional edits, want none", got.ResolveToken)
	}
	if _, err := p.ResolveCompletion("/p/a.go", 999); err == nil {
		t.Fatal("ResolveCompletion accepted an unknown token")
	}
}
