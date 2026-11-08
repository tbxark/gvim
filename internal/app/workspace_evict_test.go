package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui"
)

// createSwitchWorkspace registers a fresh root and materializes/switches to it.
func createSwitchWorkspace(t *testing.T, m *workspaceManager, label string) string {
	t.Helper()
	root := t.TempDir()
	ws, _, err := m.reg.Create(root, label)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := m.Select(ws.File.StableID)
	if err != nil || cmd == nil {
		t.Fatalf("Select %q = %v, %v", label, cmd, err)
	}
	cmd()
	return ws.File.StableID
}

// newPluginManagerTest builds a manager with the daemon's shared plugin runtime, so the
// per-workspace plugin state namespace (pluginWS) exists like in a real daemon.
func newPluginManagerTest(t *testing.T) (*workspaceManager, string) {
	t.Helper()
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	daemon := pluginhost.NewDaemon()
	opts := Options{
		Root:         t.TempDir(),
		SessionDir:   t.TempDir(),
		Daemon:       &DaemonOptions{Mode: DaemonServe},
		PluginDaemon: daemon,
		PluginShared: tui.NewPluginShared(),
	}
	m, err := newWorkspaceManager(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.closeAll(); daemon.Stop() })
	entries := m.Entries()
	if len(entries) != 1 {
		t.Fatalf("foreground entries = %+v", entries)
	}
	return m, entries[0].ID
}

func sessionMaterialized(m *workspaceManager, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id] != nil
}

func activeWorkspace(m *workspaceManager) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// TestWorkspaceEvictsLeastRecentlyFocused: over the cap the oldest-focused non-active
// workspace is evicted, the registry entry stays, and the active workspace is never chosen.
func TestWorkspaceEvictsLeastRecentlyFocused(t *testing.T) {
	m, a := newWorkspaceManagerTest(t)
	m.materializedCap = 2
	m.mu.Lock()
	m.lastFocus[a] = time.Unix(900, 0) // the foreground workspace is the oldest
	m.mu.Unlock()
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { clock = clock.Add(time.Second); return clock }

	b := createSwitchWorkspace(t, m, "b")
	c := createSwitchWorkspace(t, m, "c")
	if sessionMaterialized(m, a) {
		t.Fatalf("the least-recently-focused workspace stayed materialized: %+v", m.Entries())
	}
	if !sessionMaterialized(m, b) || !sessionMaterialized(m, c) {
		t.Fatalf("the wrong workspaces were evicted: %+v", m.Entries())
	}
	if got := activeWorkspace(m); got != c {
		t.Fatalf("active = %q, want the new workspace %q", got, c)
	}
	if !strings.Contains(m.lastEviction, "from memory") || !strings.Contains(m.lastEviction, "limit 2") {
		t.Fatalf("eviction notice = %q", m.lastEviction)
	}

	// A fourth workspace evicts the next oldest (b), not the active c.
	d := createSwitchWorkspace(t, m, "d")
	if sessionMaterialized(m, b) {
		t.Fatalf("b was not evicted next: %+v", m.Entries())
	}
	if !sessionMaterialized(m, c) || !sessionMaterialized(m, d) {
		t.Fatalf("the active workspace was evicted: %+v", m.Entries())
	}
	if got := activeWorkspace(m); got != d {
		t.Fatalf("active = %q, want %q", got, d)
	}
}

// TestWorkspaceEvictionKeepsWorkspaceWhenCaptureFails: unsaved work is never lost to the
// bound: if the capture write AND the synchronous fallback save fail, the workspace stays
// materialized (over the cap) and the failure is reported.
func TestWorkspaceEvictionKeepsWorkspaceWhenCaptureFails(t *testing.T) {
	m, a := newWorkspaceManagerTest(t)
	path := extractionFile(t, mustRoot(t, m, a), "keep.txt", "on disk\n")
	openUnsaved(t, m, a, path, "UNSAVED-")
	m.materializedCap = 1

	// Capture fails in the injected writer; the fallback save fails because the session
	// directory path cannot be created (its parent is a file).
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error { return errors.New("injected capture failure") }
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.sessions[a].rt.sessionDir = filepath.Join(blocker, "session")
	m.mu.Unlock()

	b := createSwitchWorkspace(t, m, "b")
	if !sessionMaterialized(m, a) {
		t.Fatal("a workspace whose capture could not be made durable was evicted")
	}
	if !sessionMaterialized(m, b) {
		t.Fatal("the new active workspace was not materialized")
	}
	if got := activeWorkspace(m); got != b {
		t.Fatalf("active = %q, want %q", got, b)
	}
	if !strings.Contains(m.lastNotice, "cannot evict workspace") || !strings.Contains(m.lastNotice, "stays materialized") {
		t.Fatalf("notice = %q, want the capture failure report", m.lastNotice)
	}
	// The unsaved text is still in the kept workspace.
	m.mu.Lock()
	text := m.sessions[a].rt.ed.Active().Buffer.Text()
	m.mu.Unlock()
	if !strings.Contains(text, "UNSAVED-") {
		t.Fatalf("the kept workspace lost its unsaved text: %q", text)
	}
}

// TestWorkspaceEvictionUsesFallbackAndReopenRestores: a failed capture write is rescued by
// the synchronous save, the workspace is evicted, and selecting it again materializes it
// from the saved session (unsaved tab and text restored).
func TestWorkspaceEvictionUsesFallbackAndReopenRestores(t *testing.T) {
	m, a := newWorkspaceManagerTest(t)
	path := extractionFile(t, mustRoot(t, m, a), "restore.txt", "on disk\n")
	openUnsaved(t, m, a, path, "UNSAVED-")
	m.materializedCap = 1
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error { return errors.New("injected capture failure") }

	b := createSwitchWorkspace(t, m, "b")
	if sessionMaterialized(m, a) {
		t.Fatal("the workspace was not evicted after the fallback save succeeded")
	}
	if !strings.Contains(m.lastEviction, "fallback save kept the session") {
		t.Fatalf("eviction notice = %q, want the fallback wording", m.lastEviction)
	}

	// Reopening materializes it again from the saved session.
	cmd, err := m.Select(a)
	if err != nil || cmd == nil {
		t.Fatalf("Select evicted workspace = %v, %v", cmd, err)
	}
	cmd()
	if !sessionMaterialized(m, a) {
		t.Fatalf("the evicted workspace did not rematerialize: %+v", m.Entries())
	}
	if got := activeWorkspace(m); got != a {
		t.Fatalf("active = %q, want %q", got, a)
	}
	m.mu.Lock()
	doc := m.sessions[a].rt.ed.Active()
	m.mu.Unlock()
	if doc == nil || !strings.Contains(doc.Buffer.Text(), "UNSAVED-") {
		t.Fatalf("reopened document = %+v, want the restored unsaved text", doc)
	}
	// The eviction of b (the now-oldest workspace) keeps the bound while a is shown.
	if sessionMaterialized(m, b) {
		t.Fatal("the bound was not restored after reopening")
	}
}

// TestWorkspaceEvictionExcludesRemoteWorkspaces: a remote/authority workspace owns its SSH
// runtime and is neither evicted nor counted toward the local cap (documented residual).
func TestWorkspaceEvictionExcludesRemoteWorkspaces(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	m.materializedCap = 1
	m.mu.Lock()
	m.lastFocus[m.active] = time.Unix(1999, 0) // the foreground workspace is the oldest
	m.mu.Unlock()
	clock := time.Unix(2000, 0)
	m.now = func() time.Time { clock = clock.Add(time.Second); return clock }

	createSwitchWorkspace(t, m, "b")
	c := createSwitchWorkspace(t, m, "c")
	// Turn the warm c into a remote workspace after it was built (the eviction candidate
	// rules only look at the registry authority, which is what a remote runtime has).
	m.mu.Lock()
	cSess := m.sessions[c]
	cSess.ws.File.AuthoritySpec = "user@host:/remote"
	m.mu.Unlock()

	d := createSwitchWorkspace(t, m, "d")
	if !sessionMaterialized(m, c) {
		t.Fatal("a remote workspace was evicted from memory")
	}
	if !sessionMaterialized(m, d) {
		t.Fatal("the active local workspace was evicted")
	}
	if activeWorkspace(m) != d {
		t.Fatal("the active workspace changed")
	}
}

// TestWorkspaceEvictionPreservesPluginStateFile: a local workspace's per-workspace plugin
// state file survives an eviction and is the same namespace when the workspace is
// materialized again (the shared plugin runtime flushed it when the switch left it).
func TestWorkspaceEvictionPreservesPluginStateFile(t *testing.T) {
	m, a := newPluginManagerTest(t)
	m.materializedCap = 1
	dir, err := m.reg.SessionDir(a)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "plugin-state.json")
	want := []byte(`{"version":1,"plugins":{"t":{"k":"state"}}}`)
	if err := os.WriteFile(statePath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	createSwitchWorkspace(t, m, "b")
	if sessionMaterialized(m, a) {
		t.Fatal("the workspace was not evicted")
	}
	if got, err := os.ReadFile(statePath); err != nil || string(got) != string(want) {
		t.Fatalf("eviction touched the plugin state file: %q, %v", got, err)
	}
	cmd, err := m.Select(a)
	if err != nil || cmd == nil {
		t.Fatalf("Select = %v, %v", cmd, err)
	}
	cmd()
	m.mu.Lock()
	sess := m.sessions[a]
	m.mu.Unlock()
	if sess == nil || sess.rt.pluginWS == nil {
		t.Fatal("reopened workspace has no plugin workspace")
	}
	if sess.rt.pluginWS.StatePath != statePath {
		t.Fatalf("reopened plugin state path = %q, want %q", sess.rt.pluginWS.StatePath, statePath)
	}
	if got, err := os.ReadFile(statePath); err != nil || string(got) != string(want) {
		t.Fatalf("plugin state after reopen = %q, %v", got, err)
	}
}

// mustRoot returns the workspace root of a materialized workspace.
func mustRoot(t *testing.T, m *workspaceManager, id string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.sessions[id]
	if sess == nil {
		t.Fatalf("workspace %q is not materialized", id)
	}
	return sess.ws.File.Root
}

// TestWorkspaceManagerReadsMaterializedCapFromConfig: daemon.materialized_workspaces of the
// user config becomes the manager's cap (default 8 otherwise).
func TestWorkspaceManagerReadsMaterializedCapFromConfig(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	writeUserFile(t, "config.json", `{"daemon": {"materialized_workspaces": 3}}`)
	opts := Options{Root: t.TempDir(), SessionDir: t.TempDir(), Daemon: &DaemonOptions{Mode: DaemonServe}}
	m, err := newWorkspaceManager(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	if m.materializedCap != 3 {
		t.Fatalf("materializedCap = %d, want 3 from the user config", m.materializedCap)
	}
}
