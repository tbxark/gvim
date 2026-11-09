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
	"github.com/tbxark/gvim/lsp/manager"
)

// TestRootServerProviderAppliesToExactRootOnly checks the LSP-port seam used by Deno
// auto-selection: the provider replaces a language config only for the exact root and the
// baseline returns when it is cleared; another root keeps the configured server.
func TestRootServerProviderAppliesToExactRootOnly(t *testing.T) {
	port, ok := NewFor(config.Defaults()).(*lspPort)
	if !ok {
		t.Fatal("NewFor did not return the lspPort")
	}
	root, other := "/remote/deno-proj", "/remote/other-proj"
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno"}, true
	})
	wantDeno := func(t *testing.T, got []manager.ServerConfig) {
		t.Helper()
		if len(got) != 1 || got[0].Command[0] != "deno" || got[0].Name != "deno" {
			t.Fatalf("typescript configs = %+v, want the provider's deno config", got)
		}
	}
	wantDeno(t, port.serverConfigsFor(root)["typescript"])
	baseline := port.serverConfigsFor(other)["typescript"]
	if len(baseline) != 1 || baseline[0].Command[0] == "deno" {
		t.Fatalf("other root typescript configs = %+v, want the configured server", baseline)
	}
	port.ClearRootServerProvider(root)
	cleared := port.serverConfigsFor(root)["typescript"]
	if len(cleared) != 1 || cleared[0].Command[0] == "deno" {
		t.Fatalf("cleared root typescript configs = %+v, want the baseline server", cleared)
	}
}

// TestRootServerProviderOnlyAnswersExistingLanguages checks a provider cannot invent a
// language entry absent from the plan.
func TestRootServerProviderOnlyAnswersExistingLanguages(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	port.SetRootServerProvider("/w", func(string) (manager.ServerConfig, bool) {
		return manager.ServerConfig{Command: []string{"unexpected"}}, true
	})
	for lang := range port.serverConfigsFor("/w") {
		if lang == "unexpected" {
			t.Fatal("provider invented a language entry")
		}
	}
}

// TestRootServerProviderReachesManager checks the provider actually reaches the manager: an
// auto_start:false Deno override does not start on Open, and a manual StartLanguage launches
// the deno command (captured by a fake spawner).
func TestRootServerProviderReachesManager(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{
			Command:   []string{"deno"},
			Args:      []string{"lsp"},
			Name:      "deno",
			AutoStart: false,
		}, true
	})
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	path := filepath.Join(root, "a.ts")
	// AutoStart false means Open records the document but reports no running server; that is
	// the intended no-silent-spawn behaviour, not a failure of the provider.
	_, _ = port.Open(path, "typescript", "const x = 1\n")
	if len(launched) != 0 {
		t.Fatalf("auto_start:false Deno started on Open: %v", launched)
	}
	_ = port.StartLanguage("typescript")
	if len(launched) != 1 || !strings.Contains(launched[0], "deno") {
		t.Fatalf("manual start launched %v, want deno lsp", launched)
	}
}

// TestRootChangeListener checks SetRoot notifies the app outside the port lock (the Deno
// selector rebinds from it) and does not deadlock.
func TestRootChangeListener(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	var got string
	port.SetRootChangeListener(func(root string) { got = root })
	port.SetRoot("/remote/next")
	if got != "/remote/next" {
		t.Fatalf("root listener got %q", got)
	}
}

// TestStartLanguageAppliesCompletedSelectionToExistingManager checks lifecycle: a baseline
// manager created before the async answer is not touched automatically, but an explicit
// StartLanguage applies the completed Deno selection (only the target language restarts).
func TestStartLanguageAppliesCompletedSelectionToExistingManager(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	if len(launched) == 0 || !strings.Contains(launched[0], "typescript-language-server") {
		t.Fatalf("baseline manager did not launch: %v", launched)
	}
	before := len(launched)
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
	if len(launched) != before {
		t.Fatalf("provider install restarted a running manager: %v", launched)
	}
	_ = port.StartLanguage("typescript")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "deno") {
		t.Fatalf("explicit StartLanguage did not apply the Deno selection: %v", launched)
	}
}

// TestStartLanguageRestoresBaselineWhenSelectionCleared checks a config reload/explicit
// change that removes the completed selection: the next explicit Start reverts that
// language to the configured baseline instead of keeping the old Deno override.
func TestStartLanguageRestoresBaselineWhenSelectionCleared(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	_ = port.StartLanguage("typescript")
	if len(launched) != 1 || !strings.Contains(launched[0], "deno") {
		t.Fatalf("Deno selection did not start on explicit start: %v", launched)
	}
	port.ClearRootServerProvider(root)
	_ = port.StartLanguage("typescript")
	if len(launched) != 2 || strings.Contains(launched[1], "deno") {
		t.Fatalf("cleared selection did not restore baseline on explicit start: %v", launched)
	}
}

// TestStartLanguageLeavesOtherLanguagesAlone checks replacing the target language's config
// does not stop or restart another language's servers.
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
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
	_ = port.StartLanguage("typescript")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "deno") {
		t.Fatalf("target language start = %v", launched)
	}
}

// TestRestartServerAppliesCompletedSelection checks the settings-style Restart Server path
// also applies a completed selection to an existing manager.
func TestRestartServerAppliesCompletedSelection(t *testing.T) {
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
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
	_ = port.RestartServer("typescript", "")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "deno") {
		t.Fatalf("explicit RestartServer did not apply selection: %v", launched)
	}
}

// TestRestartServerMultiServerReplacement checks the documented multi-server semantics: when
// the completed selection differs, Restart Server on any row replaces the language's whole
// server list with the selection (the row name is not used); when no replacement is needed,
// the normal named restart still applies.
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
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
	_ = port.RestartServer("typescript", "ts-two")
	if len(launched) != before+1 || !strings.Contains(launched[len(launched)-1], "deno") {
		t.Fatalf("multi-server replacement launched %v (before %d), want only the selected deno singleton", launched, before)
	}
}

// TestProviderChangeAndExplicitStartRace runs provider install/clear concurrently with the
// explicit start path; selectionMu must keep each snapshot consistent (run with -race too).
func TestProviderChangeAndExplicitStartRace(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) { return nil, errors.New("captured") })
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
				if language != "typescript" {
					return manager.ServerConfig{}, false
				}
				return manager.ServerConfig{Command: []string{"deno"}, Name: "deno", AutoStart: false}, true
			})
			port.ClearRootServerProvider(root)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = port.StartLanguage("typescript")
		}
	}()
	wg.Wait()
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Name: "deno", AutoStart: false}, true
	})
	_ = port.StartLanguage("typescript")
	if got := port.serverConfigsFor(root)["typescript"]; len(got) != 1 || got[0].Name != "deno" {
		t.Fatalf("final config = %+v, want the completed provider", got)
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

// TestSetConfigThenExplicitStartUsesReloadedExplicitServer checks the audit case with a Deno
// selection present: after a reload adds an explicit TS server and the provider stops
// offering Deno, the next explicit Start restores/replaces to the reloaded explicit baseline
// (and no reload-time restart happened).
func TestSetConfigThenExplicitStartUsesReloadedExplicitServer(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	useDeno := true
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if useDeno && language == "typescript" {
			return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
		}
		return manager.ServerConfig{}, false
	})
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	_ = port.StartLanguage("typescript")
	if len(launched) != 1 || !strings.Contains(launched[0], "deno") {
		t.Fatalf("initial selection = %v, want deno", launched)
	}
	useDeno = false
	reloaded := config.Defaults()
	reloaded.LSP = map[string][]config.LSPServer{"typescript": {{Command: "new-ts"}}}
	port.SetConfig(reloaded)
	if len(launched) != 1 {
		t.Fatalf("reload restarted the manager: %v", launched)
	}
	_ = port.StartLanguage("typescript")
	if len(launched) != 2 || strings.Contains(launched[1], "deno") || !strings.Contains(launched[1], "new-ts") {
		t.Fatalf("explicit start after reload = %v, want the reloaded explicit baseline", launched)
	}
}

// TestReplaceServersPreflightKeepsOldServerOnTrustRefusal checks the trust-revocation side
// effect: when the new selection's spawn gate refuses, an explicit Start must fail before
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
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
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
	if mgr == nil || len(mgr.CurrentServers("typescript")) != 1 || mgr.CurrentServers("typescript")[0].Command[0] == "deno" {
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
