package lsp

import (
	"testing"

	"go.lsp.dev/protocol"
)

// TestCompletionItemKeepsResolveFields checks the reduced item keeps the snippet
// format, the additional edits (auto-imports) and the resolve payload, and that a
// plain item leaves all of them empty.
func TestCompletionItemKeepsResolveFields(t *testing.T) {
	t.Parallel()
	data := protocol.LSPAny([]byte("{}"))
	te := protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: 0}, End: protocol.Position{Line: 0}}, NewText: "import"}
	item := completionItem(protocol.CompletionItem{
		Label:               "fmt.Println",
		InsertTextFormat:    protocol.InsertTextFormatSnippet,
		InsertText:          protocol.NewOptional("fmt.Println($1)"),
		AdditionalTextEdits: []protocol.TextEdit{te},
		Data:                data,
	})
	if !item.Snippet || item.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Fatalf("snippet = %v format = %v, want a snippet item", item.Snippet, item.InsertTextFormat)
	}
	if len(item.AdditionalTextEdits) != 1 || item.AdditionalTextEdits[0].NewText != "import" {
		t.Fatalf("additional edits = %+v, want the auto-import", item.AdditionalTextEdits)
	}
	if !item.HasData || item.Data == nil {
		t.Fatalf("data = %v hasData = %v, want the resolve payload", item.Data, item.HasData)
	}
	if item.raw.Label != "fmt.Println" || item.raw.Data == nil {
		t.Fatalf("raw item = %+v, want the item as the server sent it", item.raw)
	}
	labelDetail := " (use std::collections::HashMap)"
	withLD := completionItem(protocol.CompletionItem{
		Label:        "HashMap",
		LabelDetails: &protocol.CompletionItemLabelDetails{Detail: &labelDetail},
	})
	if withLD.Detail != "(use std::collections::HashMap)" || withLD.Label != "HashMap" {
		t.Fatalf("labelDetails item = %+v, want the path as detail and the bare label", withLD)
	}
	plain := completionItem(protocol.CompletionItem{Label: "x"})
	if plain.Snippet || plain.HasData || len(plain.AdditionalTextEdits) != 0 {
		t.Fatalf("plain item = %+v, want no resolve metadata", plain)
	}
}

// TestSummarizeCompletionResolveCapability checks resolveProvider is read from the
// server capabilities and from a dynamic registration.
func TestSummarizeCompletionResolveCapability(t *testing.T) {
	t.Parallel()
	yes := true
	caps := summarizeCapabilities(protocol.ServerCapabilities{CompletionProvider: &protocol.CompletionOptions{ResolveProvider: &yes}})
	if !caps.Completion || !caps.CompletionResolve {
		t.Fatalf("caps = %+v, want completion and resolve", caps)
	}
	without := summarizeCapabilities(protocol.ServerCapabilities{CompletionProvider: &protocol.CompletionOptions{}})
	if !without.Completion || without.CompletionResolve {
		t.Fatalf("caps = %+v, want completion without resolve", without)
	}
	registered := Capabilities{}
	applyRegistration(&registered, "textDocument/completion", protocol.LSPAny([]byte(`{"resolveProvider":true}`)))
	if !registered.Completion || !registered.CompletionResolve {
		t.Fatalf("registered caps = %+v, want completion and resolve", registered)
	}
}

// TestClientCapabilitiesAdvertiseSnippetsAndResolve checks servers are told the
// client accepts snippet insert text and may lazily resolve additional edits.
func TestClientCapabilitiesAdvertiseSnippetsAndResolve(t *testing.T) {
	t.Parallel()
	caps := clientCapabilities()
	if caps.TextDocument == nil || caps.TextDocument.Completion == nil || caps.TextDocument.Completion.CompletionItem == nil {
		t.Fatalf("textDocument.completion capability missing: %+v", caps.TextDocument)
	}
	item := caps.TextDocument.Completion.CompletionItem
	if item.SnippetSupport == nil || !*item.SnippetSupport {
		t.Fatalf("snippetSupport = %v, want true", item.SnippetSupport)
	}
	if item.LabelDetailsSupport == nil || !*item.LabelDetailsSupport {
		t.Fatalf("labelDetailsSupport = %v, want true", item.LabelDetailsSupport)
	}
	props := item.ResolveSupport.Properties
	want := map[string]bool{"additionalTextEdits": true, "documentation": true}
	for _, p := range props {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("resolveSupport properties = %v, missing %v", props, want)
	}
}

// TestCompletionResolve checks the resolved item is sent back with the payload of
// the original item and its lazily filled additional edits are read.
func TestCompletionResolve(t *testing.T) {
	wait := protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: 0}, End: protocol.Position{Line: 0}}, NewText: "import \"fmt\"\n"}
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	f.srv.Canned.ResolvedCompletion = &protocol.CompletionItem{
		Label:               "fmt",
		InsertText:          protocol.NewOptional("fmt"),
		AdditionalTextEdits: []protocol.TextEdit{wait},
	}
	original := CompletionItem{
		Label: "fmt", HasData: true,
		Data: protocol.LSPAny([]byte("{\"resolve\":true}")),
		raw:  protocol.CompletionItem{Label: "fmt", Data: protocol.LSPAny([]byte("{\"resolve\":true}"))},
	}
	got, err := f.client.CompletionResolve(ctx, original)
	if err != nil {
		t.Fatalf("CompletionResolve: %v", err)
	}
	sent := nextRequest[*protocol.CompletionItem](t, f.srv, "completionItem/resolve")
	if sent.Label != "fmt" || sent.Data == nil {
		t.Fatalf("server got %+v, want the original label and data", sent)
	}
	if got.InsertText != "fmt" || len(got.AdditionalTextEdits) != 1 || got.AdditionalTextEdits[0].NewText != "import \"fmt\"\n" {
		t.Fatalf("resolved = %+v, want the resolved insert and auto-import", got)
	}
}
