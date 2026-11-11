package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/daemon"
)

// TestWorkspaceSessionDirFollowsTheSetting checks a daemon workspace creates no
// session directory while editor.save_session is off, and uses the registry one when on.
func TestWorkspaceSessionDirFollowsTheSetting(t *testing.T) {
	isolateConfig(t)
	m, id := newPluginManagerTest(t)
	ws, ok, err := m.reg.Get(id)
	if err != nil || !ok {
		t.Fatalf("registry workspace = %+v, %v", ws, err)
	}
	projectConfig := filepath.Join(ws.File.Root, ".gvim", "config.json")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectConfig, []byte(`{"editor":{"save_session":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.workspaceSessionDir(id); err == nil {
		t.Fatal("a disabled project still reported a workspace session directory")
	}
	if err := os.WriteFile(projectConfig, []byte(`{"editor":{"save_session":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := m.workspaceSessionDir(id)
	if err != nil || dir == "" {
		t.Fatalf("enabled workspace session dir = %q, %v", dir, err)
	}
}

func TestDisabledSessionPreventsWorkspaceEviction(t *testing.T) {
	m := &workspaceManager{}
	s := &workspaceSession{ws: daemon.Workspace{}, rt: &runtime{}}
	kept, err := m.evictWorkspace(s, 1)
	if !kept || err == nil {
		t.Fatalf("evict without session store = %v, %v; want workspace kept", kept, err)
	}
}
