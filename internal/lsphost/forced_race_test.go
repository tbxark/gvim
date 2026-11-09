package lsphost

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/manager"
)

// TestClearForcedDropsManualBypassButKeepsManualStart checks the live-trust hook seam: a
// manual start forces the language, ClearForced removes the automatic bypass from the
// existing and future managers without preventing another explicit manual start.
func TestClearForcedDropsManualBypassButKeepsManualStart(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	port.SetAutoStartGate(func() bool { return false })
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	// Create the manager for this root first (Open records the document; the closed
	// auto-start gate means it spawns nothing).
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const x = 1\n")
	if len(launched) != 0 {
		t.Fatalf("Open spawned %d while the auto-start gate was closed", len(launched))
	}
	_ = port.StartLanguage("typescript") // the fake spawner returns "captured"
	if len(launched) != 1 {
		t.Fatalf("manual start launched %d, want 1", len(launched))
	}
	port.ClearForced()
	// The existing manager no longer has the manual bypass: an automatic Open while the
	// live auto-start gate is false starts nothing.
	_, _ = port.Open(filepath.Join(root, "b.ts"), "typescript", "const y = 2\n")
	if len(launched) != 1 {
		t.Fatalf("Open after ClearForced launched %d extra server(s)", len(launched)-1)
	}
	// A later explicit manual start remains available and goes through the (here nil) spawn
	// gate; it is the trust listener's job to make the live gate false, not to disable the
	// command.
	_ = port.StartLanguage("typescript") // the fake spawner returns "captured"
	if len(launched) != 2 {
		t.Fatalf("post-clear manual start launched %d total, want 2", len(launched))
	}
}

// TestClearForcedConcurrentStartsRace runs the live-listener ClearForced concurrently with
// automatic Opens, manual StartLanguage and RestartServer. The oracle is go test -race plus
// the final check that an explicit manual start still works.
func TestClearForcedConcurrentStartsRace(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var spawned atomic.Int64
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
		spawned.Add(1)
		return nil, errors.New("captured")
	})
	port.SetAutoStartGate(func() bool { return true })
	_, _ = port.Open(filepath.Join(root, "main.ts"), "typescript", "const x = 1\n")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(4)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				port.ClearForced()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = port.StartLanguage("typescript")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = port.RestartServer("typescript", "")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				port.SetAutoStartGate(func() bool { return j%2 == 0 })
				_, _ = port.Open(filepath.Join(root, "main.ts"), "typescript", "const x = 1\n")
			}
		}()
	}
	wg.Wait()
	port.SetAutoStartGate(func() bool { return false })
	port.ClearForced()
	_ = port.StartLanguage("typescript")
	if spawned.Load() == 0 {
		t.Fatal("no server start was attempted; the race test exercised nothing")
	}
}

// TestRestrictedManualStartDoesNotPersistForced defines the manual-start intent under
// non-Trusted trust: the explicit Start may start the server now, but it must not leave a
// persistent forced state that makes a later Open bypass the auto-start gate.
func TestRestrictedManualStartDoesNotPersistForced(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	port.SetAutoStartGate(func() bool { return false })
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if len(launched) != 0 {
		t.Fatalf("auto Open under Restricted started: %v", launched)
	}
	if err := port.StartLanguage("go"); err == nil {
		t.Fatal("expected the fake spawner's error")
	}
	if len(launched) != 1 {
		t.Fatalf("manual StartLanguage under Restricted launched %v, want one", launched)
	}
	port.mu.Lock()
	persisted := port.forced["go"]
	port.mu.Unlock()
	if persisted {
		t.Fatal("manual Start under Restricted persisted forced state")
	}
	// A later Open must not inherit automatic=false from the finished manual start.
	_, _ = port.Open(filepath.Join(root, "b.go"), "go", "package b\n")
	if len(launched) != 1 {
		t.Fatalf("Open after Restricted manual Start spawned %d extra server(s)", len(launched)-1)
	}
}

// TestInFlightStartCannotRewriteForcedAfterTrustClear is the controlled race: the manual
// start has already installed forced and is inside launch when a trust ClearForced arrives;
// the version guard must clear it again instead of leaving an automatic bypass.
func TestInFlightStartCannotRewriteForcedAfterTrustClear(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	port.SetAutoStartGate(func() bool { return false })
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	// Trusted manual start in flight; the spawn gate synchronously simulates a trust revoke
	// and clears forced after the manager has set it.
	port.SetAutoStartGate(func() bool { return true })
	port.SetSpawnGate(func(context.Context, []string, string, string) error {
		port.ClearForced()
		return nil
	})
	_ = port.StartLanguage("go")
	if len(launched) != 1 {
		t.Fatalf("in-flight manual start launched %v, want one", launched)
	}
	port.mu.Lock()
	persisted := port.forced["go"]
	port.mu.Unlock()
	if persisted {
		t.Fatal("in-flight Start rewrote forced after the trust clear")
	}
	// The manager's forced state must be gone too: with the gate closed again, a later
	// Open must not spawn.
	port.SetAutoStartGate(func() bool { return false })
	before := len(launched)
	_, _ = port.Open(filepath.Join(root, "b.go"), "go", "package b\n")
	if len(launched) != before {
		t.Fatalf("Open after in-flight clear spawned %d server(s)", len(launched)-before)
	}
}

// TestStartLanguageClearForcedRaceLeavesNoForcedBypass reproduces the Lead's interleaving:
// StartLanguage snapshots and then blocks in ReplaceServers' spawn-gate preflight; a trust
// ClearForced lands; the manager call then re-installs forced. finishManual must reconcile
// with the trust sequence and clear it again, so a later automatic Open of an
// auto_start:false config does not start.
func TestStartLanguageClearForcedRaceLeavesNoForcedBypass(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	var launched atomic.Int64
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
		launched.Add(1)
		return nil, errors.New("captured")
	})
	port.SetAutoStartGate(func() bool { return true })
	_, _ = port.Open(filepath.Join(root, "a.ts"), "typescript", "const a = 1\n")
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno", AutoStart: false}, true
	})
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	port.SetSpawnGate(func(context.Context, []string, string, string) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- port.StartLanguage("typescript") }()
	<-entered
	port.ClearForced()
	close(release)
	if err := <-done; err != nil && !strings.Contains(err.Error(), "captured") {
		t.Fatalf("StartLanguage error = %v", err)
	}
	port.SetSpawnGate(nil)
	port.mu.Lock()
	forced := port.forced["typescript"]
	port.mu.Unlock()
	if forced {
		t.Fatal("ClearForced left the port forced flag after the racing StartLanguage")
	}
	before := launched.Load()
	_, _ = port.Open(filepath.Join(root, "b.ts"), "typescript", "const b = 2\n")
	if got := launched.Load(); got != before {
		t.Fatalf("an automatic Open after the race started %d server(s) from stale forced state", got-before)
	}
}

// TestRestartDoesNotPersistForcedWhenUntrusted checks the Restart command uses the same
// trust sequencing as Start/RestartServer: a manual Restart under a closed live gate does
// not leave a future-manager forced override.
func TestRestartDoesNotPersistForcedWhenUntrusted(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	port.SetRoot(t.TempDir())
	port.SetAutoStartGate(func() bool { return false })
	_ = port.Restart("typescript")
	port.mu.Lock()
	forced := port.forced["typescript"]
	port.mu.Unlock()
	if forced {
		t.Fatal("Restart under a closed live auto-start gate persisted the forced flag")
	}
}
