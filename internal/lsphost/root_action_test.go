package lsphost

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
)

// TestServerActionRootUsesDocumentRoot: a server action from the active document picks
// that document's manager root, not an arbitrary manager from map iteration.
func TestServerActionRootUsesDocumentRoot(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := t.TempDir()
	rootA := filepath.Join(workspace, "a")
	rootB := filepath.Join(workspace, "b")
	port.SetRoot(workspace)
	port.setDoc(filepath.Join(rootA, "a.go"), lspDocRec{root: rootA, lang: "go"})
	port.setDoc(filepath.Join(rootB, "b.go"), lspDocRec{root: rootB, lang: "go"})
	if got := port.serverActionRoot(filepath.Join(rootA, "a.go"), "go"); got != rootA {
		t.Fatalf("root A action root = %q, want %q", got, rootA)
	}
	if got := port.serverActionRoot(filepath.Join(rootB, "b.go"), "go"); got != rootB {
		t.Fatalf("root B action root = %q, want %q", got, rootB)
	}
	if got := port.serverActionRoot("", "go"); got != workspace {
		t.Fatalf("no-document action root = %q, want the workspace root", got)
	}
}

// TestRestartServerForTargetsOneRoot: with two managers for the same language/name,
// restarting from a document in root A starts only root A's server.
func TestRestartServerForTargetsOneRoot(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := t.TempDir()
	rootA := filepath.Join(workspace, "a")
	rootB := filepath.Join(workspace, "b")
	port.SetRoot(workspace)
	port.mu.Lock()
	port.managers[rootA] = port.newManagerLocked(rootA)
	port.managers[rootB] = port.newManagerLocked(rootB)
	port.mu.Unlock()
	port.setDoc(filepath.Join(rootA, "a.go"), lspDocRec{root: rootA, lang: "go"})

	var roots []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		roots = append(roots, cfg.RootURI)
		return nil, errors.New("captured spawn")
	})
	_ = port.RestartServerFor(filepath.Join(rootA, "a.go"), "go", "gopls")
	if len(roots) != 1 || roots[0] != lsp.PathURI(rootA) {
		t.Fatalf("restart spawn roots = %v, want only %q", roots, lsp.PathURI(rootA))
	}
}
