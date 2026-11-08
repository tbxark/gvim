package lsp

import (
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/tbxark/gvim/lsp/lsptest"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// pair is a Client connected to a FakeServer over in-memory pipes.
type pair struct {
	client  *Client
	srvConn jsonrpc2.Conn
	srv     *lsptest.FakeServer
	srvEnd  lsptest.PipeRWC
}

// startPair builds the fake server and the client, and starts both read loops.
// It registers no cleanup, so callers that check goroutine counts can control
// teardown.
func startPair(cfg Config, stall chan struct{}) pair {
	return startPairOver(cfg, stall, func(cli lsptest.PipeRWC) io.ReadWriteCloser { return cli })
}

// startPairOver is startPair with the client end of the pipes passed through
// wrap, so a test can put a transport such as gatedRWC between the client and
// the fake server.
func startPairOver(cfg Config, stall chan struct{}, wrap func(lsptest.PipeRWC) io.ReadWriteCloser) pair {
	cliEnd, srvEnd := lsptest.NewPipePair()
	srv, srvConn := lsptest.StartServer(srvEnd, stall)
	return pair{
		client:  NewClient(wrap(cliEnd), cfg),
		srvConn: srvConn,
		srv:     srv,
		srvEnd:  srvEnd,
	}
}

// newFixture is startPair with cleanup. Cleanups run in reverse order, so a
// stall channel closed after newFixture is released before the connections close.
func newFixture(t *testing.T, cfg Config, stall chan struct{}) pair {
	t.Helper()
	p := startPair(cfg, stall)
	t.Cleanup(func() {
		_ = p.client.Close()
		_ = p.srvConn.Close()
	})
	return p
}

func newTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), lsptest.Timeout)
	t.Cleanup(cancel)
	return ctx
}

func TestStartRequiresCommandAndDoesNotLaunch(t *testing.T) {
	tests := []struct {
		name    string
		ctx     func() context.Context
		cfg     Config
		wantErr error
	}{
		{
			name:    "empty command",
			ctx:     context.Background,
			cfg:     Config{},
			wantErr: ErrNoCommand,
		},
		{
			name: "canceled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			cfg:     Config{Command: []string{"/nonexistent/lsp-server"}},
			wantErr: context.Canceled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Start(tt.ctx(), tt.cfg)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Start error = %v, want %v", err, tt.wantErr)
			}
			if c != nil {
				t.Fatalf("Start returned a client with error %v", err)
			}
		})
	}
}

func TestInitializeRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		rootURI  string
		wantRoot bool
	}{
		{name: "with root", rootURI: "file:///work/proj", wantRoot: true},
		{name: "without root", rootURI: "", wantRoot: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{RootURI: tt.rootURI}, nil)
			ctx := newTestContext(t)

			res, err := f.client.Initialize(ctx)
			if err != nil {
				t.Fatalf("Initialize: %v", err)
			}
			if err := f.client.Initialized(ctx); err != nil {
				t.Fatalf("Initialized: %v", err)
			}

			got := lsptest.Recv(t, f.srv.InitCh, "initialize params")
			folders, hasFolders := got.WorkspaceFolders.Get()
			if hasFolders != tt.wantRoot {
				t.Fatalf("server saw workspace folders = %v, want %v", hasFolders, tt.wantRoot)
			}
			if tt.wantRoot && (len(folders) != 1 || string(folders[0].URI) != tt.rootURI) {
				t.Fatalf("server saw workspace folders %+v, want one folder at %q", folders, tt.rootURI)
			}
			if gotRoot := got.RootURI != nil; gotRoot != tt.wantRoot { //nolint:staticcheck // deprecated rootUri is still sent
				t.Fatalf("server saw rootUri = %v, want %v", gotRoot, tt.wantRoot)
			}
			td := got.Capabilities.TextDocument
			if td == nil || td.PublishDiagnostics == nil || td.Synchronization == nil {
				t.Fatalf("client capabilities missing publishDiagnostics or synchronization: %+v", td)
			}

			sync, ok := res.Capabilities.TextDocumentSync.(protocol.TextDocumentSyncKind)
			if !ok || sync != protocol.TextDocumentSyncKindFull {
				t.Fatalf("server sync capability = %#v, want full", res.Capabilities.TextDocumentSync)
			}
		})
	}
}

func TestInitializeSendsCodeActionLiteralSupport(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	got := lsptest.Recv(t, f.srv.InitCh, "initialize params")
	td := got.Capabilities.TextDocument
	if td == nil || td.CodeAction == nil {
		t.Fatalf("client capabilities have no codeAction: %+v", td)
	}
	kinds := td.CodeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet
	for _, want := range []protocol.CodeActionKind{protocol.CodeActionKindQuickFix, protocol.CodeActionKindRefactor, protocol.CodeActionKindSource} {
		if !slices.Contains(kinds, want) {
			t.Errorf("codeActionLiteralSupport value set = %v, want it to include %q", kinds, want)
		}
	}
}

func TestDoneClosesWhenServerClosesPipe(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	select {
	case <-f.client.Done():
		t.Fatal("Done closed while the server is connected")
	default:
	}

	_ = f.srvEnd.Close()
	select {
	case <-f.client.Done():
	case <-time.After(lsptest.Timeout):
		t.Fatal("Done was not closed after the server closed the pipe")
	}
}

func TestDiagnosticsDelivery(t *testing.T) {
	const docURI = "file:///work/proj/main.go"
	tests := []struct {
		name      string
		openText  string
		wantOpen  int
		wantFirst string
	}{
		{name: "clean document sends an empty set", openText: "ok\nfine\n", wantOpen: 0},
		{name: "errors are delivered", openText: "ERR one\nok\nERR two\n", wantOpen: 2, wantFirst: "found ERR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{}, nil)
			ctx := newTestContext(t)

			if err := f.client.DidOpen(ctx, docURI, "go", 1, tt.openText); err != nil {
				t.Fatalf("DidOpen: %v", err)
			}
			open := lsptest.Recv(t, f.client.Diagnostics(), "diagnostics after didOpen")
			if open.URI != docURI || open.Version != 1 {
				t.Fatalf("event = {URI:%q Version:%d}, want {%q 1}", open.URI, open.Version, docURI)
			}
			if len(open.Diagnostics) != tt.wantOpen {
				t.Fatalf("didOpen diagnostics = %d, want %d", len(open.Diagnostics), tt.wantOpen)
			}
			if tt.wantFirst != "" {
				msg, ok := open.Diagnostics[0].Message.(protocol.String)
				if !ok || string(msg) != tt.wantFirst {
					t.Fatalf("first message = %#v, want %q", open.Diagnostics[0].Message, tt.wantFirst)
				}
			}

			if err := f.client.DidChange(ctx, docURI, 2, "ERR only\n"); err != nil {
				t.Fatalf("DidChange: %v", err)
			}
			change := lsptest.Recv(t, f.client.Diagnostics(), "diagnostics after didChange")
			if change.Version != 2 || len(change.Diagnostics) != 1 {
				t.Fatalf("didChange event = version %d with %d diagnostics, want version 2 with 1", change.Version, len(change.Diagnostics))
			}
			if got := lsptest.Recv(t, f.srv.ChangeCh, "server didChange"); got.TextDocument.URI != docURI {
				t.Fatalf("server saw didChange for %q", got.TextDocument.URI)
			}

			if err := f.client.DidClose(ctx, docURI); err != nil {
				t.Fatalf("DidClose: %v", err)
			}
			if got := lsptest.Recv(t, f.srv.CloseCh, "server didClose"); got.TextDocument.URI != docURI {
				t.Fatalf("server saw didClose for %q", got.TextDocument.URI)
			}
			if got := lsptest.Recv(t, f.srv.OpenCh, "server didOpen"); got.TextDocument.Text != tt.openText {
				t.Fatalf("server saw didOpen text %q, want %q", got.TextDocument.Text, tt.openText)
			}
		})
	}
}

func TestDiagnosticsDropOldestWhenFull(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)

	const total = diagnosticsBuffer + 36
	const docURI = "file:///work/proj/busy.go"
	for v := int32(1); v <= total; v++ {
		if err := f.client.DidChange(ctx, docURI, v, "ERR\n"); err != nil {
			t.Fatalf("DidChange %d: %v", v, err)
		}
	}
	// Shutdown is answered after every earlier notification has been handled,
	// so all events are in the channel when it returns.
	if err := f.client.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	var versions []int32
	for len(f.client.Diagnostics()) > 0 {
		ev := lsptest.Recv(t, f.client.Diagnostics(), "queued diagnostics")
		versions = append(versions, ev.Version)
	}
	if len(versions) != diagnosticsBuffer {
		t.Fatalf("queued events = %d, want %d", len(versions), diagnosticsBuffer)
	}
	if want := int32(total - diagnosticsBuffer + 1); versions[0] != want {
		t.Fatalf("oldest kept version = %d, want %d", versions[0], want)
	}
	if versions[len(versions)-1] != total {
		t.Fatalf("newest kept version = %d, want %d", versions[len(versions)-1], total)
	}
}

func TestContextCancelOfPendingRequest(t *testing.T) {
	tests := []struct {
		name      string
		cancelNow bool
		wantErr   error
	}{
		{name: "explicit cancel", cancelNow: true, wantErr: context.Canceled},
		{name: "deadline exceeded", cancelNow: false, wantErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stall := make(chan struct{})
			f := newFixture(t, Config{}, stall)
			t.Cleanup(func() { close(stall) })

			var ctx context.Context
			var cancel context.CancelFunc
			if tt.cancelNow {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()

			errCh := make(chan error, 1)
			go func() { errCh <- f.client.Shutdown(ctx) }()

			// The server has the request and is blocked in its handler.
			lsptest.Recv(t, f.srv.StallEntered, "server entered Shutdown")
			if tt.cancelNow {
				cancel()
			}

			var err error
			select {
			case err = <-errCh:
			case <-time.After(lsptest.Timeout):
				t.Fatal("pending request did not return after its context ended")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Shutdown error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestServerClosesPipe(t *testing.T) {
	stall := make(chan struct{})
	f := newFixture(t, Config{}, stall)
	t.Cleanup(func() { close(stall) })
	ctx := newTestContext(t)

	// A request that is still pending when the server goes away.
	pendingErr := make(chan error, 1)
	go func() { pendingErr <- f.client.Shutdown(ctx) }()
	lsptest.Recv(t, f.srv.StallEntered, "server entered Shutdown")

	_ = f.srvEnd.Close()

	select {
	case err := <-pendingErr:
		if err == nil {
			t.Fatal("pending Shutdown returned nil after the server closed the pipe")
		}
	case <-time.After(lsptest.Timeout):
		t.Fatal("pending Shutdown hung after the server closed the pipe")
	}

	ops := []struct {
		name string
		run  func() error
	}{
		{"initialize", func() error { _, err := f.client.Initialize(ctx); return err }},
		{"initialized", func() error { return f.client.Initialized(ctx) }},
		{"shutdown", func() error { return f.client.Shutdown(ctx) }},
		{"didOpen", func() error { return f.client.DidOpen(ctx, "file:///a.go", "go", 1, "x") }},
		{"didChange", func() error { return f.client.DidChange(ctx, "file:///a.go", 2, "y") }},
		{"didClose", func() error { return f.client.DidClose(ctx, "file:///a.go") }},
		{"exit", func() error { return f.client.Exit(ctx) }},
	}
	for _, op := range ops {
		if err := op.run(); err == nil {
			t.Errorf("%s returned nil error after the server closed the pipe", op.name)
		}
	}

	select {
	case _, ok := <-f.client.Diagnostics():
		if ok {
			t.Fatal("Diagnostics delivered an event after the server closed the pipe")
		}
	case <-time.After(lsptest.Timeout):
		t.Fatal("Diagnostics channel was not closed after the server closed the pipe")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	tests := []struct {
		name       string
		pending    bool
		closeCount int
	}{
		{name: "single close", closeCount: 1},
		{name: "repeated close", closeCount: 3},
		{name: "close with a pending request", pending: true, closeCount: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stall := make(chan struct{})
			f := newFixture(t, Config{}, stall)
			t.Cleanup(func() { close(stall) })
			ctx := newTestContext(t)

			pendingErr := make(chan error, 1)
			if tt.pending {
				go func() { pendingErr <- f.client.Shutdown(ctx) }()
				lsptest.Recv(t, f.srv.StallEntered, "server entered Shutdown")
			}

			errs := make(chan error, tt.closeCount)
			go func() {
				for i := 0; i < tt.closeCount; i++ {
					errs <- f.client.Close()
				}
			}()
			for i := 0; i < tt.closeCount; i++ {
				var err error
				select {
				case err = <-errs:
				case <-time.After(lsptest.Timeout):
					t.Fatalf("Close call %d did not return", i+1)
				}
				if err != nil {
					t.Fatalf("Close call %d = %v, want nil", i+1, err)
				}
			}

			if tt.pending {
				select {
				case err := <-pendingErr:
					if err == nil {
						t.Fatal("pending Shutdown returned nil after Close")
					}
				case <-time.After(lsptest.Timeout):
					t.Fatal("pending Shutdown did not return after Close")
				}
			}

			if err := f.client.DidOpen(ctx, "file:///a.go", "go", 1, ""); err == nil {
				t.Fatal("DidOpen after Close returned nil error")
			}
			select {
			case _, ok := <-f.client.Diagnostics():
				if ok {
					t.Fatal("Diagnostics delivered an event after Close")
				}
			case <-time.After(lsptest.Timeout):
				t.Fatal("Diagnostics channel was not closed by Close")
			}
		})
	}
}

// TestCloseReleasesGoroutines counts goroutines before the connection starts and
// after Close. The read loop, the Diagnostics closer, and any handler goroutines
// must all be gone. The count is polled because the closer goroutine can finish
// a moment after Close returns.
func TestCloseReleasesGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()

	p := startPair(Config{RootURI: "file:///work/proj"}, nil)
	ctx := newTestContext(t)
	if _, err := p.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := p.client.DidOpen(ctx, "file:///work/proj/a.go", "go", 1, "ERR\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	lsptest.Recv(t, p.client.Diagnostics(), "diagnostics")

	if err := p.client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = p.srvConn.Close()

	deadline := time.Now().Add(lsptest.Timeout)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines after Close = %d, baseline = %d", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
