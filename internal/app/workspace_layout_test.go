package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/session"
)

// registerLayoutWorkspace registers a dormant workspace with a layout but no session store:
// its session directory is never created, exactly like a workspace of a daemon whose config
// has editor.save_session off.
func registerLayoutWorkspace(t *testing.T, workspacesDir, root, label string, files []session.FileState, active int) string {
	t.Helper()
	reg, err := daemon.OpenRegistry(workspacesDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.Create(root, label)
	if err != nil {
		t.Fatal(err)
	}
	layout := session.Session{Version: session.Version, Root: root, Files: files, Active: active}
	if _, err := reg.UpdateLayout(ws.File.StableID, &layout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, "sessions", ws.File.StableID)); !os.IsNotExist(err) {
		t.Fatalf("the disabled session store created its directory: %v", err)
	}
	return ws.File.StableID
}

// TestWorkspaceLayoutRestoresWithoutSessionStore: with editor.save_session off a daemon
// workspace has no session store, but the registry still knows the tabs it had. Switching to
// it must reopen those files instead of showing an empty editor, and a file another workspace
// owns must come back as that workspace's shared buffer (mirror, then ownership on focus).
func TestWorkspaceLayoutRestoresWithoutSessionStore(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA, rootB := t.TempDir(), t.TempDir()
	path := filepath.Join(rootA, "shared.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	idB := registerLayoutWorkspace(t, workspacesDir, rootB, "B",
		[]session.FileState{{Path: path, Line: 2}}, 0)
	m, err := newWorkspaceManager(t.Context(), Options{
		Root: rootA,
		// The isolated user config enables saving; this launch's config turns it off.
		Config: userConfigFile(t, `{"editor":{"save_session":false}}`),
		Daemon: &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.mu.Lock()
	a := m.sessions[m.active]
	m.mu.Unlock()
	if a == nil || a.rt == nil {
		t.Fatal("no foreground workspace runtime")
	}
	if a.rt.sessionDir != "" {
		t.Fatalf("foreground sessionDir = %q, want none with save_session off", a.rt.sessionDir)
	}
	// The foreground workspace owns the file with live unsaved text.
	if err := a.rt.ed.OpenFile(path); err != nil {
		t.Fatalf("open in the foreground workspace: %v", err)
	}
	a.rt.ed.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "owner ")
	ownerText := a.rt.ed.Active().Buffer.Text()
	selectCmd, selectErr := m.Select(idB)
	runWorkspaceCmd(t, selectCmd, selectErr)
	m.mu.Lock()
	b := m.sessions[idB]
	m.mu.Unlock()
	if b == nil || b.rt == nil {
		t.Fatal("the layout workspace did not materialize")
	}
	if b.rt.sessionDir != "" {
		t.Fatalf("restored sessionDir = %q, want none with save_session off", b.rt.sessionDir)
	}
	active := b.rt.ed.Active()
	if active == nil {
		t.Fatal("the restored workspace has no document")
	}
	if active.Path != path {
		t.Fatalf("restored document = %q, want %q", active.Path, path)
	}
	// It became the focused workspace, so it takes the canonical document from A.
	if active.SharedMirror() || active.ReadOnly() {
		t.Fatal("the restored workspace did not take ownership of the shared file")
	}
	if got := active.Buffer.Text(); got != ownerText {
		t.Fatalf("restored text = %q, want the live owner text %q", got, ownerText)
	}
	if !a.rt.ed.Active().SharedMirror() || !a.rt.ed.Active().ReadOnly() {
		t.Fatal("the original workspace is not a read-only mirror")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "one\ntwo\n" {
		t.Fatalf("the restore wrote the file: %q, %v", raw, err)
	}
}
