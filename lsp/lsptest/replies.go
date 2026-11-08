package lsptest

import (
	"context"

	"go.lsp.dev/protocol"
)

// Each handler below records its params, parks when the test asked for it, and
// then either returns ErrRefused or the matching canned reply.

// Definition records the params and answers with Canned.Definition.
func (s *FakeServer) Definition(ctx context.Context, p *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Definition, nil
}

// References records the params and answers with Canned.References.
func (s *FakeServer) References(ctx context.Context, p *protocol.ReferenceParams) ([]protocol.Location, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.References, nil
}

// Hover records the params and answers with Canned.Hover.
func (s *FakeServer) Hover(ctx context.Context, p *protocol.HoverParams) (*protocol.Hover, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Hover, nil
}

// Completion records the params and answers with Canned.Completion.
func (s *FakeServer) Completion(ctx context.Context, p *protocol.CompletionParams) (protocol.CompletionResult, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Completion, nil
}

// Rename records the params and answers with Canned.Rename.
func (s *FakeServer) Rename(ctx context.Context, p *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Rename, nil
}

// CodeAction records the params and answers with Canned.CodeActions.
func (s *FakeServer) CodeAction(ctx context.Context, p *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.CodeActions, nil
}

// Implementation records the params and answers with Canned.Implementation.
func (s *FakeServer) Implementation(ctx context.Context, p *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Implementation, nil
}

// SignatureHelp records the params and answers with Canned.Signature.
func (s *FakeServer) SignatureHelp(ctx context.Context, p *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Signature, nil
}

// Formatting records the params and answers with Canned.Formatting.
func (s *FakeServer) Formatting(ctx context.Context, p *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.Formatting, nil
}

// CodeActionResolve records the params and answers with Canned.ResolvedCodeAction.
func (s *FakeServer) CodeActionResolve(ctx context.Context, p *protocol.CodeAction) (*protocol.CodeAction, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.ResolvedCodeAction, nil
}

// ExecuteCommand records the params, runs Canned.OnExecuteCommand, and answers
// with Canned.ExecuteCommandResult.
func (s *FakeServer) ExecuteCommand(ctx context.Context, p *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	RecordTo[any](s.Requests, p)
	if s.Canned.OnExecuteCommand != nil {
		s.Canned.OnExecuteCommand(ctx, p)
	}
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.ExecuteCommandResult, nil
}

// Diagnostic records the params and answers with the canned server-pulled
// diagnostics: a full report, or "unchanged" when Canned.PullUnchanged is set.
func (s *FakeServer) Diagnostic(ctx context.Context, p *protocol.DocumentDiagnosticParams) (protocol.DocumentDiagnosticReport, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	resultID := s.Canned.PullResultID
	if s.Canned.PullUnchanged {
		return &protocol.RelatedUnchangedDocumentDiagnosticReport{
			UnchangedDocumentDiagnosticReport: protocol.UnchangedDocumentDiagnosticReport{
				Kind:     string(protocol.DocumentDiagnosticReportKindUnchanged),
				ResultID: resultID,
			},
		}, nil
	}
	report := &protocol.RelatedFullDocumentDiagnosticReport{
		FullDocumentDiagnosticReport: protocol.FullDocumentDiagnosticReport{
			Kind:  string(protocol.DocumentDiagnosticReportKindFull),
			Items: s.Canned.PullItems,
		},
	}
	if resultID != "" {
		report.ResultID = &resultID
	}
	return report, nil
}

// InlayHint records the params and answers with Canned.InlayHints.
func (s *FakeServer) InlayHint(ctx context.Context, p *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.InlayHints, nil
}

// SemanticTokensFull records the params and answers with Canned.SemanticTokens.
func (s *FakeServer) SemanticTokensFull(ctx context.Context, p *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.SemanticTokens, nil
}

// SemanticTokensFullDelta records the params and answers with
// Canned.SemanticTokensDelta.
func (s *FakeServer) SemanticTokensFullDelta(ctx context.Context, p *protocol.SemanticTokensDeltaParams) (protocol.SemanticTokensDeltaResult, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.SemanticTokensDelta, nil
}

// SemanticTokensRange records the params and answers with
// Canned.SemanticTokensRange.
func (s *FakeServer) SemanticTokensRange(ctx context.Context, p *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.SemanticTokensRange, nil
}

// FoldingRanges records the params and answers with Canned.FoldingRanges.
func (s *FakeServer) FoldingRanges(ctx context.Context, p *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	RecordTo[any](s.Requests, p)
	s.park(ctx)
	if s.Canned.Fail {
		return nil, ErrRefused
	}
	return s.Canned.FoldingRanges, nil
}
