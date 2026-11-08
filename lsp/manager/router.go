package manager

import (
	"github.com/tbxark/gvim/lsp"
)

// Feature is one language-server feature. Features are routed to servers by
// language, feature filter and advertised capability.
type Feature int

// The features the editor routes. The set and the merged/exclusive split are
// Fresh's (docs/internal/lsp.md section 2.2).
const (
	FeatureDiagnostics Feature = iota
	FeatureCompletion
	FeatureCodeAction
	FeatureDocumentSymbols
	FeatureWorkspaceSymbols

	FeatureHover
	FeatureDefinition
	FeatureImplementation
	FeatureReferences
	FeatureFormat
	FeatureRename
	FeatureSignatureHelp
	FeatureInlayHints
	FeatureFoldingRange
	FeatureSemanticTokens
	FeatureDocumentHighlight
)

// String returns the feature's name as Fresh's config spells it.
func (f Feature) String() string {
	switch f {
	case FeatureDiagnostics:
		return "diagnostics"
	case FeatureCompletion:
		return "completion"
	case FeatureCodeAction:
		return "code_action"
	case FeatureDocumentSymbols:
		return "document_symbols"
	case FeatureWorkspaceSymbols:
		return "workspace_symbols"
	case FeatureHover:
		return "hover"
	case FeatureDefinition:
		return "definition"
	case FeatureImplementation:
		return "implementation"
	case FeatureReferences:
		return "references"
	case FeatureFormat:
		return "format"
	case FeatureRename:
		return "rename"
	case FeatureSignatureHelp:
		return "signature_help"
	case FeatureInlayHints:
		return "inlay_hints"
	case FeatureFoldingRange:
		return "folding_range"
	case FeatureSemanticTokens:
		return "semantic_tokens"
	case FeatureDocumentHighlight:
		return "document_highlight"
	}
	return "feature"
}

// Merged reports whether the feature merges results from every eligible server.
// Merged features are diagnostics, completion, code actions and the symbols;
// every other feature is exclusive: the first eligible server answers, and an
// empty answer from it is final (there is no fallback to the next server).
func (f Feature) Merged() bool {
	switch f {
	case FeatureDiagnostics, FeatureCompletion, FeatureCodeAction,
		FeatureDocumentSymbols, FeatureWorkspaceSymbols:
		return true
	}
	return false
}

// ParseFeatureFilter builds a server's routing filter from the only_features and
// except_features names of its config. A name this package does not know is
// ignored: the config loader has already reported it.
func ParseFeatureFilter(only, except []string) FeatureFilter {
	if len(only) > 0 {
		return FeatureFilter{only: featureSet(only)}
	}
	if len(except) > 0 {
		return FeatureFilter{except: featureSet(except)}
	}
	return FeatureFilter{}
}

// featureSet converts names to features, dropping unknown names.
func featureSet(names []string) map[Feature]bool {
	out := make(map[Feature]bool, len(names))
	for _, name := range names {
		if f, ok := featureByName(name); ok {
			out[f] = true
		}
	}
	return out
}

// featureByName returns the feature a config name stands for.
func featureByName(name string) (Feature, bool) {
	for f := FeatureDiagnostics; f <= FeatureDocumentHighlight; f++ {
		if f.String() == name {
			return f, true
		}
	}
	return 0, false
}

// FeatureFilter is a server's only/except routing filter: the set of features it
// takes, or the set it does not. The zero value allows every feature.
type FeatureFilter struct {
	only   map[Feature]bool
	except map[Feature]bool
}

// Allows reports whether the filter lets feature through. The zero value allows
// everything.
func (f FeatureFilter) Allows(feature Feature) bool {
	switch {
	case f.only != nil:
		return f.only[feature]
	case f.except != nil:
		return !f.except[feature]
	}
	return true
}

// Capability reports whether a server's initialize capabilities advertise feature.
// A client whose initialize has not finished advertises nothing, which keeps a
// request off a server of unknown capabilities (the gate-and-retry model).
func Capability(c lsp.Capabilities, f Feature) bool {
	if !c.Initialized {
		return false
	}
	switch f {
	case FeatureDiagnostics:
		return c.Diagnostics
	case FeatureCompletion:
		return c.Completion
	case FeatureCodeAction:
		return c.CodeAction
	case FeatureDocumentSymbols:
		return c.DocumentSymbols
	case FeatureWorkspaceSymbols:
		return c.WorkspaceSymbols
	case FeatureHover:
		return c.Hover
	case FeatureDefinition:
		return c.Definition
	case FeatureImplementation:
		return c.Implementation
	case FeatureReferences:
		return c.References
	case FeatureFormat:
		return c.Format
	case FeatureRename:
		return c.Rename
	case FeatureSignatureHelp:
		return c.SignatureHelp
	case FeatureInlayHints:
		return c.InlayHints
	case FeatureFoldingRange:
		return c.FoldingRanges
	case FeatureSemanticTokens:
		return c.SemanticTokens
	case FeatureDocumentHighlight:
		return c.DocumentHighlight
	}
	return false
}
