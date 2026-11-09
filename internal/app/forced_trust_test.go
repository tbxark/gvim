package app

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/trust"
)

// clearForcedLSP wraps the real LSP port so a test can count ClearForced while forwarding
// the optional host seams the app wires at build time (a plain embedded interface would
// hide them from the app's type assertions).
type clearForcedLSP struct {
	tui.LSPSession
	clearCalls atomic.Int32
}

func (p *clearForcedLSP) ClearForced() {
	p.clearCalls.Add(1)
	if c, ok := p.LSPSession.(interface{ ClearForced() }); ok {
		c.ClearForced()
	}
}

func (p *clearForcedLSP) SetSpawnEnv(fn func() []string) {
	if s, ok := p.LSPSession.(interface{ SetSpawnEnv(func() []string) }); ok {
		s.SetSpawnEnv(fn)
	}
}

func (p *clearForcedLSP) SetSpawnGate(fn func(context.Context, []string, string, string) error) {
	if s, ok := p.LSPSession.(interface {
		SetSpawnGate(func(context.Context, []string, string, string) error)
	}); ok {
		s.SetSpawnGate(fn)
	}
}

func (p *clearForcedLSP) SetAutoStartGate(fn func() bool) {
	if s, ok := p.LSPSession.(interface{ SetAutoStartGate(func() bool) }); ok {
		s.SetAutoStartGate(fn)
	}
}

func (p *clearForcedLSP) SetConfig(cfg config.Config) {
	if s, ok := p.LSPSession.(interface{ SetConfig(config.Config) }); ok {
		s.SetConfig(cfg)
	}
}

// TestTrustListenerClearsForcedAndKeepsManualStart checks the live-trust hook: every trust
// change reaches ClearForced, a forced launch from the Trusted state cannot survive the
// revoke, and an explicit StartLanguage after restoring Trusted still starts the server.
func TestTrustListenerClearsForcedAndKeepsManualStart(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	user := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(user, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := useTrustStore(t)
	if err := store.Set(root, trust.Trusted); err != nil {
		t.Fatal(err)
	}
	prev := newLSP
	var spy *clearForcedLSP
	newLSP = func(cfg config.Config) tui.LSPSession {
		base := prev(cfg)
		spy = &clearForcedLSP{LSPSession: base}
		return spy
	}
	t.Cleanup(func() { newLSP = prev })
	got, err := buildCaptured(t, Options{Root: root, Config: user, Simple: true, NoPlugins: true, SessionDir: t.TempDir()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if spy == nil {
		t.Fatal("the LSP port was not built")
	}
	tc, ok := got.deps.Trust.(*trustController)
	if !ok {
		t.Fatal("the runtime trust controller is not wired")
	}
	spawned := 0
	if base, ok := spy.LSPSession.(interface {
		SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
	}); ok {
		base.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
			spawned++
			return nil, context.Canceled
		})
	} else {
		t.Fatal("the LSP port has no spawner seam")
	}
	spy.SetRoot(root)
	// Revoke; the listener must call ClearForced and a manager created under Blocked must
	// not auto-start.
	if _, err := tc.Decide(trust.Blocked); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if spy.clearCalls.Load() == 0 {
		t.Fatal("the trust listener did not call ClearForced on revoke")
	}
	_, _ = spy.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if spawned != 0 {
		t.Fatalf("manager created under Blocked spawned %d server(s)", spawned)
	}
	// Restore Trusted: another clear (harmless), then an explicit manual start still works.
	before := spy.clearCalls.Load()
	if _, err := tc.Decide(trust.Trusted); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if spy.clearCalls.Load() <= before {
		t.Fatal("the trust listener did not call ClearForced on restore")
	}
	_ = spy.StartLanguage("go")
	if spawned != 1 {
		t.Fatalf("manual StartLanguage after restore spawned %d server(s), want 1", spawned)
	}
}
