package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/trust"
)

// TestRestrictedReloadDoesNotBypassTrustAutoStart checks the settings-reload hook against
// live trust: a Restricted/Blocked workspace reloading a user config with lsp_enabled true
// and an lsp entry must not install an auto-starting baseline (the raw reloaded config would
// bypass lspConfig). A new manager after that reload must not spawn a server on Open.
func TestRestrictedReloadDoesNotBypassTrustAutoStart(t *testing.T) {
	for _, level := range []trust.Level{trust.Restricted, trust.Blocked} {
		t.Run(level.String(), func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			user := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(user, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			store := useTrustStore(t)
			if err := store.Set(root, level); err != nil {
				t.Fatal(err)
			}
			prev := newLSP
			var port tui.LSPSession
			newLSP = func(cfg config.Config) tui.LSPSession {
				port = prev(cfg)
				return port
			}
			t.Cleanup(func() { newLSP = prev })
			got, err := buildCaptured(t, Options{Root: root, Config: user, Simple: true, NoPlugins: true, SessionDir: t.TempDir()})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if port == nil {
				t.Fatal("the LSP port was not built")
			}
			spawned := 0
			if setter, ok := port.(interface {
				SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
			}); ok {
				setter.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
					spawned++
					return nil, context.Canceled
				})
			}
			if err := os.WriteFile(user, []byte("{\"lsp_enabled\":true,\"lsp\":{\"go\":{\"command\":\"gopls\"}}}"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := settingsPort(t, got).Reload(); err != nil {
				t.Fatalf("settings reload: %v", err)
			}
			if port.Supports("go") {
				t.Fatal("a Restricted/Blocked reload installed an auto-starting go baseline")
			}
			port.SetRoot(root)
			_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
			if spawned != 0 {
				t.Fatalf("a new manager spawned %d server(s) under %s after reload", spawned, level)
			}
		})
	}
}

// TestTrustRevokeRegatesLSPBaseline checks the live trust listener: revoking a Trusted
// session re-gates the LSP baseline (no auto-start), so a manager created afterwards cannot
// spawn a server on Open.
func TestTrustRevokeRegatesLSPBaseline(t *testing.T) {
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
	var port tui.LSPSession
	newLSP = func(cfg config.Config) tui.LSPSession {
		port = prev(cfg)
		return port
	}
	t.Cleanup(func() { newLSP = prev })
	got, err := buildCaptured(t, Options{Root: root, Config: user, Simple: true, NoPlugins: true, SessionDir: t.TempDir()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if port == nil {
		t.Fatal("the LSP port was not built")
	}
	spawned := 0
	if setter, ok := port.(interface {
		SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
	}); ok {
		setter.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
			spawned++
			return nil, context.Canceled
		})
	}
	tc, ok := got.deps.Trust.(*trustController)
	if !ok {
		t.Fatal("the runtime trust controller is not wired")
	}
	// Create the manager while the workspace is Trusted: its config says AutoStart true.
	port.SetRoot(root)
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if spawned != 1 {
		t.Fatalf("trusted manager start spawned %d server(s), want 1", spawned)
	}
	if _, err := tc.Decide(trust.Blocked); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if port.Supports("go") {
		t.Fatal("the live trust revoke left an auto-starting baseline")
	}
	// The existing manager still carries its old AutoStart config; the live auto-start
	// gate must stop Open from spawning, while its process/handle state is left alone.
	_, _ = port.Open(filepath.Join(root, "b.go"), "go", "package b\n")
	if spawned != 1 {
		t.Fatalf("Open after revoke spawned %d extra server(s)", spawned-1)
	}
}

// TestRestrictedRevokeStopsNewLanguageAutoStart checks the app-level regression: a manager
// created while Trusted has AutoStart configs for languages that never ran; after a revoke to
// Restricted, opening one of those languages must not spawn a server.
func TestRestrictedRevokeStopsNewLanguageAutoStart(t *testing.T) {
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
	var port tui.LSPSession
	newLSP = func(cfg config.Config) tui.LSPSession {
		port = prev(cfg)
		return port
	}
	t.Cleanup(func() { newLSP = prev })
	got, err := buildCaptured(t, Options{Root: root, Config: user, Simple: true, NoPlugins: true, SessionDir: t.TempDir()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if port == nil {
		t.Fatal("the LSP port was not built")
	}
	spawned := 0
	if setter, ok := port.(interface {
		SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
	}); ok {
		setter.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
			spawned++
			return nil, context.Canceled
		})
	}
	port.SetRoot(root)
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if spawned != 1 {
		t.Fatalf("trusted manager start spawned %d server(s), want 1", spawned)
	}
	tc, ok := got.deps.Trust.(*trustController)
	if !ok {
		t.Fatal("the runtime trust controller is not wired")
	}
	if _, err := tc.Decide(trust.Restricted); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	if spawned != 1 {
		t.Fatalf("Restricted Open of a never-run language spawned %d server(s)", spawned-1)
	}
}
