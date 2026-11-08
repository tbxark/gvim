package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/tui"
)

// TestDaemonWorkspacePluginStateFile: a daemon workspace runtime scopes gvim.state to its
// own workspace session directory (sessions/<stable-id>/plugin-state.json), and co-tenant
// workspaces get distinct files; a direct launch keeps the editor-wide per-plugin files.
func TestDaemonWorkspacePluginStateFile(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	fg := m.sessions[foregroundID]
	m.mu.Unlock()
	fgDir, err := m.reg.SessionDir(foregroundID)
	if err != nil {
		t.Fatal(err)
	}
	wantFg := filepath.Join(fgDir, "plugin-state.json")
	if fg.rt.pluginStateFile != wantFg {
		t.Fatalf("foreground plugin state file = %q, want %q", fg.rt.pluginStateFile, wantFg)
	}

	// A second (co-tenant-capable) workspace gets its own file.
	root := t.TempDir()
	cmd, err := m.Create(root, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var secondID string
	for _, e := range m.Entries() {
		if e.ID != foregroundID {
			secondID = e.ID
		}
	}
	if secondID == "" {
		t.Fatalf("second workspace not listed: %+v", m.Entries())
	}
	m.mu.Lock()
	second := m.sessions[secondID]
	m.mu.Unlock()
	secondDir, err := m.reg.SessionDir(secondID)
	if err != nil {
		t.Fatal(err)
	}
	wantSecond := filepath.Join(secondDir, "plugin-state.json")
	if second.rt.pluginStateFile != wantSecond {
		t.Fatalf("second plugin state file = %q, want %q", second.rt.pluginStateFile, wantSecond)
	}
	if wantSecond == wantFg {
		t.Fatal("two workspaces share one plugin state file")
	}
	// The file really matches the session directory layout the daemon restores from.
	if filepath.Base(filepath.Dir(wantSecond)) != secondID {
		t.Fatalf("plugin state file %q does not live under sessions/%s", wantSecond, secondID)
	}

	// A direct (non-daemon) launch keeps the editor-wide per-plugin state files.
	directRoot := t.TempDir()
	sessDir := t.TempDir()
	direct, err := buildRuntime(t.Context(), Options{
		Root:       directRoot,
		SessionDir: sessDir,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(direct.close)
	if direct.pluginStateFile != "" {
		t.Fatalf("a direct launch scoped plugin state to %q", direct.pluginStateFile)
	}
	// A daemon runtime's file is created lazily by the plugin runtime, not by buildRuntime.
	if _, err := os.Stat(wantFg); !os.IsNotExist(err) {
		t.Fatalf("buildRuntime created the plugin state file eagerly: %v", err)
	}
	_ = tui.WorkspaceEntry{}
}
