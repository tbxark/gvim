package lsphost

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
)

// reloadTypescript applies a settings reload that replaces the typescript servers.
func reloadTypescript(port *lspPort, servers ...config.LSPServer) {
	cfg := config.Defaults()
	cfg.LSP = map[string][]config.LSPServer{"typescript": servers}
	port.SetConfig(cfg)
}

// TestStartLanguageLeavesOtherLanguagesAlone checks applying a changed plan for the target
// language does not stop or restart another language's servers.
func TestStartLanguageLeavesOtherLanguagesAlone(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	_, _ = port.Open(filepath.Join(root, "a.js"), "javascript", "const x = 1\n")
	before := len(launched)
	reloadTypescript(port, config.LSPServer{Command: "new-ts"})
	_ = port.StartLanguage("typescript")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "new-ts") {
		t.Fatalf("target language start = %v", launched)
	}
}

// TestRestartServerAppliesChangedPlan checks the settings-style Restart Server path also
// applies a changed server plan to an existing manager.
func TestRestartServerAppliesChangedPlan(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	before := len(launched)
	reloadTypescript(port, config.LSPServer{Command: "new-ts"})
	_ = port.RestartServer("typescript", "")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "new-ts") {
		t.Fatalf("explicit RestartServer did not apply the changed plan: %v", launched)
	}
}

// TestRestartServerMultiServerReplacement checks the documented multi-server semantics: when
// the plan differs, Restart Server on any row replaces the language's whole server list with
// the new plan (the row name is not used).
func TestRestartServerMultiServerReplacement(t *testing.T) {
	cfg := config.Defaults()
	cfg.LSP = map[string][]config.LSPServer{
		"typescript": {{Command: "ts-one"}, {Command: "ts-two"}},
	}
	port := NewFor(cfg).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	before := len(launched)
	reloadTypescript(port, config.LSPServer{Command: "new-ts"})
	_ = port.RestartServer("typescript", "ts-two")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "new-ts") {
		t.Fatalf("multi-server replacement launched %v (before %d), want only the new-ts singleton", launched, before)
	}
}

// TestSetConfigAndExplicitStartRace runs plan reloads concurrently with the explicit start
// path; selectionMu must keep each snapshot consistent (run with -race too).
func TestSetConfigAndExplicitStartRace(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) { return nil, errors.New("captured") })
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			reloadTypescript(port, config.LSPServer{Command: "new-ts", Dormant: true})
			port.SetConfig(config.Defaults())
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			_ = port.StartLanguage("typescript")
		}
	}()
	wg.Wait()
	reloadTypescript(port, config.LSPServer{Command: "new-ts", Dormant: true})
	_ = port.StartLanguage("typescript")
	got := port.managers[root].CurrentServers("typescript")
	if len(got) != 1 || got[0].Command[0] != "new-ts" {
		t.Fatalf("final config = %+v, want the last reloaded server", got)
	}
}

// TestSetConfigUpdatesBaselineForExplicitStart checks a settings reload updates the server
// baseline even though the LSP port was built with the startup config: the next explicit
// Start sees the new explicit server instead of the old built-in one, and reload itself does
// not touch the running manager.
func TestSetConfigUpdatesBaselineForExplicitStart(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	before := len(launched)
	reloaded := config.Defaults()
	reloaded.LSP = map[string][]config.LSPServer{"typescript": {{Command: "new-ts"}}}
	port.SetConfig(reloaded)
	if len(launched) != before {
		t.Fatalf("SetConfig restarted a running manager: %v", launched)
	}
	_ = port.StartLanguage("typescript")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "new-ts") {
		t.Fatalf("explicit Start after SetConfig = %v, want the reloaded new-ts baseline", launched)
	}
}

// TestReplaceServersPreflightKeepsOldServerOnTrustRefusal checks the trust-revocation side
// effect: when the new plan's spawn gate refuses, an explicit Start must fail before
// stopping/replacing the existing language servers, so the old server/config survives.
func TestReplaceServersPreflightKeepsOldServerOnTrustRefusal(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	before := len(launched)
	reloadTypescript(port, config.LSPServer{Command: "new-ts", Dormant: true})
	port.SetSpawnGate(func(context.Context, []string, string, string) error {
		return errors.New("workspace trust: Blocked")
	})
	err := port.StartLanguage("typescript")
	if err == nil || !strings.Contains(err.Error(), "Blocked") {
		t.Fatalf("StartLanguage error = %v, want the trust refusal", err)
	}
	if len(launched) != before {
		t.Fatalf("refused replacement launched/spawned something: %v", launched)
	}
	mgr := port.managers[root]
	if mgr == nil || len(mgr.CurrentServers("typescript")) != 1 || mgr.CurrentServers("typescript")[0].Command[0] == "new-ts" {
		t.Fatalf("old manager config was replaced despite the preflight refusal: %+v", mgr)
	}
}

// TestAutoStartGateStopsNewLanguageOnExistingManager checks the app/port regression: a
// manager created while trusted has AutoStart configs for several languages; after the live
// gate changes to Restricted (AutoStartGate false), opening a new language that never ran
// must not spawn a server, and a manual Start for it must still work.
func TestAutoStartGateStopsNewLanguageOnExistingManager(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	// Create the manager while trusted; only go starts.
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if len(launched) != 1 || !strings.Contains(launched[0], "gopls") {
		t.Fatalf("trusted manager start = %v, want one gopls", launched)
	}
	// Live Restricted: the existing manager's old AutoStart configs must not start a
	// never-run language on Open.
	port.SetAutoStartGate(func() bool { return false })
	before := len(launched)
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	if len(launched) != before {
		t.Fatalf("Open of a new language under Restricted spawned: %v", launched)
	}
	// Manual Start still starts it through the spawn gate (nil here).
	// The fake spawner fails on purpose; the launch is what this test observes.
	_ = port.StartLanguage("typescript")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "typescript-language-server") {
		t.Fatalf("manual start after revoke = %v, want the configured TS server", launched)
	}
}
