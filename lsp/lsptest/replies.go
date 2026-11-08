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
