// Package lsp is a pure-Go Language Server Protocol client built on
// go.lsp.dev/jsonrpc2 and go.lsp.dev/protocol.
//
// A Client wraps one JSON-RPC connection. Requests take a context.Context and
// return when the server answers or the context ends. Notifications (initialized,
// didOpen, didChange, didClose, exit) are writes that do not wait for a reply.
// Diagnostics arrive on the channel returned by Client.Diagnostics.
//
// Only Start launches a process (with os/exec, and only when Config.Command is
// set). NewClient wraps an existing io.ReadWriteCloser, which is how tests
// connect the client to a fake server over io.Pipe. The multi-server Manager and
// the built-in server table live in package lsp/manager.
package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// diagnosticsBuffer is the capacity of the Diagnostics channel.
const diagnosticsBuffer = 64

// ErrNoCommand is returned by Start when Config.Command is empty.
var ErrNoCommand = errors.New("lsp: no server command")

// Config describes a language server.
type Config struct {
	// Command is the program and its arguments. Only Start reads it; NewClient
	// ignores it.
	Command []string
	// RootURI is the workspace root sent in initialize, for example
	// "file:///home/u/proj". Empty means no root.
	RootURI string
	// Env, when non-nil, is appended to the parent environment of the server
	// process.
	Env []string
	// InitializationOptions is the JSON sent as initialize's
	// initializationOptions, or nil for none. It is passed through verbatim, so
	// a server reads it in the form the user wrote.
	InitializationOptions protocol.LSPAny
	// ApplyEdit, when non-nil, answers a server's workspace/applyEdit request.
	// It runs on the connection's read goroutine, so it must not wait on the
	// UI: enqueue the edit and return whether it was accepted. Answering here,
	// off the request queue, is what keeps a server's executeCommand followed by
	// applyEdit from deadlocking on the main thread.
	ApplyEdit func(protocol.ApplyWorkspaceEditParams) bool
}

// Client is a connection to one language server. All methods are safe for
// concurrent use. Close is safe to call more than once.
type Client struct {
	conn    jsonrpc2.Conn
	server  protocol.Server
	rwc     *guardedRWC
	rootURI string

	// ctx is passed to the read loop. Close cancels it after the connection
	// has stopped.
	ctx    context.Context
	cancel context.CancelFunc

	// diags is closed after the read loop exits, so no send can follow the close.
	diags chan DiagnosticsEvent

	// diagMu guards diagByURI, the latest publishDiagnostics event per document.
	// Unlike diags, it is never trimmed by the drop-oldest policy.
	diagMu    sync.Mutex
	diagByURI map[string]DiagnosticsEvent

	// applyEdit answers workspace/applyEdit; nil when the editor takes none.
	applyEdit func(protocol.ApplyWorkspaceEditParams) bool
	// initOptions answers workspace/configuration, and is what initialize sent.
	initOptions protocol.LSPAny

	// capMu guards the capability snapshot: capStatic is what the initialize
	// result advertised, dyn holds the providers registered later by
	// registration id, and caps is the two combined. The read goroutine writes
	// them; request callers read them.
	capMu     sync.Mutex
	caps      Capabilities
	capStatic Capabilities
	dyn       map[string]dynRegistration
	initRes   *protocol.InitializeResult

	// proc is set only by Start. stderr holds the tail of its error output.
	proc   *exec.Cmd
	stderr *tailBuffer

	closeOnce sync.Once
	closeErr  error
}

// dynRegistration is one provider a server registered with
// client/registerCapability, kept so an unregistration can recompute the
// capability snapshot from the static capabilities.
type dynRegistration struct {
	method  string
	options protocol.LSPAny
}

// Start launches the server in cfg.Command with os/exec and connects to its
// stdin and stdout. ctx is checked once, before the launch. It does not bind
// the process lifetime: the server runs until Close (or until it exits).
func Start(ctx context.Context, cfg Config) (*Client, error) {
	if len(cfg.Command) == 0 {
		return nil, ErrNoCommand
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The program is looked up here, so a server that is not installed is told apart from
	// one that failed, and nothing is launched for it. Start runs in a command, never in
	// the update loop, so the PATH search is off the loop.
	path, err := exec.LookPath(cfg.Command[0])
	if err != nil {
		return nil, &NotInstalledError{Command: cfg.Command[0]}
	}
	cmd := exec.Command(path, cfg.Command[1:]...) //nolint:gosec // the caller chooses the server command
	stderr := &tailBuffer{max: stderrKeep}
	cmd.Stderr = stderr
	// A grandchild that keeps the error pipe open must not keep Close waiting.
	cmd.WaitDelay = killGrace
	if cfg.Env != nil {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("lsp: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("lsp: start %s: %w", cfg.Command[0], err)
	}

	c := NewClient(procPipes{r: stdout, w: stdin}, cfg)
	c.proc = cmd
	c.stderr = stderr
	return c, nil
}

// stderrKeep is how much of a server's error output the client keeps.
const stderrKeep = 8 << 10

// StderrTail returns the last lines of what the server process wrote to its error output,
// for the message of a server that failed. It is empty for a client that was not launched by
// Start. Call it after Close to be sure the process has written all it had.
func (c *Client) StderrTail() string {
	return c.stderr.tail(4, 400)
}

// NewClient wraps rwc as a connection to a server and starts the read loop. The
// caller owns rwc only until Close, which closes it. cfg.Command and cfg.Env are
// ignored; cfg.RootURI is used by Initialize.
func NewClient(rwc io.ReadWriteCloser, cfg Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		rwc:         &guardedRWC{rwc: rwc},
		rootURI:     cfg.RootURI,
		ctx:         ctx,
		cancel:      cancel,
		diags:       make(chan DiagnosticsEvent, diagnosticsBuffer),
		diagByURI:   make(map[string]DiagnosticsEvent),
		applyEdit:   cfg.ApplyEdit,
		initOptions: cfg.InitializationOptions,
		dyn:         make(map[string]dynRegistration),
	}
	c.conn = jsonrpc2.NewConn(jsonrpc2.NewHeaderStream(c.rwc), jsonrpc2.WithCodec(lspCodec{}))
	c.server = protocol.ServerDispatcher(c.conn)

	h := &clientHandler{c: c, rootURI: cfg.RootURI}
	c.conn.Go(ctx, protocol.ClientHandler(h, jsonrpc2.MethodNotFoundHandler))

	// The read loop is the only sender on diags. Done closes after the loop has
	// exited, so closing diags here cannot race with a send.
	go func() {
		<-c.conn.Done()
		close(c.diags)
	}()
	return c
}

// Initialize sends the initialize request with the root from Config.RootURI and
// the client capabilities this package supports (publishDiagnostics,
// synchronization, and code action literals). It returns the server's result.
// Call Initialized after it.
func (c *Client) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	params := &protocol.InitializeParams{
		Capabilities:          clientCapabilities(),
		InitializationOptions: c.initOptions,
	}
	if c.rootURI != "" {
		root := uri.URI(c.rootURI)
		params.WorkspaceFolders = protocol.NewNullable([]protocol.WorkspaceFolder{{URI: root, Name: "root"}})
		// rootUri is deprecated in LSP 3.16 in favour of workspaceFolders, but
		// older servers still read only rootUri, so both are sent.
		params.RootURI = &root //nolint:staticcheck // deprecated, kept for older servers
	}
	res, err := c.server.Initialize(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("lsp: initialize: %w", err)
	}
	c.capMu.Lock()
	c.initRes = res
	c.capStatic = summarizeCapabilities(res.Capabilities)
	c.caps = c.capStatic
	for _, reg := range c.dyn {
		applyRegistration(&c.caps, reg.method, reg.options)
	}
	c.capMu.Unlock()
	return res, nil
}

// InitializeResult returns the server's initialize result. ok is false before
// Initialize returns.
func (c *Client) InitializeResult() (*protocol.InitializeResult, bool) {
	c.capMu.Lock()
	defer c.capMu.Unlock()
	return c.initRes, c.initRes != nil
}

// Capabilities returns what the server can serve: the providers it advertised in
// its initialize result, plus the ones it registered later. Initialized is false
// until Initialize returns, and the manager routes no request to a server whose
// capabilities it does not know.
func (c *Client) Capabilities() Capabilities {
	c.capMu.Lock()
	defer c.capMu.Unlock()
	return c.caps
}

// registerCapabilities applies the providers the server registered with
// client/registerCapability, so a server that advertises little statically can
// still serve features it registers dynamically.
func (c *Client) registerCapabilities(regs []protocol.Registration) {
	c.capMu.Lock()
	defer c.capMu.Unlock()
	for _, reg := range regs {
		dyn := dynRegistration{method: reg.Method, options: reg.RegisterOptions}
		c.dyn[reg.ID] = dyn
		applyRegistration(&c.caps, dyn.method, dyn.options)
	}
}

// unregisterCapabilities removes the providers the server unregistered and
// recomputes the snapshot from the static capabilities plus what is left.
func (c *Client) unregisterCapabilities(regs []protocol.Unregistration) {
	c.capMu.Lock()
	defer c.capMu.Unlock()
	for _, reg := range regs {
		delete(c.dyn, reg.ID)
	}
	c.caps = c.capStatic
	for _, dyn := range c.dyn {
		applyRegistration(&c.caps, dyn.method, dyn.options)
	}
}

// Initialized sends the initialized notification, which ends the handshake.
func (c *Client) Initialized(ctx context.Context) error {
	if err := c.server.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		return fmt.Errorf("lsp: initialized: %w", err)
	}
	return nil
}

// DidOpen sends textDocument/didOpen with the full text of the document.
func (c *Client) DidOpen(ctx context.Context, docURI, languageID string, version int32, text string) error {
	err := c.server.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri.URI(docURI),
			LanguageID: protocol.LanguageKind(languageID),
			Version:    version,
			Text:       text,
		},
	})
	if err != nil {
		return fmt.Errorf("lsp: didOpen %s: %w", docURI, err)
	}
	return nil
}

// DidChange sends textDocument/didChange with the whole new text. The client
// uses full-document sync, so there is no incremental edit.
func (c *Client) DidChange(ctx context.Context, docURI string, version int32, text string) error {
	err := c.server.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
			Version:                version,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: text},
		},
	})
	if err != nil {
		return fmt.Errorf("lsp: didChange %s: %w", docURI, err)
	}
	return nil
}

// DidClose sends textDocument/didClose.
func (c *Client) DidClose(ctx context.Context, docURI string) error {
	err := c.server.DidClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri.URI(docURI)},
	})
	if err != nil {
		return fmt.Errorf("lsp: didClose %s: %w", docURI, err)
	}
	return nil
}

// Shutdown sends the shutdown request. The server stops processing requests
// but does not exit until it gets exit. Send Exit, then Close.
func (c *Client) Shutdown(ctx context.Context) error {
	if err := c.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("lsp: shutdown: %w", err)
	}
	return nil
}

// Exit sends the exit notification. The server then terminates.
func (c *Client) Exit(ctx context.Context) error {
	if err := c.server.Exit(ctx); err != nil {
		return fmt.Errorf("lsp: exit: %w", err)
	}
	return nil
}

// Done returns a channel that is closed once the connection has ended: the
// server exited, its pipe closed, or Close was called.
func (c *Client) Done() <-chan struct{} {
	return c.conn.Done()
}

// Diagnostics returns the channel of publishDiagnostics events. The channel is
// closed when the connection ends, either after Close or after the server
// closes its side of the transport.
//
// Delivery policy: the channel has capacity 64, and the read loop never blocks
// on it. When the buffer is full, the oldest queued event is dropped to make room
// for the newest one. A consumer that falls behind therefore sees the most
// recent state of each document, not every intermediate version.
func (c *Client) Diagnostics() <-chan DiagnosticsEvent {
	return c.diags
}

// DiagnosticsFor returns the latest publishDiagnostics event for docURI. Use it
// for the current state of a document; Diagnostics is a bounded queue and can
// omit intermediate events. ok is false until the server publishes for docURI.
// An ok event with no diagnostics means the server cleared them. The returned
// slice is a copy.
func (c *Client) DiagnosticsFor(docURI string) (DiagnosticsEvent, bool) {
	c.diagMu.Lock()
	ev, ok := c.diagByURI[docURI]
	c.diagMu.Unlock()
	ev.Diagnostics = slices.Clone(ev.Diagnostics)
	return ev, ok
}

// DiagnosticsAll returns the latest publishDiagnostics event for every document
// the server has published for, keyed by document URI. The returned slices are
// copies. It is the workspace-wide view the diagnostics panel lists.
func (c *Client) DiagnosticsAll() map[string]DiagnosticsEvent {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	out := make(map[string]DiagnosticsEvent, len(c.diagByURI))
	for uri, ev := range c.diagByURI {
		ev.Diagnostics = slices.Clone(ev.Diagnostics)
		out[uri] = ev
	}
	return out
}

// publish records ev as the latest event for its document and queues it without
// blocking. Only the read loop calls it, so a single-producer drop-oldest loop
// always finishes after at most one drop.
func (c *Client) publish(ev DiagnosticsEvent) {
	c.diagMu.Lock()
	c.diagByURI[ev.URI] = ev
	c.diagMu.Unlock()

	for {
		select {
		case c.diags <- ev:
			return
		default:
		}
		select {
		case <-c.diags: // drop the oldest queued event
		default:
		}
	}
}

// Close tears the connection down. It does not send shutdown or exit, so call
// Shutdown and Exit first for a graceful stop. Close does this:
//
//  1. cancels the read loop context and closes the transport. This unblocks
//     the read loop even when requests are still pending, and pending requests
//     return an error.
//  2. waits for the connection to terminate, which closes Diagnostics.
//  3. for a process started by Start, closes its stdin and waits for it to
//     exit, killing it after a short grace period.
//
// Close is idempotent: the first call does the work and every later call returns
// the same result. Its error is the transport close error, if any.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		_ = c.rwc.Close()
		c.closeErr = c.conn.Close()
		if c.proc != nil {
			waitProcess(c.proc)
		}
	})
	return c.closeErr
}

// clientCapabilities lists what this client supports. It advertises the whole
// surface the editor asks for: workspace edits (with document changes and file
// operations), server-pulled configuration, publishDiagnostics with versions,
// full text synchronization, code action literals with resolve, rename, format,
// inlay hints, folding ranges, semantic tokens and workspace symbols. A server
// gates some of its own providers on these flags.
func clientCapabilities() protocol.ClientCapabilities {
	yes := new(true)
	return protocol.ClientCapabilities{
		Workspace: &protocol.WorkspaceClientCapabilities{
			ApplyEdit: yes,
			WorkspaceEdit: &protocol.WorkspaceEditClientCapabilities{
				DocumentChanges: yes,
				ResourceOperations: []protocol.ResourceOperationKind{
					protocol.ResourceOperationKindCreate,
					protocol.ResourceOperationKindRename,
					protocol.ResourceOperationKindDelete,
				},
				FailureHandling: protocol.FailureHandlingKindTextOnlyTransactional,
			},
			Configuration:    yes,
			WorkspaceFolders: yes,
			ExecuteCommand:   &protocol.ExecuteCommandClientCapabilities{},
			Symbol:           &protocol.WorkspaceSymbolClientCapabilities{},
		},
		TextDocument: &protocol.TextDocumentClientCapabilities{
			Synchronization: &protocol.TextDocumentSyncClientCapabilities{},
			PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{
				VersionSupport: yes,
			},
			CodeAction: &protocol.CodeActionClientCapabilities{
				CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
					CodeActionKind: protocol.ClientCodeActionKindOptions{
						ValueSet: []protocol.CodeActionKind{
							protocol.CodeActionKindQuickFix,
							protocol.CodeActionKindRefactor,
							protocol.CodeActionKindRefactorExtract,
							protocol.CodeActionKindRefactorInline,
							protocol.CodeActionKindRefactorRewrite,
							protocol.CodeActionKindSource,
							protocol.CodeActionKindSourceOrganizeImports,
							protocol.CodeActionKindSourceFixAll,
						},
					},
				},
				IsPreferredSupport: yes,
				DataSupport:        yes,
				ResolveSupport: protocol.ClientCodeActionResolveOptions{
					Properties: []string{"edit"},
				},
			},
			Hover:             &protocol.HoverClientCapabilities{ContentFormat: []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText}},
			SignatureHelp:     &protocol.SignatureHelpClientCapabilities{},
			Definition:        &protocol.DefinitionClientCapabilities{},
			Implementation:    &protocol.ImplementationClientCapabilities{},
			References:        &protocol.ReferenceClientCapabilities{},
			DocumentSymbol:    &protocol.DocumentSymbolClientCapabilities{},
			Formatting:        &protocol.DocumentFormattingClientCapabilities{},
			Rename:            &protocol.RenameClientCapabilities{},
			FoldingRange:      &protocol.FoldingRangeClientCapabilities{},
			InlayHint:         &protocol.InlayHintClientCapabilities{},
			DocumentHighlight: &protocol.DocumentHighlightClientCapabilities{},
			SemanticTokens: protocol.SemanticTokensClientCapabilities{
				Requests: protocol.ClientSemanticTokensRequestOptions{
					Full:  &protocol.ClientSemanticTokensRequestFullDelta{Delta: yes},
					Range: protocol.Boolean(true),
				},
				TokenTypes:     semanticTokenTypes,
				TokenModifiers: semanticTokenModifiers,
				Formats:        []protocol.TokenFormat{protocol.TokenFormatRelative},
			},
		},
		Window: &protocol.WindowClientCapabilities{WorkDoneProgress: yes},
	}
}

// semanticTokenTypes are the standard LSP token types, the subset this client
// understands. A server's legend names them.
var semanticTokenTypes = []string{
	"namespace", "type", "class", "enum", "interface", "struct", "typeParameter",
	"parameter", "variable", "property", "enumMember", "event", "function", "method",
	"macro", "keyword", "modifier", "comment", "string", "number", "regexp",
	"operator", "decorator",
}

// semanticTokenModifiers are the standard LSP token modifiers.
var semanticTokenModifiers = []string{
	"declaration", "definition", "readonly", "static", "deprecated", "abstract",
	"async", "modification", "documentation", "defaultLibrary",
}

//go:fix inline
