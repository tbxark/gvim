package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui"
)

// TestWorkspaceManagerPortLifecycle drives the app/TUI port path of the workspace layer
// without a program: create (materialize), switch, rename, close, and the per-workspace
// session capture.
func TestWorkspaceManagerPortLifecycle(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "a.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Root:       rootA,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	}
	m, err := newWorkspaceManager(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	entries := m.Entries()
	if len(entries) != 1 || !entries[0].Active || !entries[0].Materialized {
		t.Fatalf("foreground entries = %+v", entries)
	}
	first := entries[0]
	if first.Label != filepath.Base(mustResolve(t, rootA)) {
		t.Fatalf("label = %q", first.Label)
	}
	// Create + switch to a second workspace; the first is captured into its session dir.
	// The switch materializes off the UI goroutine, so the returned command is run to
	// completion.
	created, createdErr := m.Create(rootB, "second")
	runWorkspaceCmd(t, created, createdErr)

	entries = m.Entries()
	if len(entries) != 2 {
		t.Fatalf("after create: %+v", entries)
	}
	second := workspaceTestEntry(t, entries, func(e tui.WorkspaceEntry) bool { return e.Active })
	if second.Label != "second" {
		t.Fatalf("new workspace entry = %+v", second)
	}
	first = workspaceTestEntry(t, entries, func(e tui.WorkspaceEntry) bool { return e.ID == first.ID })
	if err := m.waitWrites(5 * time.Second); err != nil {
		t.Fatalf("waiting for the capture write: %v", err)
	}
	sessionA := filepath.Join(workspacesDir, "sessions", first.ID, "session.json")
	if _, err := os.Stat(sessionA); err != nil {
		t.Fatalf("switching away captured no session for the first workspace: %v", err)
	}
	// Switch back; the warm first runtime is shown again.
	selected, selectedErr := m.Select(first.ID)
	runWorkspaceCmd(t, selected, selectedErr)

	list := m.Entries()
	active := workspaceTestEntry(t, list, func(e tui.WorkspaceEntry) bool { return e.Active })
	if active.ID != first.ID {
		t.Fatalf("after select: %+v", list)
	}
	// Rename the active workspace.
	if err := m.Rename(first.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	renamed := workspaceTestEntry(t, m.Entries(), func(e tui.WorkspaceEntry) bool { return e.ID == first.ID })
	if renamed.Label != "renamed" {
		t.Fatalf("rename: %+v", m.Entries())
	}
	// Close the inactive workspace: registry file gone, session data kept.
	closed, closedErr := m.Close(second.ID)
	runWorkspaceCmd(t, closed, closedErr)

	for _, e := range m.Entries() {
		if e.ID == second.ID {
			t.Fatal("the closed workspace is still listed")
		}
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, "sessions", second.ID, "session.json")); err != nil {
		t.Fatalf("close removed the closed workspace's session data: %v", err)
	}
	// Closing the last workspace is refused.
	if cmd, err := m.Close(first.ID); err == nil {
		if cmd != nil {
			cmd()
		}
		if _, still := m.sessions[first.ID]; !still {
			t.Fatal("closing the last workspace closed it")
		}
	}
	_ = daemon.WorkspaceInfo{}
}

// runWorkspaceCmd runs a workspace command and fails on its (nil) walker errors.
func runWorkspaceCmd(t *testing.T, cmd tui.WorkspaceCmd, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("workspace command returned %T", msg)
		}
	}
}

// workspaceTestEntry finds the entry matching match.
func workspaceTestEntry(t *testing.T, entries []tui.WorkspaceEntry, match func(tui.WorkspaceEntry) bool) tui.WorkspaceEntry {
	t.Helper()
	for _, e := range entries {
		if match(e) {
			return e
		}
	}
	t.Fatalf("no workspace entry matches: %+v", entries)
	return tui.WorkspaceEntry{}
}

// mustResolve resolves a workspace root the way the registry keys it.
func mustResolve(t *testing.T, path string) string {
	t.Helper()
	resolved, err := daemon.CanonicalRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
