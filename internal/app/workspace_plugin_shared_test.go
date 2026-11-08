package app

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui"
)

// TestDaemonWorkspaceSharesOnePluginRuntime: the daemon builds ONE plugin runtime for its
// local workspaces; each workspace contributes its own plugin OS/state namespace, and a
// switch rebinds the shared host/runtime to the active model.
func TestDaemonWorkspaceSharesOnePluginRuntime(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	rootA := t.TempDir()
	daemon := pluginhost.NewDaemon()
	shared := tui.NewPluginShared()
	opts := Options{
		Root:         rootA,
		SessionDir:   t.TempDir(),
		Daemon:       &DaemonOptions{Mode: DaemonServe},
		PluginDaemon: daemon,
		PluginShared: shared,
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
	fgID := entries[0].ID
	m.mu.Lock()
	fg := m.sessions[fgID]
	m.mu.Unlock()
	if fg.rt.pluginWS == nil {
		t.Fatal("the daemon workspace has no shared plugin workspace")
	}
	rt := daemon.Runtime()
	if rt == nil {
		t.Fatal("the daemon built no shared plugin runtime")
	}
	if shared.Active() != fg.rt.model {
		t.Fatal("the shared host is not bound to the foreground workspace")
	}
	fgDir, err := m.reg.SessionDir(fgID)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(fgDir, "plugin-state.json"); fg.rt.pluginWS.StatePath != want || rt.WorkspaceStatePath() != want {
		t.Fatalf("foreground plugin state = ws %q runtime %q, want %q", fg.rt.pluginWS.StatePath, rt.WorkspaceStatePath(), want)
	}

	// A second workspace reuses the same runtime and rebinds the host/state on the switch.
	rootB := t.TempDir()
	cmd, err := m.Create(rootB, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var secondID string
	for _, e := range m.Entries() {
		if e.ID != fgID {
			secondID = e.ID
		}
	}
	if secondID == "" {
		t.Fatalf("second workspace not listed: %+v", m.Entries())
	}
	m.mu.Lock()
	second := m.sessions[secondID]
	m.mu.Unlock()
	if second.rt.pluginWS == nil {
		t.Fatal("the second workspace has no shared plugin workspace")
	}
	if second.rt.pluginWS == fg.rt.pluginWS {
		t.Fatal("two workspaces share one plugin OS/state workspace")
	}
	if daemon.Runtime() != rt {
		t.Fatal("a switch built a second plugin runtime")
	}
	if shared.Active() != second.rt.model {
		t.Fatal("the shared host did not rebind to the entering workspace")
	}
	secondDir, err := m.reg.SessionDir(secondID)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(secondDir, "plugin-state.json"); rt.WorkspaceStatePath() != want {
		t.Fatalf("after switch runtime plugin state = %q, want %q", rt.WorkspaceStatePath(), want)
	}

	// Switching back rebinds the host and the state namespace.
	selectCmd, err := m.Select(fgID)
	if err != nil {
		t.Fatal(err)
	}
	runWorkspaceCmd(t, selectCmd, err)
	if shared.Active() != fg.rt.model {
		t.Fatal("the shared host did not rebind on switching back")
	}
	if got := rt.WorkspaceStatePath(); got != fg.rt.pluginWS.StatePath {
		t.Fatalf("switching back left the state namespace at %q", got)
	}
}
