package lsp

import (
	"context"
	"fmt"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// CompletionItem is one completion candidate, reduced to what an editor shows
// and inserts. InsertText is the text the server wants inserted: the textEdit
// text if the item has one, else insertText, else the label.
//
// EditRange is the document range that InsertText replaces. It is nil when the
// item has no text edit, and then InsertText goes at the cursor. For an
// InsertReplaceEdit it is the insert range. ReplaceRange is the replace range of
// an InsertReplaceEdit, and nil otherwise.
type CompletionItem struct {
	Label        string
	Detail       string
	InsertText   string
	EditRange    *protocol.Range
	ReplaceRange *protocol.Range
}

// CodeAction is one code action offered for a range. Kind is empty for a bare
// command, which carries no kind.
type CodeAction struct {
	Title string
	Kind  string
}

// Definition asks for the definition of the symbol at pos in docURI. A
// LocationLink result is reduced to its target selection range.
func (c *Client) Definition(ctx context.Context, docURI string, pos protocol.Position) ([]protocol.Location, error) {
	res, err := c.server.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: definition %s: %w", docURI, err)
	}
	return locations(res), nil
}

// Implementation asks for the implementations of the symbol at pos in docURI.
// Results are reduced as in Definition.
func (c *Client) Implementation(ctx context.Context, docURI string, pos protocol.Position) ([]protocol.Location, error) {
	res, err := c.server.Implementation(ctx, &protocol.ImplementationParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: implementation %s: %w", docURI, err)
	}
	return locations(res), nil
}

// References asks for the references to the symbol at pos in docURI. Set
// includeDeclaration to include the declaration itself.
func (c *Client) References(ctx context.Context, docURI string, pos protocol.Position, includeDeclaration bool) ([]protocol.Location, error) {
	res, err := c.server.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
		Context:                    protocol.ReferenceContext{IncludeDeclaration: includeDeclaration},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: references %s: %w", docURI, err)
	}
	return res, nil
}

// Hover returns the hover text at pos in docURI, or "" when the server has none.
func (c *Client) Hover(ctx context.Context, docURI string, pos protocol.Position) (string, error) {
	res, err := c.server.Hover(ctx, &protocol.HoverParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
	})
	if err != nil {
		return "", fmt.Errorf("lsp: hover %s: %w", docURI, err)
	}
	if res == nil {
		return "", nil
	}
	return hoverText(res.Contents), nil
}

// Completion returns the completion candidates at pos in docURI. An incomplete
// list is returned as the server sent it, without a follow-up request.
func (c *Client) Completion(ctx context.Context, docURI string, pos protocol.Position) ([]CompletionItem, error) {
	res, err := c.server.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: completion %s: %w", docURI, err)
	}
	var items []protocol.CompletionItem
	switch v := res.(type) {
	case protocol.CompletionItemSlice:
		items = v
	case *protocol.CompletionList:
		if v != nil {
			items = v.Items
		}
	}
	out := make([]CompletionItem, 0, len(items))
	for _, it := range items {
		out = append(out, completionItem(it))
	}
	return out, nil
}

// Rename asks the server to rename the symbol at pos in docURI to newName, and
// returns the text edits it wants, keyed by document URI. A workspace edit that
// creates, renames or deletes files is an error, because those cannot be
// expressed as text edits.
func (c *Client) Rename(ctx context.Context, docURI string, pos protocol.Position, newName string) (map[string][]protocol.TextEdit, error) {
	res, err := c.server.Rename(ctx, &protocol.RenameParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
		NewName:                    newName,
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: rename %s: %w", docURI, err)
	}
	if res == nil {
		return nil, nil
	}
	edits := make(map[string][]protocol.TextEdit)
	if len(res.DocumentChanges) > 0 {
		// The LSP spec says documentChanges takes precedence over changes.
		for _, dc := range res.DocumentChanges {
			tde, ok := dc.(*protocol.TextDocumentEdit)
			if !ok {
				return nil, fmt.Errorf("lsp: rename %s: unsupported document change %T", docURI, dc)
			}
			u := string(tde.TextDocument.URI)
			for _, el := range tde.Edits {
				te, err := plainTextEdit(el)
				if err != nil {
					return nil, fmt.Errorf("lsp: rename %s: %w", docURI, err)
				}
				edits[u] = append(edits[u], te)
			}
		}
		return edits, nil
	}
	for u, es := range res.Changes {
		edits[string(u)] = append(edits[string(u)], es...)
	}
	return edits, nil
}

// CodeActions asks for the code actions available for rng in docURI. The
// request sends no diagnostics as context, so a server that offers fixes only
// for a diagnostic returns fewer actions.
func (c *Client) CodeActions(ctx context.Context, docURI string, rng protocol.Range) ([]CodeAction, error) {
	res, err := c.server.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		Range:        rng,
		// An empty list, not null: some servers reject a null diagnostics array.
		Context: protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{}},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: code action %s: %w", docURI, err)
	}
	out := make([]CodeAction, 0, len(res))
	for _, r := range res {
		switch a := r.(type) {
		case *protocol.CodeAction:
			ca := CodeAction{Title: a.Title}
			if a.Kind != nil {
				ca.Kind = string(*a.Kind)
			}
			out = append(out, ca)
		case *protocol.Command:
			out = append(out, CodeAction{Title: a.Title})
		}
	}
	return out, nil
}

// positionParams builds the document and position part shared by most requests.
func positionParams(docURI string, pos protocol.Position) protocol.TextDocumentPositionParams {
	return protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		Position:     pos,
	}
}

// locations reduces a definition-style result to plain locations. A LocationLink
// contributes its target selection range, the span the server wants selected.
func locations(res protocol.DefinitionResult) []protocol.Location {
	switch v := res.(type) {
	case *protocol.Location:
		if v == nil {
			return nil
		}
		return []protocol.Location{*v}
	case protocol.LocationSlice:
		return []protocol.Location(v)
	case protocol.DefinitionLinkSlice:
		out := make([]protocol.Location, 0, len(v))
		for _, l := range v {
			out = append(out, protocol.Location{URI: l.TargetURI, Range: l.TargetSelectionRange})
		}
		return out
	}
	return nil
}

// completionItem reduces one protocol item. The text edit takes precedence over
// insertText, which takes precedence over the label, as the LSP spec says. The
// text edit's range is kept, so an editor can replace the text it covers instead
// of inserting beside it.
func completionItem(it protocol.CompletionItem) CompletionItem {
	detail, _ := it.Detail.Get()
	out := CompletionItem{Label: it.Label, Detail: detail, InsertText: it.Label}
	if s, ok := it.InsertText.Get(); ok {
		out.InsertText = s
	}
	switch te := it.TextEdit.(type) {
	case *protocol.TextEdit:
		if te != nil {
			out.InsertText = te.NewText
			out.EditRange = &te.Range
		}
	case *protocol.InsertReplaceEdit:
		if te != nil {
			out.InsertText = te.NewText
			out.EditRange = &te.Insert
			out.ReplaceRange = &te.Replace
		}
	}
	return out
}

// plainTextEdit returns the text edit inside a document change element. A
// snippet edit is an error: inserting its text raw would leave the $ placeholders
// in the document.
func plainTextEdit(el protocol.TextDocumentEditElement) (protocol.TextEdit, error) {
	switch e := el.(type) {
	case *protocol.TextEdit:
		if e != nil {
			return *e, nil
		}
	case *protocol.AnnotatedTextEdit:
		if e != nil {
			return e.TextEdit, nil
		}
	}
	return protocol.TextEdit{}, fmt.Errorf("unsupported text edit %T", el)
}

// hoverText reduces hover contents to plain text. Markdown code fence lines are
// dropped, so a code block reads as its lines. Plaintext is returned as written.
func hoverText(contents protocol.HoverContents) string {
	switch v := contents.(type) {
	case protocol.String:
		return stripFences(string(v))
	case *protocol.MarkupContent:
		if v == nil {
			return ""
		}
		if v.Kind == protocol.MarkupKindMarkdown {
			return stripFences(v.Value)
		}
		return v.Value
	case *protocol.MarkedStringWithLanguage: //nolint:staticcheck // deprecated, but older servers still send it
		if v == nil {
			return ""
		}
		return v.Value
	case protocol.MarkedStringSlice:
		parts := make([]string, 0, len(v))
		for _, m := range v {
			parts = append(parts, markedStringText(m))
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// markedStringText returns the text of one MarkedString, which is deprecated but
// still sent by older servers.
func markedStringText(m protocol.MarkedString) string { //nolint:staticcheck // deprecated, but older servers still send it
	switch v := m.(type) {
	case protocol.String:
		return stripFences(string(v))
	case *protocol.MarkedStringWithLanguage: //nolint:staticcheck // deprecated, but older servers still send it
		if v != nil {
			return v.Value
		}
	}
	return ""
}

// stripFences removes markdown code fence lines and the blank lines around the
// text. Code inside the fences keeps its indentation.
func stripFences(md string) string {
	var kept []string
	for line := range strings.SplitSeq(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Trim(strings.Join(kept, "\n"), "\n")
}
