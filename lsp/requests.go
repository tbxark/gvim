package lsp

import (
	"context"
	"encoding/json"
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

// CodeAction is one code action a server offers for a range. Title and Kind are
// what a menu shows. Edit and Command are what applies the action: an edit is a
// workspace edit (text edits across files, plus file create/rename/delete), and a
// command is executed by the server. A code action with neither, but with Data,
// needs ResolveCodeAction first: the server fills the edit or the command in.
// IsCommand marks an action the server sent as a bare protocol.Command, which has
// no kind, no edit and no data.
type CodeAction struct {
	Title     string
	Kind      string
	Preferred bool
	Edit      *protocol.WorkspaceEdit
	Command   *protocol.Command
	// Data is the server's opaque payload for codeAction/resolve; HasData says
	// the server sent one.
	Data    protocol.LSPAny
	HasData bool
	// IsCommand marks a bare command, which can only be executed.
	IsCommand bool

	// raw is the action as the server sent it, which ResolveCodeAction sends
	// back. It is empty for a bare command.
	raw protocol.CodeAction
}

// resolve picks the fields of one server answer.
func (c CodeAction) resolve(raw protocol.CodeAction) CodeAction {
	c.raw = raw
	c.Title = raw.Title
	if raw.Kind != nil {
		c.Kind = string(*raw.Kind)
	}
	if raw.IsPreferred != nil {
		c.Preferred = *raw.IsPreferred
	}
	c.Edit = raw.Edit
	if raw.Command.Command != "" {
		cmd := raw.Command
		c.Command = &cmd
	}
	if len(raw.Data) > 0 && string(raw.Data) != "null" {
		c.Data, c.HasData = raw.Data, true
	}
	return c
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
// returns the workspace edit it answers with, which may touch several files and
// may create, rename or delete files.
func (c *Client) Rename(ctx context.Context, docURI string, pos protocol.Position, newName string) (*protocol.WorkspaceEdit, error) {
	res, err := c.server.Rename(ctx, &protocol.RenameParams{
		TextDocumentPositionParams: positionParams(docURI, pos),
		NewName:                    newName,
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: rename %s: %w", docURI, err)
	}
	return res, nil
}

// CodeActions asks for the code actions available for rng in docURI.
// diagnostics is the stored diagnostics whose ranges overlap rng, which many
// servers (clangd, eslint) require as context before they offer a quick fix; it
// may be nil, and is sent as an empty list because some servers reject null.
func (c *Client) CodeActions(ctx context.Context, docURI string, rng protocol.Range, diagnostics []protocol.Diagnostic) ([]CodeAction, error) {
	if diagnostics == nil {
		diagnostics = []protocol.Diagnostic{}
	}
	res, err := c.server.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		Range:        rng,
		Context:      protocol.CodeActionContext{Diagnostics: diagnostics},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: code action %s: %w", docURI, err)
	}
	out := make([]CodeAction, 0, len(res))
	for _, r := range res {
		switch a := r.(type) {
		case *protocol.CodeAction:
			if a != nil {
				out = append(out, CodeAction{}.resolve(*a))
			}
		case *protocol.Command:
			if a != nil {
				out = append(out, CodeAction{Title: a.Title, Command: a, IsCommand: true})
			}
		}
	}
	return out, nil
}

// ResolveCodeAction completes a code action that needs a resolve request: one
// with data but no edit and no command. An action that does not need resolving is
// returned as it is. The server answers with the edit and/or the command filled
// in, and an action that is a bare command cannot be resolved.
func (c *Client) ResolveCodeAction(ctx context.Context, action CodeAction) (CodeAction, error) {
	if !action.HasData || action.IsCommand {
		return action, nil
	}
	res, err := c.server.CodeActionResolve(ctx, &action.raw)
	if err != nil {
		return CodeAction{}, fmt.Errorf("lsp: code action resolve %q: %w", action.Title, err)
	}
	if res == nil {
		return action, nil
	}
	return CodeAction{}.resolve(*res), nil
}

// ExecuteCommand runs a server command, such as the command of a code action.
// Some commands ask the client to apply an edit while they run, which is answered
// on the read goroutine and queued for the editor; see Config.ApplyEdit.
func (c *Client) ExecuteCommand(ctx context.Context, cmd protocol.Command) error {
	_, err := c.server.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   cmd.Command,
		Arguments: cmd.Arguments,
	})
	if err != nil {
		return fmt.Errorf("lsp: executeCommand %q: %w", cmd.Command, err)
	}
	return nil
}

// InlayHints asks for the inlay hints of rng in docURI.
func (c *Client) InlayHints(ctx context.Context, docURI string, rng protocol.Range) ([]protocol.InlayHint, error) {
	hints, err := c.server.InlayHint(ctx, &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		Range:        rng,
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: inlay hints %s: %w", docURI, err)
	}
	return hints, nil
}

// FoldingRanges asks for the folding ranges of docURI.
func (c *Client) FoldingRanges(ctx context.Context, docURI string) ([]protocol.FoldingRange, error) {
	ranges, err := c.server.FoldingRanges(ctx, &protocol.FoldingRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: folding ranges %s: %w", docURI, err)
	}
	return ranges, nil
}

// SemanticTokensFull asks for the semantic tokens of the whole document.
func (c *Client) SemanticTokensFull(ctx context.Context, docURI string) (*protocol.SemanticTokens, error) {
	tokens, err := c.server.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: semantic tokens %s: %w", docURI, err)
	}
	return tokens, nil
}

// SemanticTokensDelta asks for the edit that turns the tokens of
// previousResultID into the document's current tokens.
func (c *Client) SemanticTokensDelta(ctx context.Context, docURI, previousResultID string) (protocol.SemanticTokensDeltaResult, error) {
	res, err := c.server.SemanticTokensFullDelta(ctx, &protocol.SemanticTokensDeltaParams{
		TextDocument:     protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		PreviousResultID: previousResultID,
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: semantic tokens delta %s: %w", docURI, err)
	}
	return res, nil
}

// SemanticTokensRange asks for the semantic tokens of rng in docURI.
func (c *Client) SemanticTokensRange(ctx context.Context, docURI string, rng protocol.Range) (*protocol.SemanticTokens, error) {
	tokens, err := c.server.SemanticTokensRange(ctx, &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		Range:        rng,
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: semantic tokens range %s: %w", docURI, err)
	}
	return tokens, nil
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

// Formatting asks the server to format the whole document and returns the text edits it
// wants, in the order the server gave them. tabSize and insertSpaces are the editor's
// settings. A server with no formatting answers with a null result, which is no edits.
func (c *Client) Formatting(ctx context.Context, docURI string, tabSize int, insertSpaces bool) ([]protocol.TextEdit, error) {
	edits, err := c.server.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
		Options: protocol.FormattingOptions{
			TabSize:      uint32(max(tabSize, 1)),
			InsertSpaces: insertSpaces,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("lsp: formatting %s: %w", docURI, err)
	}
	return edits, nil
}

// PullDiagnostics asks for the server-pulled diagnostics of docURI
// (textDocument/diagnostic). previousResultID is the result id of the last full
// answer, or "" for a full pull. The server may answer "unchanged" (items is nil
// and Unchanged is true) when it has nothing new for that id.
type PullDiagnosticsResult struct {
	Items     []protocol.Diagnostic
	ResultID  string
	Unchanged bool
}

// PullDiagnostics asks the server for the document's diagnostics. Some servers
// report their problems only this way instead of publishing them.
func (c *Client) PullDiagnostics(ctx context.Context, docURI, previousResultID string) (PullDiagnosticsResult, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": docURI},
	}
	if previousResultID != "" {
		params["previousResultId"] = previousResultID
	}
	raw, err := c.Request(ctx, "textDocument/diagnostic", params)
	if err != nil {
		return PullDiagnosticsResult{}, fmt.Errorf("lsp: pull diagnostics %s: %w", docURI, err)
	}
	var report struct {
		Kind     string                `json:"kind"`
		ResultID string                `json:"resultId"`
		Items    []protocol.Diagnostic `json:"items"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return PullDiagnosticsResult{}, fmt.Errorf("lsp: pull diagnostics %s: bad report: %w", docURI, err)
	}
	return PullDiagnosticsResult{
		Items:     report.Items,
		ResultID:  report.ResultID,
		Unchanged: report.Kind == "unchanged",
	}, nil
}

// WorkspaceDiagnosticItem is one file's diagnostics from a workspace/diagnostic
// pull. Unchanged is set for a report that says the previous result still holds;
// Items is then empty.
type WorkspaceDiagnosticItem struct {
	URI       string
	Items     []protocol.Diagnostic
	Version   *int32
	ResultID  string
	Unchanged bool
}

// WorkspaceDiagnostics asks for the whole workspace's diagnostics
// (workspace/diagnostic), including files the editor has not opened. previous
// maps a document URI to the result id of its last report; the server may answer
// "unchanged" for those. A server that does not advertise workspace diagnostics
// answers with an error, so the caller can fall back to the per-document sets.
func (c *Client) WorkspaceDiagnostics(ctx context.Context, previous map[string]string) ([]WorkspaceDiagnosticItem, error) {
	ids := make([]map[string]string, 0, len(previous))
	for docURI, value := range previous {
		ids = append(ids, map[string]string{"uri": docURI, "value": value})
	}
	params := map[string]any{"previousResultIds": ids}
	raw, err := c.Request(ctx, "workspace/diagnostic", params)
	if err != nil {
		return nil, fmt.Errorf("lsp: workspace diagnostics: %w", err)
	}
	var report struct {
		Items []struct {
			URI      string                `json:"uri"`
			Version  *int32                `json:"version"`
			Kind     string                `json:"kind"`
			ResultID string                `json:"resultId"`
			Items    []protocol.Diagnostic `json:"items"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("lsp: workspace diagnostics: bad report: %w", err)
	}
	out := make([]WorkspaceDiagnosticItem, 0, len(report.Items))
	for _, item := range report.Items {
		out = append(out, WorkspaceDiagnosticItem{
			URI:       item.URI,
			Items:     item.Items,
			Version:   item.Version,
			ResultID:  item.ResultID,
			Unchanged: item.Kind == "unchanged",
		})
	}
	return out, nil
}
