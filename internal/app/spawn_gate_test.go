package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/trust"
)

// The app wiring of the spawn gate: one live decision reaches the authority one-shots,
// the terminal and local language-server starts; a Restricted workspace refuses a
// project executable (explicit or PATH-resolved) and allows a system tool; Blocked
// refuses everything; Trusted allows the project's own.

// buildGatedRuntime builds a session for root at level, with the trust store pointed at
// a temporary file and the level recorded before the runtime resolves it.
func buildGatedRuntime(t *testing.T, level trust.Level, root string) *runtime {
	t.Helper()
	isolateConfig(t)
	store := useTrustStore(t)
	if err := store.Set(root, level); err != nil {
		t.Fatal(err)
	}
	opts := Options{Root: root, SessionDir: t.TempDir(), Simple: true, NoPlugins: true}
	rt, err := buildRuntime(context.Background(), opts, false)
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	t.Cleanup(func() { rt.close() })
	return rt
}

func spawnTestRoot(t *testing.T) (root, tool, binDir string) {
	t.Helper()
	root = t.TempDir()
	tool = filepath.Join(root, "gradlew")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir = filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	batched := filepath.Join(binDir, "tool")
	if err := os.WriteFile(batched, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(batched, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, tool, binDir
}

func TestSpawnGateRefusesProjectExecutables(t *testing.T) {
	root, tool, binDir := spawnTestRoot(t)
	rt := buildGatedRuntime(t, trust.Restricted, root)
	one := rt.auth.OneShot()
	ctx := context.Background()

	// An explicit project path is refused before anything runs.
	_, err := one.Run(ctx, []string{"./gradlew"}, hostos.ProcOptions{Cwd: root})
	if !errors.Is(err, trust.ErrSpawnDenied) {
		t.Fatalf("explicit project executable: %v", err)
	}
	// A bare name resolved through the child's PATH to a project directory is refused.
	_, err = one.Run(ctx, []string{"tool"}, hostos.ProcOptions{Cwd: root, Env: map[string]string{"PATH": binDir}})
	if !errors.Is(err, trust.ErrSpawnDenied) {
		t.Fatalf("PATH-resolved project executable: %v", err)
	}
	// An absolute project path is refused too.
	_, err = one.Run(ctx, []string{tool}, hostos.ProcOptions{Cwd: root})
	if !errors.Is(err, trust.ErrSpawnDenied) {
		t.Fatalf("absolute project executable: %v", err)
	}
	// A system tool is allowed: the gate lets it through and the real OS runs it.
	if _, err := one.Run(ctx, []string{"sh", "-c", "exit 0"}, hostos.ProcOptions{Cwd: root}); err != nil {
		t.Fatalf("system tool refused: %v", err)
	}
	// The dock terminal's shell is gated through the authority's Launch: a project shell
	// is refused while Restricted.
	_, err = rt.auth.Terminal().Launch(ctx, terminalOptionsAt(tool, root))
	if !errors.Is(err, trust.ErrSpawnDenied) {
		t.Fatalf("project terminal shell: %v", err)
	}
	if _, err := rt.auth.Terminal().Launch(ctx, terminalOptionsAt("/bin/sh", root)); err != nil {
		t.Fatalf("system terminal shell refused: %v", err)
	}
}

func terminalOptionsAt(shell, dir string) authority.TerminalOptions {
	return authority.TerminalOptions{Shell: shell, Args: []string{"-i"}, Dir: dir}
}

func TestSpawnGateBlockedRefusesEverything(t *testing.T) {
	root, _, _ := spawnTestRoot(t)
	rt := buildGatedRuntime(t, trust.Blocked, root)
	ctx := context.Background()
	if _, err := rt.auth.OneShot().Run(ctx, []string{"sh", "-c", "exit 0"}, hostos.ProcOptions{Cwd: root}); !errors.Is(err, trust.ErrSpawnDenied) {
		t.Fatalf("blocked one-shot: %v", err)
	}
	if _, err := rt.auth.Terminal().Launch(ctx, terminalOptionsAt("/bin/sh", root)); !errors.Is(err, trust.ErrSpawnDenied) {
		t.Fatalf("blocked terminal: %v", err)
	}
}

func TestSpawnGateTrustedAllowsTheProject(t *testing.T) {
	root, _, _ := spawnTestRoot(t)
	rt := buildGatedRuntime(t, trust.Trusted, root)
	ctx := context.Background()
	if _, err := rt.auth.OneShot().Run(ctx, []string{"./gradlew"}, hostos.ProcOptions{Cwd: root}); err != nil {
		t.Fatalf("trusted project executable refused: %v", err)
	}
}

func TestSpawnGateRefusesLanguageServerStarts(t *testing.T) {
	root, _, _ := spawnTestRoot(t)
	isolateConfig(t)
	store := useTrustStore(t)
	if err := store.Set(root, trust.Blocked); err != nil {
		t.Fatal(err)
	}
	prev := newLSP
	var got tui.LSPSession
	newLSP = func(cfg config.Config) tui.LSPSession {
		got = prev(cfg)
		return got
	}
	t.Cleanup(func() { newLSP = prev })
	rt, err := buildRuntime(context.Background(), Options{Root: root, SessionDir: t.TempDir(), Simple: true, NoPlugins: true}, false)
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	t.Cleanup(func() { rt.close() })
	if got == nil {
		t.Fatal("the LSP session was not built")
	}
	// A document opens the workspace's manager; the manual start then consults the gate.
	got.SetRoot(root)
	// Open creates the workspace's manager; under Restricted the auto-start is stopped
	// (that error is the existing lsp_enabled behavior), so only the manual start below
	// is the gate's concern.
	_, _ = got.Open(filepath.Join(root, "a.go"), "go", "package a\n")
	// A blocked workspace refuses every server start, manual starts included.
	err = got.StartLanguage("go")
	if err == nil || !strings.Contains(err.Error(), "Blocked") {
		t.Fatalf("StartLanguage error = %v, want the trust refusal", err)
	}
}
