package lsp

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/lsp/lsptest"
)

// newCancelableFixture is newFixture over a fake server started with the standard LSP
// handler chain, so a $/cancelRequest reaches a running handler's context.
func newCancelableFixture(t *testing.T, cfg Config) pair {
	t.Helper()
	cliEnd, srvEnd := lsptest.NewPipePair()
	srv, srvConn := lsptest.StartServerCancelable(srvEnd, nil)
	p := pair{client: NewClient(cliEnd, cfg), srvConn: srvConn, srv: srv, srvEnd: srvEnd}
	t.Cleanup(func() {
		_ = p.client.Close()
		_ = p.srvConn.Close()
	})
	return p
}

// workspaceItem is one canned workspace report for the tests below.
func workspaceItem(t *testing.T) []protocol.WorkspaceDocumentDiagnosticReport {
	t.Helper()
	return []protocol.WorkspaceDocumentDiagnosticReport{
		&protocol.WorkspaceFullDocumentDiagnosticReport{
			FullDocumentDiagnosticReport: protocol.FullDocumentDiagnosticReport{
				Kind:  string(protocol.DocumentDiagnosticReportKindFull),
				Items: []protocol.Diagnostic{{Message: protocol.String("later")}},
			},
			URI: "file:///work/proj/held.go",
		},
	}
}

// TestWorkspaceDiagnosticsCancelSendsCancelRequest checks the workspace pull's
// cancellation: a held workspace/diagnostic request is canceled by the caller, the
// client returns context.Canceled promptly, the fake server sees its request context
// canceled (the $/cancelRequest reached it), and a later pull on the same client works,
// so the canceled request leaves nothing behind.
func TestWorkspaceDiagnosticsCancelSendsCancelRequest(t *testing.T) {
	f := newCancelableFixture(t, Config{})
	f.srv.Canned.WorkspaceItems = workspaceItem(t)
	f.srv.WorkspaceHold = make(chan struct{})

	callCtx, cancel := context.WithCancel(context.Background())
	type result struct {
		items []WorkspaceDiagnosticItem
		err   error
	}
	done := make(chan result, 1)
	go func() {
		items, err := f.client.WorkspaceDiagnostics(callCtx, nil)
		done <- result{items, err}
	}()
	lsptest.Recv(t, f.srv.WorkspaceEntered, "the workspace pull entered")

	cancel()
	got := <-done
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("WorkspaceDiagnostics error = %v, want context.Canceled", got.err)
	}
	if got.items != nil {
		t.Fatalf("canceled pull returned %+v, want no items", got.items)
	}
	lsptest.Recv(t, f.srv.WorkspaceCanceled, "the server saw the request canceled")

	// Release the held handler: it answers a request that no longer exists, and the
	// answer must not reach anything.
	close(f.srv.WorkspaceHold)
	f.srv.WorkspaceHold = nil
	// A fresh pull on the same client answers normally.
	items, err := f.client.WorkspaceDiagnostics(newTestContext(t), nil)
	if err != nil {
		t.Fatalf("the pull after a canceled one: %v", err)
	}
	if len(items) != 1 || items[0].URI != "file:///work/proj/held.go" {
		t.Fatalf("items after a canceled pull = %+v", items)
	}
}

// TestWorkspaceDiagnosticsDeadlineCancelsThePull checks the timeout fallback: a pull
// that outlives its deadline is canceled on the wire too, with DeadlineExceeded for
// the caller.
func TestWorkspaceDiagnosticsDeadlineCancelsThePull(t *testing.T) {
	f := newCancelableFixture(t, Config{})
	f.srv.WorkspaceHold = make(chan struct{})
	callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := f.client.WorkspaceDiagnostics(callCtx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WorkspaceDiagnostics error = %v, want context.DeadlineExceeded", err)
	}
	lsptest.Recv(t, f.srv.WorkspaceCanceled, "the server saw the deadline cancellation")
	close(f.srv.WorkspaceHold)
}
