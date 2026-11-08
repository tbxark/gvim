package lsp

import (
	"testing"

	"github.com/tbxark/gvim/lsp/lsptest"
)

func TestDiagnosticsForTracksLatestPerURI(t *testing.T) {
	const (
		mainURI  = "file:///work/proj/main.go"
		otherURI = "file:///work/proj/other.go"
	)
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)

	if _, ok := f.client.DiagnosticsFor(mainURI); ok {
		t.Fatal("DiagnosticsFor reported a snapshot before any publish")
	}

	if err := f.client.DidOpen(ctx, mainURI, "go", 1, "ERR one\nERR two\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	lsptest.Recv(t, f.client.Diagnostics(), "open event")
	ev, ok := f.client.DiagnosticsFor(mainURI)
	if !ok || ev.Version != 1 || len(ev.Diagnostics) != 2 {
		t.Fatalf("snapshot after open = %+v, %v; want version 1 with 2 diagnostics", ev, ok)
	}

	if err := f.client.DidOpen(ctx, otherURI, "go", 5, "ok\n"); err != nil {
		t.Fatalf("DidOpen other: %v", err)
	}
	lsptest.Recv(t, f.client.Diagnostics(), "other open event")
	if ev, ok := f.client.DiagnosticsFor(otherURI); !ok || ev.Version != 5 || len(ev.Diagnostics) != 0 {
		t.Fatalf("snapshot for other = %+v, %v; want version 5 with no diagnostics", ev, ok)
	}
	if ev, _ := f.client.DiagnosticsFor(mainURI); len(ev.Diagnostics) != 2 {
		t.Fatalf("main snapshot changed by other document: %d diagnostics", len(ev.Diagnostics))
	}

	if err := f.client.DidChange(ctx, mainURI, 2, "ok\n"); err != nil {
		t.Fatalf("DidChange: %v", err)
	}
	lsptest.Recv(t, f.client.Diagnostics(), "change event")
	ev, ok = f.client.DiagnosticsFor(mainURI)
	if !ok || ev.Version != 2 || len(ev.Diagnostics) != 0 {
		t.Fatalf("snapshot after clean change = %+v, %v; want version 2 cleared", ev, ok)
	}
}

func TestDiagnosticsForSurvivesDroppedEvents(t *testing.T) {
	const docURI = "file:///work/proj/busy.go"
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)

	const total = diagnosticsBuffer + 36
	for v := int32(1); v <= total; v++ {
		if err := f.client.DidChange(ctx, docURI, v, "ERR\n"); err != nil {
			t.Fatalf("DidChange %d: %v", v, err)
		}
	}
	// Shutdown is answered after every earlier notification, so the snapshot is
	// complete when it returns.
	if err := f.client.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	ev, ok := f.client.DiagnosticsFor(docURI)
	if !ok || ev.Version != total || len(ev.Diagnostics) != 1 {
		t.Fatalf("snapshot = version %d (%d diagnostics, ok %v), want latest version %d", ev.Version, len(ev.Diagnostics), ok, total)
	}
}

func TestDiagnosticsForReturnsCopy(t *testing.T) {
	const docURI = "file:///work/proj/copy.go"
	f := newFixture(t, Config{}, nil)
	ctx := newTestContext(t)

	if err := f.client.DidOpen(ctx, docURI, "go", 1, "ERR\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	lsptest.Recv(t, f.client.Diagnostics(), "open event")

	ev, _ := f.client.DiagnosticsFor(docURI)
	if len(ev.Diagnostics) != 1 {
		t.Fatalf("snapshot has %d diagnostics, want 1", len(ev.Diagnostics))
	}
	ev.Diagnostics[0].Message = nil
	ev.Diagnostics = ev.Diagnostics[:0]

	again, _ := f.client.DiagnosticsFor(docURI)
	if len(again.Diagnostics) != 1 || again.Diagnostics[0].Message == nil {
		t.Fatalf("snapshot changed through a returned value: %+v", again.Diagnostics)
	}
}
