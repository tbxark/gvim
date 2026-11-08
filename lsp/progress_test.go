package lsp

import (
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/lsp/lsptest"
)

// waitProgress waits until fn reports true, which the notification's handling on
// the client read goroutine finishes asynchronously.
func waitProgress(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(lsptest.Timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("progress state did not arrive")
}

// progressParams builds a $/progress notification for the fake server to send.
func progressParams(token protocol.ProgressToken, value string) *protocol.ProgressParams {
	return &protocol.ProgressParams{Token: token, Value: protocol.LSPAny(value)}
}

// TestProgressBeginReportEnd checks the $/progress lifecycle: begin adds an entry,
// report updates its message and percentage, end removes it.
func TestProgressBeginReportEnd(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := f.srv.CreateProgress(ctx, &protocol.WorkDoneProgressCreateParams{Token: protocol.String("idx")}); err != nil {
		t.Fatalf("create progress: %v", err)
	}
	if err := f.srv.SendProgress(ctx, progressParams(protocol.String("idx"), `{"kind":"begin","title":"Indexing","percentage":0}`)); err != nil {
		t.Fatalf("begin: %v", err)
	}
	waitProgress(t, func() bool { return len(f.client.Progress()) == 1 })
	got := f.client.Progress()
	if len(got) != 1 || got[0].Title != "Indexing" || got[0].Percentage != 0 || got[0].Message != "" {
		t.Fatalf("progress after begin = %+v", got)
	}
	if err := f.srv.SendProgress(ctx, progressParams(protocol.String("idx"), `{"kind":"report","message":"crates","percentage":42}`)); err != nil {
		t.Fatalf("report: %v", err)
	}
	waitProgress(t, func() bool {
		got := f.client.Progress()
		return len(got) == 1 && got[0].Message == "crates"
	})
	got = f.client.Progress()
	if len(got) != 1 || got[0].Message != "crates" || got[0].Percentage != 42 {
		t.Fatalf("progress after report = %+v", got)
	}
	if err := f.srv.SendProgress(ctx, progressParams(protocol.String("idx"), `{"kind":"end"}`)); err != nil {
		t.Fatalf("end: %v", err)
	}
	waitProgress(t, func() bool { return len(f.client.Progress()) == 0 })
}

// TestProgressTokensAndCap checks the token forms and the cap: a numeric token works
// like a string one, and more than maxProgress entries keep only the newest.
func TestProgressTokensAndCap(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := f.srv.SendProgress(ctx, progressParams(protocol.Integer(7), `{"kind":"begin","title":"seven"}`)); err != nil {
		t.Fatalf("numeric begin: %v", err)
	}
	waitProgress(t, func() bool { return len(f.client.Progress()) > 0 })
	if got := f.client.Progress(); len(got) != 1 || got[0].Token != "7" || got[0].Title != "seven" {
		t.Fatalf("numeric token entry = %+v", got)
	}
	for i := range maxProgress + 2 {
		token := protocol.String(string(rune('a' + i)))
		value := `{"kind":"begin","title":"task"}`
		if err := f.srv.SendProgress(ctx, progressParams(token, value)); err != nil {
			t.Fatalf("begin %d: %v", i, err)
		}
	}
	waitProgress(t, func() bool { return len(f.client.Progress()) == maxProgress })
	got := f.client.Progress()
	if len(got) != maxProgress {
		t.Fatalf("progress entries = %d, want the cap %d", len(got), maxProgress)
	}
	for _, p := range got {
		if p.Title != "task" {
			t.Fatalf("cap kept %+v, want the newest entries", p)
		}
	}
	// A report for an unknown token does nothing; an end for one is harmless.
	if err := f.srv.SendProgress(ctx, progressParams(protocol.String("missing"), `{"kind":"report","message":"x"}`)); err != nil {
		t.Fatalf("unknown report: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if got := len(f.client.Progress()); got != maxProgress {
		t.Fatalf("entries = %d after an unknown report, want %d", got, maxProgress)
	}
}

// TestProgressFromTheReadLoop checks the progress handlers answer on the reader
// path: the fake server's calls return without the client's read loop stopping.
func TestProgressFromTheReadLoop(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)
	if _, err := f.client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// Two requests from the server in a row, each answered before the next: the
	// handlers never wait on the UI.
	for range 2 {
		if err := f.srv.CreateProgress(ctx, &protocol.WorkDoneProgressCreateParams{Token: protocol.String("t")}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	default:
	}
	_ = lsptest.Timeout
}
