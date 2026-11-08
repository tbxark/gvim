package lsp

import (
	"encoding/json"

	"go.lsp.dev/protocol"
)

// Capabilities is the subset of a server's initialize capabilities that the
// editor routes features on, plus the providers the server registered later with
// client/registerCapability. The zero value advertises nothing: a handle is
// invisible to routing until Initialize returns, which is the gate that keeps a
// request off a server whose capabilities are unknown.
type Capabilities struct {
	// Initialized is set once the initialize handshake has completed.
	Initialized bool

	// Merged features: results combine across every eligible server.
	Diagnostics      bool
	Completion       bool
	CodeAction       bool
	DocumentSymbols  bool
	WorkspaceSymbols bool

	// Exclusive features: the first eligible server in handle order serves them.
	Hover             bool
	Definition        bool
	Implementation    bool
	References        bool
	Format            bool
	Rename            bool
	SignatureHelp     bool
	InlayHints        bool
	FoldingRanges     bool
	SemanticTokens    bool
	DocumentHighlight bool

	// CodeActionResolve is the server's codeActionProvider.resolveProvider: an
	// action with data but no edit and no command needs a resolve request first.
	CodeActionResolve bool

	// SemanticTokensFull, SemanticTokensDelta and SemanticTokensRange say what the
	// server's semanticTokensProvider offers.
	SemanticTokensFull  bool
	SemanticTokensDelta bool
	SemanticTokensRange bool
	// SemanticTokensLegend decodes the token types and modifiers the server sends.
	SemanticTokensLegend *protocol.SemanticTokensLegend
}

// summarizeCapabilities reads the capabilities of an initialize result into the
// routing snapshot. A server publishes diagnostics unless it says otherwise, so
// Diagnostics starts on: every language server may send publishDiagnostics.
func summarizeCapabilities(c protocol.ServerCapabilities) Capabilities {
	caps := Capabilities{
		Initialized:       true,
		Diagnostics:       true,
		Completion:        c.CompletionProvider != nil,
		SignatureHelp:     c.SignatureHelpProvider != nil,
		Hover:             providerEnabled(c.HoverProvider),
		Definition:        providerEnabled(c.DefinitionProvider),
		Implementation:    providerEnabled(c.ImplementationProvider),
		References:        providerEnabled(c.ReferencesProvider),
		Format:            providerEnabled(c.DocumentFormattingProvider) || providerEnabled(c.DocumentRangeFormattingProvider),
		Rename:            providerEnabled(c.RenameProvider),
		DocumentSymbols:   providerEnabled(c.DocumentSymbolProvider),
		WorkspaceSymbols:  providerEnabled(c.WorkspaceSymbolProvider),
		FoldingRanges:     providerEnabled(c.FoldingRangeProvider),
		InlayHints:        providerEnabled(c.InlayHintProvider),
		DocumentHighlight: providerEnabled(c.DocumentHighlightProvider),
	}
	switch p := c.CodeActionProvider.(type) {
	case *protocol.CodeActionOptions:
		caps.CodeAction = p != nil
		if p != nil && p.ResolveProvider != nil {
			caps.CodeActionResolve = *p.ResolveProvider
		}
	default:
		caps.CodeAction = providerEnabled(c.CodeActionProvider)
	}
	switch p := c.SemanticTokensProvider.(type) {
	case *protocol.SemanticTokensOptions:
		if p != nil {
			caps.SemanticTokens = true
			caps.SemanticTokensLegend = &p.Legend
			caps.SemanticTokensFull, caps.SemanticTokensDelta = semanticTokensFull(p.Full)
			caps.SemanticTokensRange = providerEnabled(p.Range)
		}
	case *protocol.SemanticTokensRegistrationOptions:
		if p != nil {
			caps.SemanticTokens = true
			caps.SemanticTokensLegend = &p.Legend
			caps.SemanticTokensFull, caps.SemanticTokensDelta = semanticTokensFull(p.Full)
			caps.SemanticTokensRange = providerEnabled(p.Range)
		}
	}
	if caps.SemanticTokens {
		// A provider that gives only a delta request still needs the full one to
		// have something to apply the delta to, so full is on whenever delta is.
		caps.SemanticTokensFull = caps.SemanticTokensFull || caps.SemanticTokensDelta
	}
	return caps
}

// providerEnabled reports whether a server capability provider is on. The LSP
// models each provider as true, false, or an options object; a missing field
// decodes to nil, and any options object (of whichever type the capability has)
// means "on".
func providerEnabled(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case protocol.Boolean:
		return bool(x)
	}
	return true
}

// semanticTokensFull reads the semanticTokensProvider.full options.
func semanticTokensFull(f protocol.SemanticTokensOptionsFull) (full, delta bool) {
	switch x := f.(type) {
	case protocol.Boolean:
		return bool(x), false
	case *protocol.SemanticTokensFullDelta:
		if x != nil && x.Delta != nil && *x.Delta {
			return true, true
		}
		return true, false
	}
	return false, false
}

// applyRegistration turns on the capabilities a dynamically registered provider
// serves. It runs on the connection's read goroutine, before the registration
// request is answered, so a server sees its registration in force before it can
// send the first notification for it.
func applyRegistration(caps *Capabilities, method string, options protocol.LSPAny) {
	switch method {
	case "textDocument/publishDiagnostics", "textDocument/diagnostic", "workspace/diagnostic":
		caps.Diagnostics = true
	case "textDocument/completion":
		caps.Completion = true
	case "textDocument/codeAction":
		caps.CodeAction = true
		if resolveProvider(options) {
			caps.CodeActionResolve = true
		}
	case "textDocument/documentSymbol":
		caps.DocumentSymbols = true
	case "workspace/symbol":
		caps.WorkspaceSymbols = true
	case "textDocument/hover":
		caps.Hover = true
	case "textDocument/definition", "textDocument/declaration", "textDocument/typeDefinition":
		caps.Definition = true
	case "textDocument/implementation":
		caps.Implementation = true
	case "textDocument/references":
		caps.References = true
	case "textDocument/formatting", "textDocument/rangeFormatting", "textDocument/onTypeFormatting":
		caps.Format = true
	case "textDocument/rename":
		caps.Rename = true
	case "textDocument/signatureHelp":
		caps.SignatureHelp = true
	case "textDocument/inlayHint":
		caps.InlayHints = true
	case "textDocument/foldingRange":
		caps.FoldingRanges = true
	case "textDocument/documentHighlight":
		caps.DocumentHighlight = true
	case "textDocument/semanticTokens", "textDocument/semanticTokens/full":
		caps.SemanticTokens, caps.SemanticTokensFull = true, true
	case "textDocument/semanticTokens/full/delta":
		caps.SemanticTokens, caps.SemanticTokensFull, caps.SemanticTokensDelta = true, true, true
	case "textDocument/semanticTokens/range":
		caps.SemanticTokens, caps.SemanticTokensRange = true, true
	}
}

// resolveProvider reports whether a registration's options say the provider
// resolves in a second request (the codeAction resolveProvider).
func resolveProvider(options protocol.LSPAny) bool {
	if len(options) == 0 {
		return false
	}
	var opts struct {
		ResolveProvider *bool `json:"resolveProvider"`
	}
	if json.Unmarshal(options, &opts) != nil {
		return false
	}
	return opts.ResolveProvider != nil && *opts.ResolveProvider
}
