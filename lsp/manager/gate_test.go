package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/lsp"
)

// The manager's trust gate: a server start consults it with the argv, the workspace root
// and the child's effective PATH, and a refusal starts nothing and reports the reason
// through the standard failed-to-start error.

func TestManagerGateDeniesBeforeStart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := NewManager(root, map[string][]ServerConfig{
		"go": {{Command: []string{"gopls"}}},
	}, nil)
	started := false
	m.start = func(context.Context, lsp.Config) (*lsp.Client, error) {
		started = true
		return nil, errors.New("should not start")
	}
	var gotArgv []string
	var gotCwd, gotPath string
	m.SetGate(func(_ context.Context, argv []string, cwd, pathValue string) error {
		gotArgv = argv
		gotCwd = cwd
		gotPath = pathValue
		return errors.New("workspace trust: the workspace is Restricted")
	})
	err := m.StartLanguage("go")
	if err == nil || !strings.Contains(err.Error(), "Restricted") {
		t.Fatalf("StartLanguage error = %v", err)
	}
	if started {
		t.Error("the gate denial still started the server")
	}
	if len(gotArgv) == 0 || filepath.Base(gotArgv[0]) != "gopls" {
		t.Errorf("gate argv = %v", gotArgv)
	}
	if gotCwd != root {
		t.Errorf("gate cwd = %q, want %q", gotCwd, root)
	}
	if gotPath != os.Getenv("PATH") {
		t.Errorf("gate PATH = %q, want the process PATH", gotPath)
	}
}

func TestManagerGateAllowsWithANilGate(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir(), map[string][]ServerConfig{"go": {{Command: []string{"gopls"}}}}, nil)
	// A nil gate is installed by default: the start hook runs (and fails here, which is
	// the point — the gate did not stand in its way).
	started := false
	m.start = func(context.Context, lsp.Config) (*lsp.Client, error) {
		started = true
		return nil, errors.New("no server in this test")
	}
	if err := m.StartLanguage("go"); err == nil {
		t.Fatal("the fake start succeeded")
	}
	if !started {
		t.Error("the start hook was not reached with no gate")
	}
}

// TestManagerResolvesTheServerThroughTheOverlayPath: the child sees the last PATH entry of
// its environment, so an active project environment's PATH is the one that finds the
// server program and the one the gate judges.
func TestManagerResolvesTheServerThroughTheOverlayPath(t *testing.T) {
	t.Parallel()
	bin := t.TempDir()
	exe := filepath.Join(bin, "fakels-overlay")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(t.TempDir(), map[string][]ServerConfig{
		"go": {{Command: []string{"fakels-overlay"}}},
	}, nil)
	m.SetSpawnEnv(func() []string { return []string{"PATH=" + bin} })
	var gotPath, gotProgram string
	m.SetGate(func(_ context.Context, _ []string, _, pathValue string) error {
		gotPath = pathValue
		return nil
	})
	m.start = func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		gotProgram = cfg.Program
		return nil, errors.New("stop here")
	}
	_ = m.StartLanguage("go")
	if gotPath != bin {
		t.Errorf("gate PATH = %q, want the overlay PATH %q", gotPath, bin)
	}
	if gotProgram != exe {
		t.Errorf("program = %q, want %q resolved through the overlay PATH", gotProgram, exe)
	}
}
