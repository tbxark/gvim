package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/trust"
)

// waitLSPBaseline waits until the serialized baseline worker applied the latest
// reload/trust state. App tests use it to observe the async worker without sleep loops in
// production code.
func waitLSPBaseline(t *testing.T, got built) {
	t.Helper()
	s, ok := settingsPort(t, got).(*settingsService)
	if !ok || s.lspBase == nil {
		return
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.lspBase.Applied() >= s.lspBase.Seq() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("LSP baseline did not apply: applied %d seq %d", s.lspBase.Applied(), s.lspBase.Seq())
}

func TestTrustGrantStartsExistingManagerAndRespectsDisabledServers(t *testing.T) {
	for _, tc := range []struct {
		name, config, project, command string
		want                           int
	}{
		{"enabled", "{}", "", "gopls", 1},
		{"globally disabled", `{"lsp_enabled":false}`, "", "", 0},
		{"language disabled", `{"lsp":{"go":{"enabled":false}}}`, "", "", 0},
		{"project disabled", "{}", `{"lsp":{"go":{"enabled":false}}}`, "", 0},
		{"project server dormant", "{}", `{"lsp":{"go":{"auto_start":false}}}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			user := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(user, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.project != "" {
				writeTestFile(t, filepath.Join(root, ".gvim", "config.json"), tc.project)
			}
			store := useTrustStore(t)
			if err := store.Set(root, trust.Restricted); err != nil {
				t.Fatal(err)
			}
			prev := newLSP
			var port tui.LSPSession
			newLSP = func(cfg config.Config) tui.LSPSession { port = prev(cfg); return port }
			t.Cleanup(func() { newLSP = prev })
			got, err := buildCaptured(t, Options{Root: root, Config: user, Simple: true, NoPlugins: true, SessionDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			spawned := 0
			var command string
			port.(interface {
				SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
			}).SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
				spawned++
				command = cfg.Command[0]
				return nil, context.Canceled
			})
			port.SetRoot(root)
			_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a")
			if spawned != 0 {
				t.Fatalf("restricted spawned %d servers", spawned)
			}
			if _, err := got.deps.Trust.Decide(trust.Trusted); err != nil {
				t.Fatal(err)
			}
			// No baseline wait: trust takes effect even when its worker is delayed.
			_, _ = port.Open(filepath.Join(root, "b.go"), "go", "package b")
			if spawned != tc.want || command != tc.command {
				t.Fatalf("after trust spawned %d servers with command %q, want %d with %q", spawned, command, tc.want, tc.command)
			}
		})
	}
}

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
			waitLSPBaseline(t, got)
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
	waitLSPBaseline(t, got)
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
	waitLSPBaseline(t, got)
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	if spawned != 1 {
		t.Fatalf("Restricted Open of a never-run language spawned %d server(s)", spawned-1)
	}
}

// TestTrustedManualStartDoesNotSurviveRevoke checks the forced-state audit: a language
// manually started while Trusted must not keep bypassing the auto-start gate after a revoke,
// for the existing manager and for a new root manager created later.
func TestTrustedManualStartDoesNotSurviveRevoke(t *testing.T) {
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
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n") // trusted auto
	if spawned != 1 {
		t.Fatalf("trusted auto start spawned %d, want 1", spawned)
	}
	// Manual start under Trusted creates the forced state.
	_ = port.StartLanguage("go")
	if spawned != 2 {
		t.Fatalf("manual trusted start spawned %d, want 2", spawned)
	}
	tc, ok := got.deps.Trust.(*trustController)
	if !ok {
		t.Fatal("the runtime trust controller is not wired")
	}
	if _, err := tc.Decide(trust.Restricted); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	waitLSPBaseline(t, got)
	// Same manager: forced cleared, live auto gate closed.
	_, _ = port.Open(filepath.Join(root, "b.go"), "go", "package b\n")
	if spawned != 2 {
		t.Fatalf("existing manager started %d server(s) after revoke", spawned-2)
	}
	// New root manager: port-level forced cleared, so it is not forced either.
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "go.mod"), []byte("module sub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = port.Open(filepath.Join(sub, "a.go"), "go", "package a\n")
	if spawned != 2 {
		t.Fatalf("new root manager started %d server(s) after revoke", spawned-2)
	}
}
