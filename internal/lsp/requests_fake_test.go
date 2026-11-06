package lsp

import (
	"context"
	"errors"

	"go.lsp.dev/protocol"
)

// errRefused is the error the fake server returns from every request method
// while canned.fail is set. Tests check that it reaches the caller.
var errRefused = errors.New("fake server refused")

// cannedReplies is what the fake server answers to each request method. A test
// sets the fields it needs before it sends the request. Unset fields reply with
// null or an empty list.
type cannedReplies struct {
	fail           bool
	definition     protocol.DefinitionResult
	references     []protocol.Location
	hover          *protocol.Hover
	signature      *protocol.SignatureHelp
	completion     protocol.CompletionResult
	rename         *protocol.WorkspaceEdit
	codeActions    []protocol.CommandOrCodeAction
	implementation protocol.DefinitionResult
}

// Each handler below records its params, parks when the test asked for it, and
// then either returns errRefused or the matching canned reply.

func (s *fakeServer) Definition(ctx context.Context, p *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.definition, nil
}

func (s *fakeServer) References(ctx context.Context, p *protocol.ReferenceParams) ([]protocol.Location, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.references, nil
}

func (s *fakeServer) Hover(ctx context.Context, p *protocol.HoverParams) (*protocol.Hover, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.hover, nil
}

func (s *fakeServer) Completion(ctx context.Context, p *protocol.CompletionParams) (protocol.CompletionResult, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.completion, nil
}

func (s *fakeServer) Rename(ctx context.Context, p *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.rename, nil
}

func (s *fakeServer) CodeAction(ctx context.Context, p *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.codeActions, nil
}

func (s *fakeServer) Implementation(ctx context.Context, p *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.implementation, nil
}

func (s *fakeServer) SignatureHelp(ctx context.Context, p *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	recordTo[any](s.requests, p)
	s.park(ctx)
	if s.canned.fail {
		return nil, errRefused
	}
	return s.canned.signature, nil
}
