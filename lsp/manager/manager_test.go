package manager

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/lsptest"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

const (
	mgrRoot    = "/work/proj"
	mgrRootURI = "file:///work/proj"
	mgrAPath   = "/work/proj/a.go"
	mgrAURI    = "file:///work/proj/a.go"
	mgrBPath   = "/work/proj/b.go"
	mgrBURI    = "file:///work/proj/b.go"
)

// errStartFailed is returned by startLog when its fail field is set.
var errStartFailed = errors.New("start refused")

// startLog is a Manager start hook that records each server it starts. Each
// server is a lsptest.FakeServer on in-memory pipes, so a test can inspect what the
// Manager sent. No process is launched.
type startLog struct {
	t     *testing.T
	cfgs  []lsp.Config
	pairs []pair
	// fail, when set, makes start return it and start nothing.
	fail error
	// dead, when set, closes the server end before the handshake.
	dead bool
}

// pair is an lsp.Client connected to a FakeServer over in-memory pipes.
type pair struct {
	client  *lsp.Client
	srvConn jsonrpc2.Conn
	srv     *lsptest.FakeServer
	srvEnd  lsptest.PipeRWC
}

// startPair builds the fake server and the client, and starts both read loops.
// It registers no cleanup, so callers that check goroutine counts can control
// teardown.
func startPair(cfg lsp.Config, stall chan struct{}) pair {
	return startPairOver(cfg, stall, func(cli lsptest.PipeRWC) io.ReadWriteCloser { return cli })
}

// startPairOver is startPair with the client end of the pipes passed through
// wrap, so a test can put a transport such as gatedRWC between the client and
// the fake server.
func startPairOver(cfg lsp.Config, stall chan struct{}, wrap func(lsptest.PipeRWC) io.ReadWriteCloser) pair {
	cliEnd, srvEnd := lsptest.NewPipePair()
	srv, srvConn := lsptest.StartServer(srvEnd, stall)
	return pair{
		client:  lsp.NewClient(wrap(cliEnd), cfg),
		srvConn: srvConn,
		srv:     srv,
		srvEnd:  srvEnd,
	}
}

func newTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), lsptest.Timeout)
	t.Cleanup(cancel)
	return ctx
}

func (s *startLog) start(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
	s.cfgs = append(s.cfgs, cfg)
	if s.fail != nil {
		return nil, s.fail
	}
	p := startPair(cfg, nil)
	s.t.Cleanup(func() {
		_ = p.client.Close()
		_ = p.srvConn.Close()
	})
	if s.dead {
		_ = p.srvEnd.Close()
	}
	s.pairs = append(s.pairs, p)
	return p.client, nil
}

// newLoggedManager returns a Manager for mgrRoot whose servers come from log.
func newLoggedManager(log *startLog, servers map[string]ServerConfig) *Manager {
	m := NewManager(mgrRoot, servers)
	m.start = log.start
	return m
}

var goAndMarkdown = map[string]ServerConfig{
	"go":       {Command: []string{"gopls"}, Args: []string{"serve"}},
	"markdown": {Command: []string{"marksman"}},
}

func TestManagerStartsOneServerPerLanguage(t *testing.T) {
	log := &startLog{t: t}
	m := newLoggedManager(log, goAndMarkdown)

	if len(log.cfgs) != 0 {
		t.Fatalf("NewManager started %d servers, want none until Open", len(log.cfgs))
	}

	if err := m.Open(mgrAPath, "go", "ERR\n"); err != nil {
		t.Fatalf("Open a.go: %v", err)
	}
	if len(log.cfgs) != 1 {
		t.Fatalf("servers started = %d, want 1", len(log.cfgs))
	}
	if want := []string{"gopls", "serve"}; !slices.Equal(log.cfgs[0].Command, want) || log.cfgs[0].RootURI != mgrRootURI {
		t.Fatalf("start config = %+v, want command %v at root %q", log.cfgs[0], want, mgrRootURI)
	}

	goSrv := onlyPair(t, log)
	init := lsptest.Recv(t, goSrv.srv.InitCh, "initialize params")
	folders, _ := init.WorkspaceFolders.Get()
	if len(folders) != 1 || folders[0].URI != mgrRootURI {
		t.Fatalf("initialize workspace folders = %+v, want one folder at %q", folders, mgrRootURI)
	}

	open := lsptest.Recv(t, goSrv.srv.OpenCh, "didOpen a.go")
	if open.TextDocument.URI != mgrAURI || open.TextDocument.LanguageID != "go" ||
		open.TextDocument.Version != 1 || open.TextDocument.Text != "ERR\n" {
		t.Fatalf("didOpen = %+v, want go document a.go version 1", open.TextDocument)
	}

	c, ok := m.ClientFor(mgrAPath)
	if !ok || c != goSrv.client {
		t.Fatal("ClientFor did not return the client that opened a.go")
	}
	lsptest.Recv(t, c.Diagnostics(), "diagnostics for a.go")
	if ev, ok := c.DiagnosticsFor(mgrAURI); !ok || len(ev.Diagnostics) != 1 {
		t.Fatalf("snapshot for a.go = %+v, %v; want one diagnostic", ev, ok)
	}

	if err := m.Open(mgrBPath, "go", "ok\n"); err != nil {
		t.Fatalf("Open b.go: %v", err)
	}
	if len(log.cfgs) != 1 {
		t.Fatalf("second go document started another server: %d starts", len(log.cfgs))
	}
	lsptest.Recv(t, goSrv.srv.OpenCh, "didOpen b.go")
	if cb, ok := m.ClientFor(mgrBPath); !ok || cb != c {
		t.Fatal("documents of one language do not share a client")
	}

	if err := m.Open("/work/proj/README.md", "markdown", "# hi\n"); err != nil {
		t.Fatalf("Open README.md: %v", err)
	}
	if len(log.cfgs) != 2 || !slices.Equal(log.cfgs[1].Command, []string{"marksman"}) {
		t.Fatalf("starts = %+v, want a second server for markdown", log.cfgs)
	}
}

func TestManagerChangeAndClose(t *testing.T) {
	log := &startLog{t: t}
	m := newLoggedManager(log, goAndMarkdown)
	if err := m.Open(mgrAPath, "go", "ERR\n"); err != nil {
		t.Fatalf("Open a.go: %v", err)
	}
	if err := m.Open(mgrBPath, "go", "ok\n"); err != nil {
		t.Fatalf("Open b.go: %v", err)
	}
	srv := onlyPair(t, log).srv
	lsptest.Recv(t, srv.OpenCh, "didOpen a.go")
	lsptest.Recv(t, srv.OpenCh, "didOpen b.go")

	for _, want := range []struct {
		text    string
		version int32
	}{{"ok\n", 2}, {"ERR again\n", 3}} {
		if err := m.Change(mgrAPath, want.text); err != nil {
			t.Fatalf("Change a.go: %v", err)
		}
		got := lsptest.Recv(t, srv.ChangeCh, "didChange a.go")
		if got.TextDocument.URI != mgrAURI || got.TextDocument.Version != want.version {
			t.Fatalf("didChange = %q version %d, want %q version %d", got.TextDocument.URI, got.TextDocument.Version, mgrAURI, want.version)
		}
		whole, ok := got.ContentChanges[0].(*protocol.TextDocumentContentChangeWholeDocument)
		if !ok || whole.Text != want.text {
			t.Fatalf("didChange content = %#v, want whole document %q", got.ContentChanges, want.text)
		}
	}

	if err := m.Close(mgrAPath); err != nil {
		t.Fatalf("Close a.go: %v", err)
	}
	if got := lsptest.Recv(t, srv.CloseCh, "didClose a.go"); got.TextDocument.URI != mgrAURI {
		t.Fatalf("didClose for %q, want %q", got.TextDocument.URI, mgrAURI)
	}
	if _, ok := m.ClientFor(mgrAPath); ok {
		t.Fatal("ClientFor still reports a.go after Close")
	}
	if _, ok := m.ClientFor(mgrBPath); !ok {
		t.Fatal("closing a.go also forgot b.go")
	}
	if err := m.Change(mgrAPath, "late"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("Change after Close = %v, want ErrNotOpen", err)
	}
	if err := m.Close(mgrAPath); err != nil {
		t.Fatalf("second Close of a.go: %v", err)
	}
}

func TestManagerReopenReplacesDocument(t *testing.T) {
	log := &startLog{t: t}
	m := newLoggedManager(log, goAndMarkdown)
	if err := m.Open(mgrAPath, "go", "one\n"); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	srv := onlyPair(t, log).srv
	lsptest.Recv(t, srv.OpenCh, "first didOpen")

	if err := m.Open(mgrAPath, "go", "two\n"); err != nil {
		t.Fatalf("second Open: %v", err)
	}
	lsptest.Recv(t, srv.CloseCh, "didClose before reopen")
	open := lsptest.Recv(t, srv.OpenCh, "second didOpen")
	if open.TextDocument.Version != 1 || open.TextDocument.Text != "two\n" {
		t.Fatalf("reopened document = version %d text %q, want version 1 text %q", open.TextDocument.Version, open.TextDocument.Text, "two\n")
	}
	if len(log.cfgs) != 1 {
		t.Fatalf("reopen started %d servers, want 1", len(log.cfgs))
	}
}

func TestManagerOpenErrors(t *testing.T) {
	tests := []struct {
		name       string
		servers    map[string]ServerConfig
		configure  func(*startLog)
		languageID string
		wantErr    error
		wantStarts int
		// wantClosedClient is set when the started client must be closed again.
		wantClosedClient bool
	}{
		{
			name:       "language without a server",
			servers:    goAndMarkdown,
			languageID: "rust",
			wantErr:    ErrNoServer,
			wantStarts: 0,
		},
		{
			name:       "start fails",
			servers:    goAndMarkdown,
			configure:  func(l *startLog) { l.fail = errStartFailed },
			languageID: "go",
			wantErr:    errStartFailed,
			wantStarts: 1,
		},
		{
			name:             "server exits during the handshake",
			servers:          goAndMarkdown,
			configure:        func(l *startLog) { l.dead = true },
			languageID:       "go",
			wantStarts:       1,
			wantClosedClient: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &startLog{t: t}
			if tt.configure != nil {
				tt.configure(log)
			}
			m := newLoggedManager(log, tt.servers)

			err := m.Open(mgrAPath, tt.languageID, "x\n")
			if err == nil {
				t.Fatal("Open returned nil error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Open error = %v, want %v", err, tt.wantErr)
			}
			if len(log.cfgs) != tt.wantStarts {
				t.Fatalf("servers started = %d, want %d", len(log.cfgs), tt.wantStarts)
			}
			if _, ok := m.ClientFor(mgrAPath); ok {
				t.Fatal("ClientFor reports a document whose Open failed")
			}
			if tt.wantClosedClient {
				expectClientClosed(t, onlyPair(t, log).client)
			}
		})
	}
}

func TestManagerRealStartNeedsCommand(t *testing.T) {
	// NewManager uses the real lsp.Start. An empty Command fails before any process
	// is launched.
	m := NewManager(mgrRoot, map[string]ServerConfig{"go": {}})
	if err := m.Open(mgrAPath, "go", "x\n"); !errors.Is(err, lsp.ErrNoCommand) {
		t.Fatalf("Open with empty command = %v, want lsp.ErrNoCommand", err)
	}
}

func TestManagerChangeWithoutOpen(t *testing.T) {
	log := &startLog{t: t}
	m := newLoggedManager(log, goAndMarkdown)
	if err := m.Change(mgrAPath, "x\n"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("Change on unopened path = %v, want ErrNotOpen", err)
	}
	if len(log.cfgs) != 0 {
		t.Fatalf("Change started %d servers, want none", len(log.cfgs))
	}
}

func TestManagerShutdownStopsServersAndForgetsDocuments(t *testing.T) {
	log := &startLog{t: t}
	m := newLoggedManager(log, goAndMarkdown)
	if err := m.Open(mgrAPath, "go", "ok\n"); err != nil {
		t.Fatalf("Open a.go: %v", err)
	}
	if err := m.Open(mgrBPath, "go", "ok\n"); err != nil {
		t.Fatalf("Open b.go: %v", err)
	}
	old := onlyPair(t, log).client
	ctx := newTestContext(t)

	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	for _, path := range []string{mgrAPath, mgrBPath} {
		if _, ok := m.ClientFor(path); ok {
			t.Fatalf("ClientFor(%q) still reports a document after Shutdown", path)
		}
	}
	expectClientClosed(t, old)

	if err := m.Open(mgrAPath, "go", "again\n"); err != nil {
		t.Fatalf("Open after Shutdown: %v", err)
	}
	if len(log.cfgs) != 2 {
		t.Fatalf("servers started = %d, want 2 (one before and one after Shutdown)", len(log.cfgs))
	}
}

// onlyPair returns the single server log started, failing the test when the
// count is different.
func onlyPair(t *testing.T, log *startLog) pair {
	t.Helper()
	if len(log.pairs) != 1 {
		t.Fatalf("servers started = %d, want 1", len(log.pairs))
	}
	return log.pairs[0]
}

// expectClientClosed checks that c has closed its connection, which closes the
// Diagnostics channel. Events still queued from earlier publishes are drained
// first.
func expectClientClosed(t *testing.T, c *lsp.Client) {
	t.Helper()
	deadline := time.After(lsptest.Timeout)
	for {
		select {
		case _, ok := <-c.Diagnostics():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("client was not closed")
		}
	}
}

// gatedRWC is the client transport for the Manager tests that need a slow or
// stalled server. Each write waits delay before it is sent. Once stalled is set,
// a write blocks until Close, as a write does when the server stops reading its
// input. stuck receives a value when a stalled write begins.
type gatedRWC struct {
	rwc     io.ReadWriteCloser
	delay   time.Duration
	stalled atomic.Bool
	stuck   chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newGatedRWC(rwc io.ReadWriteCloser, delay time.Duration) *gatedRWC {
	return &gatedRWC{rwc: rwc, delay: delay, stuck: make(chan struct{}, 1), closed: make(chan struct{})}
}

func (g *gatedRWC) Read(p []byte) (int, error) { return g.rwc.Read(p) }

func (g *gatedRWC) Write(p []byte) (int, error) {
	if g.stalled.Load() {
		select {
		case g.stuck <- struct{}{}:
		default:
		}
		<-g.closed
		return 0, io.ErrClosedPipe
	}
	time.Sleep(g.delay)
	return g.rwc.Write(p)
}

func (g *gatedRWC) Close() error {
	g.once.Do(func() { close(g.closed) })
	return g.rwc.Close()
}

// gatedServers is a Manager start hook. Each start runs a fake server behind a
// gatedRWC with the given write delay, and keeps every gate it creates.
type gatedServers struct {
	t     *testing.T
	delay time.Duration
	gates []*gatedRWC
}

func (s *gatedServers) start(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
	p := startPairOver(cfg, nil, func(cli lsptest.PipeRWC) io.ReadWriteCloser {
		g := newGatedRWC(cli, s.delay)
		s.gates = append(s.gates, g)
		return g
	})
	s.t.Cleanup(func() {
		_ = p.client.Close()
		_ = p.srvConn.Close()
	})
	return p.client, nil
}

func TestManagerReplacesServerThatExited(t *testing.T) {
	log := &startLog{t: t}
	m := newLoggedManager(log, goAndMarkdown)
	if err := m.Open(mgrAPath, "go", "one\n"); err != nil {
		t.Fatalf("Open a.go: %v", err)
	}
	first := log.pairs[0]
	lsptest.Recv(t, first.srv.OpenCh, "didOpen a.go")

	// The server exits. Its end of the pipes closes, and the client's read loop ends.
	_ = first.srvEnd.Close()
	select {
	case <-first.client.Done():
	case <-time.After(lsptest.Timeout):
		t.Fatal("client did not notice that the server exited")
	}

	if err := m.Open(mgrBPath, "go", "two\n"); err != nil {
		t.Fatalf("Open b.go after the server exited: %v", err)
	}
	if len(log.pairs) != 2 {
		t.Fatalf("servers started = %d, want 2", len(log.pairs))
	}
	second := log.pairs[1]
	lsptest.Recv(t, second.srv.OpenCh, "didOpen b.go on the new server")
	if c, ok := m.ClientFor(mgrBPath); !ok || c != second.client {
		t.Fatal("ClientFor(b.go) is not the new server's client")
	}
	if _, ok := m.ClientFor(mgrAPath); ok {
		t.Fatal("ClientFor(a.go) reports a document that the exited server held")
	}
	if err := m.Change(mgrAPath, "late\n"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("Change(a.go) after the server exited = %v, want ErrNotOpen", err)
	}
	if err := m.Close(mgrAPath); err != nil {
		t.Fatalf("Close(a.go) after the server exited = %v, want nil", err)
	}
}

func TestStalledWriteDoesNotBlockShutdown(t *testing.T) {
	servers := &gatedServers{t: t}
	m := NewManager(mgrRoot, goAndMarkdown)
	m.start = servers.start
	m.timeout = 100 * time.Millisecond

	if err := m.Open(mgrAPath, "go", "ok\n"); err != nil {
		t.Fatalf("Open a.go: %v", err)
	}
	first, ok := m.ClientFor(mgrAPath)
	if !ok {
		t.Fatal("ClientFor(a.go) reports no client after Open")
	}
	stalled := servers.gates[0]
	stalled.stalled.Store(true)

	// Opening b.go writes didOpen to the server, and that write now blocks.
	openErr := make(chan error, 1)
	go func() { openErr <- m.Open(mgrBPath, "go", "ok\n") }()
	lsptest.Recv(t, stalled.stuck, "didOpen write stalled")

	// Shutdown runs while the write is still blocked, so it must return without
	// waiting for the write.
	shutErr := make(chan error, 1)
	go func() { shutErr <- m.Shutdown(context.Background()) }()
	select {
	case err := <-shutErr:
		if err == nil {
			t.Fatal("Shutdown returned nil although the server's writes were stalled")
		}
	case <-time.After(lsptest.Timeout):
		t.Fatal("Shutdown blocked behind a stalled write")
	}
	select {
	case err := <-openErr:
		if err == nil {
			t.Fatal("Open(b.go) returned nil although its write stalled")
		}
	case <-time.After(lsptest.Timeout):
		t.Fatal("Open(b.go) did not return after its write stalled")
	}
	expectClientClosed(t, first)
	if _, ok := m.ClientFor(mgrAPath); ok {
		t.Fatal("ClientFor(a.go) still reports a document after Shutdown")
	}

	// The stalled server was closed, so the next request starts a new one.
	if err := m.Open(mgrBPath, "go", "again\n"); err != nil {
		t.Fatalf("Open(b.go) after the stalled server was closed: %v", err)
	}
	if len(servers.gates) != 2 {
		t.Fatalf("servers started = %d, want 2", len(servers.gates))
	}
}

func TestEachManagerStepGetsItsOwnDeadline(t *testing.T) {
	// Every write takes 120ms and the timeout is 200ms. Opening a document runs
	// start, initialize, initialized and didOpen, about 360ms in all. Reopening it
	// runs didClose and didOpen, about 240ms in all. Each step is inside its own
	// timeout, so both calls succeed even though the totals exceed the timeout.
	servers := &gatedServers{t: t, delay: 120 * time.Millisecond}
	m := NewManager(mgrRoot, goAndMarkdown)
	m.start = servers.start
	m.timeout = 200 * time.Millisecond

	if err := m.Open(mgrAPath, "go", "one\n"); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := m.Open(mgrAPath, "go", "two\n"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestShutdownDuringStartClosesTheNewServer(t *testing.T) {
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	started := make(chan *lsp.Client, 1)

	m := NewManager(mgrRoot, goAndMarkdown)
	m.start = func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		p := startPair(cfg, nil)
		t.Cleanup(func() {
			_ = p.client.Close()
			_ = p.srvConn.Close()
		})
		started <- p.client
		<-release
		return p.client, nil
	}

	openErr := make(chan error, 1)
	go func() { openErr <- m.Open(mgrAPath, "go", "x\n") }()
	server := lsptest.Recv(t, started, "server start")

	// Shutdown must not wait for the start, and must not see the server, which
	// has not been recorded yet.
	shutErr := make(chan error, 1)
	go func() { shutErr <- m.Shutdown(newTestContext(t)) }()
	select {
	case err := <-shutErr:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(lsptest.Timeout):
		t.Fatal("Shutdown blocked while a server was starting")
	}

	releaseOnce()
	select {
	case err := <-openErr:
		if err == nil {
			t.Fatal("Open returned nil although Shutdown ran while its server was starting")
		}
	case <-time.After(lsptest.Timeout):
		t.Fatal("Open did not return after the start finished")
	}
	expectClientClosed(t, server)
	if _, ok := m.ClientFor(mgrAPath); ok {
		t.Fatal("ClientFor(a.go) reports a document after Shutdown")
	}
}

// TestSlowServerStartDoesNotBlockChangeOrClose is the regression for a start that
// held the send lock. While a second language's server is starting, a change and
// a close of a document on the first server must still go out at once.
func TestSlowServerStartDoesNotBlockChangeOrClose(t *testing.T) {
	tests := []struct {
		name string
		op   func(m *Manager) error
	}{
		{"change", func(m *Manager) error { return m.Change(mgrAPath, "edited\n") }},
		{"close", func(m *Manager) error { return m.Close(mgrAPath) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			releaseOnce := sync.OnceFunc(func() { close(release) })
			t.Cleanup(releaseOnce)

			m := NewManager(mgrRoot, goAndMarkdown)
			m.start = func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
				if slices.Equal(cfg.Command, []string{"marksman"}) {
					entered <- struct{}{}
					<-release
				}
				p := startPair(cfg, nil)
				t.Cleanup(func() {
					_ = p.client.Close()
					_ = p.srvConn.Close()
				})
				return p.client, nil
			}
			if err := m.Open(mgrAPath, "go", "ok\n"); err != nil {
				t.Fatalf("Open a.go: %v", err)
			}

			openErr := make(chan error, 1)
			go func() { openErr <- m.Open("/work/proj/README.md", "markdown", "# hi\n") }()
			lsptest.Recv(t, entered, "markdown server start")

			opErr := make(chan error, 1)
			go func() { opErr <- tt.op(m) }()
			select {
			case err := <-opErr:
				if err != nil {
					t.Fatalf("%s of a.go during another server's start: %v", tt.name, err)
				}
			case <-time.After(lsptest.Timeout):
				t.Fatalf("%s of a.go blocked behind another server's start", tt.name)
			}

			releaseOnce()
			select {
			case err := <-openErr:
				if err != nil {
					t.Fatalf("Open README.md after its start: %v", err)
				}
			case <-time.After(lsptest.Timeout):
				t.Fatal("Open README.md did not return after its start finished")
			}
		})
	}
}

// TestConcurrentOpensShareOneServerStart checks that opens for one language which
// arrive during its start wait for that start, rather than starting a second server.
func TestConcurrentOpensShareOneServerStart(t *testing.T) {
	var starts atomic.Int32
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)

	m := NewManager(mgrRoot, goAndMarkdown)
	m.start = func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		starts.Add(1)
		<-release
		p := startPair(cfg, nil)
		t.Cleanup(func() {
			_ = p.client.Close()
			_ = p.srvConn.Close()
		})
		return p.client, nil
	}

	first := make(chan error, 1)
	go func() { first <- m.Open(mgrAPath, "go", "a\n") }()
	for starts.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	second := make(chan error, 1)
	go func() { second <- m.Open(mgrBPath, "go", "b\n") }()
	time.Sleep(20 * time.Millisecond)
	releaseOnce()

	for _, ch := range []chan error{first, second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("concurrent Open: %v", err)
			}
		case <-time.After(lsptest.Timeout):
			t.Fatal("concurrent Open did not return")
		}
	}
	if n := starts.Load(); n != 1 {
		t.Fatalf("servers started = %d, want 1 shared by both opens", n)
	}
	for _, p := range []string{mgrAPath, mgrBPath} {
		if _, ok := m.ClientFor(p); !ok {
			t.Fatalf("ClientFor(%s) reports no document after its Open", p)
		}
	}
}
