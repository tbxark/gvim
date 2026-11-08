package app

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/trust"
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

// TestDaemonWorkspaceProjectPluginConfigDiffer: the plugin config layer buildRuntime
// snapshots into each workspace differs with the workspace's PROJECT config (a trusted
// project can disable a plugin), so the daemon sees a config change on the switch and asks
// for one pump; two workspaces with the same config do not.
func TestDaemonWorkspaceProjectPluginConfigDiffer(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	rootA := t.TempDir()
	writeTestFile(t, filepath.Join(rootA, ".gvim", "config.json"), `{"plugins":{"alpha":{"enabled":false}}}`)
	rootB := t.TempDir()
	store := useTrustStore(t)
	for _, root := range []string{rootA, rootB} {
		if err := store.Set(root, trust.Trusted); err != nil {
			t.Fatal(err)
		}
	}
	daemon := pluginhost.NewDaemon()
	opts := Options{
		Root:         rootA,
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
		t.Fatalf("entries = %+v", entries)
	}
	m.mu.Lock()
	fg := m.sessions[entries[0].ID]
	m.mu.Unlock()
	if fg.rt.pluginWS == nil {
		t.Fatal("foreground workspace has no plugin workspace")
	}
	if off, ok := fg.rt.pluginWS.PluginEnabled["alpha"]; !ok || off {
		t.Fatalf("foreground workspace plugin config = %+v, want alpha disabled by the project config", fg.rt.pluginWS.PluginEnabled)
	}
	keyA := fg.rt.pluginWS.PluginConfigKey

	cmd, err := m.Create(rootB, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var secondID string
	for _, e := range m.Entries() {
		if e.ID != entries[0].ID {
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
		t.Fatal("second workspace has no plugin workspace")
	}
	if _, ok := second.rt.pluginWS.PluginEnabled["alpha"]; ok {
		t.Fatalf("second workspace inherited the first project's plugin config: %+v", second.rt.pluginWS.PluginEnabled)
	}
	if second.rt.pluginWS.PluginConfigKey == keyA {
		t.Fatal("the two workspaces have the same plugin config key despite different project configs")
	}
	// Switching back to the project with the disabled plugin is a config change too.
	selectCmd, err := m.Select(entries[0].ID)
	if err != nil {
		t.Fatalf("Select = %v", err)
	}
	runWorkspaceCmd(t, selectCmd, err)
	if daemon.Runtime().WorkspaceStatePath() == "" {
		t.Fatal("the runtime has no active state namespace after switching back")
	}
}
