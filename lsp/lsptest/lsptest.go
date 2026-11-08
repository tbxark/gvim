// Package lsptest is the fake language server the lsp tests drive: an in-memory
// server over io.Pipe pairs, the client and server ends of those pipes, and the
// waits and replies shared by the client, request and manager tests.
//
// It imports no package of this module, so the white-box tests of package lsp can
// import it: a client, or a Manager's start hook, is built by the test itself with
// lsp.NewClient and StartServer.
package lsptest

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Timeout bounds every wait in the tests that use this package, so a hang fails
// instead of blocking the run.
const Timeout = 5 * time.Second

// PipeRWC joins an io.PipeReader and an io.PipeWriter into one
// io.ReadWriteCloser.
type PipeRWC struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p PipeRWC) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p PipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }

// Close closes both directions. Closing the writer makes the peer's reads return
// io.EOF, and closing the reader makes the peer's writes fail.
func (p PipeRWC) Close() error {
	_ = p.r.Close()
	return p.w.Close()
}

// NewPipePair returns the client and server ends of two in-memory pipes.
func NewPipePair() (client, server PipeRWC) {
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	return PipeRWC{r: cr, w: cw}, PipeRWC{r: sr, w: sw}
}

// RecordTo sends v without blocking. A full channel drops the value instead of
// blocking the server's read loop.
func RecordTo[T any](ch chan T, v T) {
	select {
	case ch <- v:
	default:
	}
}

// Recv waits for one value on ch or fails the test after Timeout.
func Recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed", what)
		}
		return v
	case <-time.After(Timeout):
		t.Fatalf("%s: timed out after %v", what, Timeout)
	}
	var zero T
	return zero
}

// ErrRefused is the error the fake server returns from every request method
// while Canned.Fail is set. Tests check that it reaches the caller.
var ErrRefused = errors.New("fake server refused")

// CannedReplies is what the fake server answers to each request method. A test
// sets the fields it needs before it sends the request. Unset fields reply with
// null or an empty list.
type CannedReplies struct {
	Fail           bool
	Definition     protocol.DefinitionResult
	References     []protocol.Location
	Hover          *protocol.Hover
	Signature      *protocol.SignatureHelp
	Completion     protocol.CompletionResult
	Rename         *protocol.WorkspaceEdit
	CodeActions    []protocol.CommandOrCodeAction
	Implementation protocol.DefinitionResult
	Formatting     []protocol.TextEdit

	// Caps is what Initialize answers. A zero value reports full document sync
	// and nothing else.
	Caps protocol.ServerCapabilities

	// WorkspaceItems is what a workspace/diagnostic pull answers.
	WorkspaceItems []protocol.WorkspaceDocumentDiagnosticReport

	// PullItems and PullResultID are what a textDocument/diagnostic pull answers.
	// PullUnchanged makes it answer "unchanged" instead of a full report.
	PullItems     []protocol.Diagnostic
	PullResultID  string
	PullUnchanged bool

	// The answers of the requests a code action pipeline sends.
	ResolvedCodeAction   *protocol.CodeAction
	ExecuteCommandResult protocol.LSPAny
	InlayHints           []protocol.InlayHint
	SemanticTokens       *protocol.SemanticTokens
	SemanticTokensDelta  protocol.SemanticTokensDeltaResult
	SemanticTokensRange  *protocol.SemanticTokens
	FoldingRanges        []protocol.FoldingRange
	// OnExecuteCommand, when set, runs inside the executeCommand handler before
	// it answers. It may send a request back to the client (after jsonrpc2.Async)
	// to test the nested applyEdit flow.
	OnExecuteCommand func(context.Context, *protocol.ExecuteCommandParams)
}

// FakeServer answers the requests the client under test sends, and records each
// notification it receives. It does not emit log output.
type FakeServer struct {
	protocol.UnimplementedServer

	// InitCh, OpenCh, ChangeCh and CloseCh receive the params of the matching
	// notification, in arrival order.
	InitCh   chan *protocol.InitializeParams
	OpenCh   chan *protocol.DidOpenTextDocumentParams
	ChangeCh chan *protocol.DidChangeTextDocumentParams
	CloseCh  chan *protocol.DidCloseTextDocumentParams

	// Requests receives the params of each request method, in arrival order.
	// Canned holds their replies.
	Requests chan any
	Canned   CannedReplies

	// StallEntered signals that a handler parked on the stall channel.
	StallEntered chan struct{}

	// client sends publishDiagnostics back to the client under test.
	client protocol.Client

	// stall, when non-nil, makes Shutdown and the request handlers that call park
	// block until it is closed. Each one signals StallEntered first.
	stall chan struct{}
}

// StartServer builds a FakeServer on srvEnd and starts its read loop. stall is
// the channel that parks handlers (nil: none). The caller closes the returned
// connection and srvEnd.
func StartServer(srvEnd PipeRWC, stall chan struct{}) (*FakeServer, jsonrpc2.Conn) {
	srv := &FakeServer{
		InitCh:       make(chan *protocol.InitializeParams, 128),
		OpenCh:       make(chan *protocol.DidOpenTextDocumentParams, 128),
		ChangeCh:     make(chan *protocol.DidChangeTextDocumentParams, 128),
		CloseCh:      make(chan *protocol.DidCloseTextDocumentParams, 128),
		Requests:     make(chan any, 128),
		StallEntered: make(chan struct{}, 1),
		stall:        stall,
	}
	srvConn := jsonrpc2.NewConn(jsonrpc2.NewHeaderStream(srvEnd), jsonrpc2.WithCodec(codec{}))
	srv.client = protocol.ClientDispatcher(srvConn)
	srvConn.Go(context.Background(), protocol.ServerHandler(srv, jsonrpc2.MethodNotFoundHandler))
	return srv, srvConn
}

// park blocks the calling handler until stall is closed. It does nothing when
// stall is nil. The handler is made asynchronous first, so the read loop keeps
// running and the client's $/cancelRequest write does not block behind it.
func (s *FakeServer) park(ctx context.Context) {
	if s.stall == nil {
		return
	}
	jsonrpc2.Async(ctx)
	RecordTo(s.StallEntered, struct{}{})
	<-s.stall
}

// Initialize records the params and reports the canned capabilities, or full
// document sync and nothing else when none are set.
func (s *FakeServer) Initialize(_ context.Context, p *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	RecordTo(s.InitCh, p)
	caps := s.Canned.Caps
	if caps.TextDocumentSync == nil {
		caps.TextDocumentSync = protocol.TextDocumentSyncKindFull
	}
	return &protocol.InitializeResult{Capabilities: caps}, nil
}

// ApplyEdit sends a workspace/applyEdit request to the client under test and
// returns its answer. The caller must run on a goroutine that may block, such as
// inside an OnExecuteCommand hook started with jsonrpc2.Async.
func (s *FakeServer) ApplyEdit(ctx context.Context, params *protocol.ApplyWorkspaceEditParams) (*protocol.ApplyWorkspaceEditResult, error) {
	return s.client.ApplyEdit(ctx, params)
}

// RegisterCapability sends client/registerCapability to the client under test.
func (s *FakeServer) RegisterCapability(ctx context.Context, params *protocol.RegistrationParams) error {
	return s.client.RegisterCapability(ctx, params)
}

// UnregisterCapability sends client/unregisterCapability to the client under test.
func (s *FakeServer) UnregisterCapability(ctx context.Context, params *protocol.UnregistrationParams) error {
	return s.client.UnregisterCapability(ctx, params)
}

// Configuration sends workspace/configuration to the client under test.
func (s *FakeServer) Configuration(ctx context.Context, params *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	return s.client.Configuration(ctx, params)
}

// Shutdown parks when the test asked for it, then succeeds.
func (s *FakeServer) Shutdown(ctx context.Context) error {
	s.park(ctx)
	return nil
}

// DidOpen records didOpen and publishes diagnostics for it.
func (s *FakeServer) DidOpen(_ context.Context, p *protocol.DidOpenTextDocumentParams) error {
	RecordTo(s.OpenCh, p)
	return s.publish(p.TextDocument.URI, p.TextDocument.Version, p.TextDocument.Text)
}

// DidChange records didChange and publishes diagnostics for the new text.
func (s *FakeServer) DidChange(_ context.Context, p *protocol.DidChangeTextDocumentParams) error {
	RecordTo(s.ChangeCh, p)
	text := ""
	for _, ch := range p.ContentChanges {
		if whole, ok := ch.(*protocol.TextDocumentContentChangeWholeDocument); ok {
			text = whole.Text
		}
	}
	return s.publish(p.TextDocument.URI, p.TextDocument.Version, text)
}

// DidClose records didClose.
func (s *FakeServer) DidClose(_ context.Context, p *protocol.DidCloseTextDocumentParams) error {
	RecordTo(s.CloseCh, p)
	return nil
}

// publish reports one diagnostic per line that contains "ERR".
func (s *FakeServer) publish(u uri.URI, version int32, text string) error {
	diags := make([]protocol.Diagnostic, 0)
	for i, line := range strings.Split(text, "\n") {
		if idx := strings.Index(line, "ERR"); idx >= 0 {
			diags = append(diags, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(i), Character: uint32(idx)},
					End:   protocol.Position{Line: uint32(i), Character: uint32(idx + 3)},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  protocol.String("found ERR"),
			})
		}
	}
	return s.client.PublishDiagnostics(context.Background(), &protocol.PublishDiagnosticsParams{
		URI:         u,
		Version:     protocol.NewOptional(version),
		Diagnostics: diags,
	})
}

// CreateProgress sends the server's window/workDoneProgress/create request to the
// client under test.
func (s *FakeServer) CreateProgress(ctx context.Context, params *protocol.WorkDoneProgressCreateParams) error {
	return s.client.WorkDoneProgressCreate(ctx, params)
}

// SendProgress sends one $/progress notification to the client under test.
func (s *FakeServer) SendProgress(ctx context.Context, params *protocol.ProgressParams) error {
	return s.client.Progress(ctx, params)
}
