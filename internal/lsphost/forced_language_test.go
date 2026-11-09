package lsphost

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
)

func newForcedTestPort(t *testing.T) (*lspPort, string, *int) {
	t.Helper()
	cfg := config.Defaults()
	cfg.LSP = map[string][]config.LSPServer{
		"go": {{Command: "gopls", Dormant: true}}, // AutoStart false: forced is observable
	}
	port := NewFor(cfg).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	spawned := 0
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) {
		spawned++
		return nil, errors.New("captured")
	})
	return port, root, &spawned
}

// TestFinishManualPerLanguageTokenClears clears A's forced after a newer action for another
// language: the per-language token means A's finish still owns its own language even though
// javascript advanced separately. A stale manager.forced would start on a later Trusted Open
// of the manual (auto_start:false) go server.
func TestFinishManualPerLanguageTokenClears(t *testing.T) {
	port, root, spawned := newForcedTestPort(t)
	port.SetAutoStartGate(func() bool { return false })
	mgr := port.managerFor(root)
	mgr.ForceLanguage("go")
	port.mu.Lock()
	port.forced["go"] = true
	port.actionSeq["go"] = 3
	port.trustSeq = 4
	port.mu.Unlock()
	// Newer manual action for another language: it must not invalidate go's finish token.
	port.mu.Lock()
	port.actionSeq["javascript"] = 9
	port.actionSeq["typescript"] = 9
	port.mu.Unlock()
	port.finishManual("go", 3, 4, false)
	port.mu.Lock()
	persisted := port.forced["go"]
	port.mu.Unlock()
	if persisted {
		t.Fatal("finishManual left port forced state after a cross-language action")
	}
	// The manager's forced state must be gone too: with Trusted restored, an automatic
	// Open of this auto_start:false language must not start.
	port.SetAutoStartGate(func() bool { return true })
	before := *spawned
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if *spawned != before {
		t.Fatal("finishManual left manager.forced; a Trusted Open auto-started the manual server")
	}
}

// TestRootWideRestartDoesNotBumpLanguageTokens checks a root-wide Restart does not make an
// in-flight per-language Start lose its token, while a language Restart only bumps that
// language.
func TestRootWideRestartDoesNotBumpLanguageTokens(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	port.SetRoot(t.TempDir())
	port.mu.Lock()
	port.actionSeq["go"] = 7
	port.actionSeq["javascript"] = 2
	port.mu.Unlock()
	if err := port.Restart(""); err != nil {
		t.Fatalf("root-wide Restart: %v", err)
	}
	port.mu.Lock()
	goRoot, jsRoot := port.actionSeq["go"], port.actionSeq["javascript"]
	port.mu.Unlock()
	if goRoot != 7 || jsRoot != 2 {
		t.Fatalf("root-wide Restart bumped tokens: go=%d javascript=%d", goRoot, jsRoot)
	}
	if err := port.Restart("javascript"); err != nil {
		t.Fatalf("language Restart: %v", err)
	}
	port.mu.Lock()
	goAfter, jsAfter := port.actionSeq["go"], port.actionSeq["javascript"]
	port.mu.Unlock()
	if goAfter != 7 || jsAfter != 3 {
		t.Fatalf("language Restart tokens: go=%d javascript=%d, want go unchanged 7, js 3", goAfter, jsAfter)
	}
}

// TestRootWideRestartClearsForcedForNewManager checks the audit's new-manager behavior:
// root-wide Restart clears the port-level forced state, so a manager created after the
// rebuilt roots does not inherit a stale manual override even if the gate later reopens.
func TestRootWideRestartClearsForcedForNewManager(t *testing.T) {
	port, root, spawned := newForcedTestPort(t)
	port.SetAutoStartGate(func() bool { return false })
	port.mu.Lock()
	port.forced["go"] = true // stale intent the audit describes
	port.forced["javascript"] = true
	port.mu.Unlock()
	if err := port.Restart(""); err != nil {
		t.Fatalf("root-wide Restart: %v", err)
	}
	port.mu.Lock()
	left := len(port.forced)
	port.mu.Unlock()
	if left != 0 {
		t.Fatalf("root-wide Restart left %d port forced entries", left)
	}
	// Reopen the gate: a new go manager must not inherit force for the auto_start:false
	// server. If p.forced survived, newManagerLocked would ForceLanguage("go") and this
	// Open would start it.
	port.SetAutoStartGate(func() bool { return true })
	before := *spawned
	_, _ = port.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	if *spawned != before {
		t.Fatal("new manager inherited stale forced state after root-wide Restart")
	}
}
