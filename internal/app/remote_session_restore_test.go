package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/session"
)

// remoteSessionManager builds a daemon-hosted remote workspace through the fake authority
// with the given session seed and returns the manager and the workspace session directory.
func remoteSessionManager(t *testing.T, slot authority.Slot, spec *authority.RemoteSpec, seed func(dir string)) (*workspaceManager, string) {
	t.Helper()
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	reg, err := daemon.OpenRegistry(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.CreateWithAuthority(spec.Workspace(), "", spec.Raw)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := reg.SessionDir(ws.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		seed(dir)
	}
	stubMountAuthority(t, slot)
	m, err := newWorkspaceManager(context.Background(), Options{
		Remote:     spec,
		SessionDir: t.TempDir(),
		NoPlugins:  true,
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: "app-test-remote-restore"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.closeAllChecked(5 * time.Second) })
	return m, dir
}

// foregroundRuntime returns the manager's active runtime.
func foregroundRuntime(t *testing.T, m *workspaceManager) *runtime {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.sessions[m.active]
	if sess == nil {
		t.Fatal("no foreground workspace runtime")
	}
	return sess.rt
}

// seedRemoteSession writes the saved session of a remote workspace over two files, with
// unsaved stash text on the first and a two-pane layout.
func seedRemoteSession(t *testing.T, root string) (a, b string) {
	t.Helper()
	a = root + "/src/a.go"
	b = root + "/src/b.go"
	return a, b
}

func writeRemoteSession(t *testing.T, dir string, a, b string, stashText string) {
	t.Helper()
	stashName, err := session.StashContent(dir, stashText)
	if err != nil {
		t.Fatal(err)
	}
	layout := twoSplitLayout(a, b)
	must(t, session.Save(dir, session.Session{
		Root:     "/srv/proj",
		Active:   1,
		Explorer: true,
		Files: []session.FileState{
			{Path: a, Line: 2, Col: 1, StashFile: stashName},
			{Path: b, Line: 3},
		},
		Layout: layout,
	}))
}

func remoteSessionFS() *daemonRemoteFS {
	fs := newDaemonRemoteFS()
	fs.put("/srv/proj/src/a.go", "package main\n\nfunc main() {}\n")
	fs.put("/srv/proj/src/b.go", "package other\n\nvar X = 1\n")
	return fs
}

// TestDaemonRemoteWorkspaceRestoresSession checks a daemon-hosted remote workspace reopens
// its saved tabs, active tab, unsaved stash, explorer state and split layout through the
// authority after the connection is up.
func TestDaemonRemoteWorkspaceRestoresSession(t *testing.T) {
	isolateConfig(t)
	fs := remoteSessionFS()
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/main.go", Raw: "u@h:/srv/proj/main.go"}
	a, b := seedRemoteSession(t, "/srv/proj")
	m, _ := remoteSessionManager(t, jumpBoxSlot(fs), spec, func(dir string) {
		writeRemoteSession(t, dir, a, b, "package main\n\nfunc main() { /* unsaved */ }\n")
	})
	rt := foregroundRuntime(t, m)
	tabs := rt.ed.Tabs()
	if len(tabs) != 2 {
		t.Fatalf("restored tabs = %+v, want the two saved remote files", tabs)
	}
	if !tabs[1].Active {
		t.Fatalf("restored active tab = %+v, want the saved active index 1", tabs)
	}
	layout := rt.model.SplitState()
	if layout == nil || layout.Root.Dir != session.SplitSideBySide || layout.Root.Ratio != 35 || len(layout.Root.Leaves()) != 2 {
		t.Fatalf("restored split layout = %+v", layout)
	}
	if layout.Focus != 1 || layout.Root.A.Path != a || layout.Root.B.Path != b {
		t.Fatalf("restored layout focus/leaves = %+v", layout)
	}
	// Reading the stashed text is checked after the pure layout capture (activating a tab
	// changes the focused pane's tab, as any editor navigation would).
	rt.ed.ActivateTab(0)
	if text := rt.ed.Active().Buffer.Text(); !strings.Contains(text, "/* unsaved */") {
		t.Fatalf("restored remote stash text = %q", text)
	}
}

// TestDaemonRemoteWorkspaceMissingFileKeepsStash checks a remote file that vanished is a
// notice-bearing failure: its unsaved stash is kept (the runtime keeps the file in
// rt.kept) and the other tabs still restore.
func TestDaemonRemoteWorkspaceMissingFileKeepsStash(t *testing.T) {
	isolateConfig(t)
	fs := newDaemonRemoteFS()
	fs.put("/srv/proj/src/a.go", "package main\n")
	// b.go is absent on the remote host.
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/main.go", Raw: "u@h:/srv/proj/main.go"}
	a, b := seedRemoteSession(t, "/srv/proj")
	m, dir := remoteSessionManager(t, jumpBoxSlot(fs), spec, func(dir string) {
		// a.go exists on the remote host; b.go vanished but carries unsaved text, which
		// must be kept and reported.
		stashName, err := session.StashContent(dir, "b unsaved\n")
		if err != nil {
			t.Fatal(err)
		}
		must(t, session.Save(dir, session.Session{
			Root: "/srv/proj",
			Files: []session.FileState{
				{Path: a},
				{Path: b, StashFile: stashName},
			},
		}))
	})
	rt := foregroundRuntime(t, m)
	if len(rt.ed.Tabs()) != 1 {
		t.Fatalf("restored tabs = %+v, want only the existing remote file", rt.ed.Tabs())
	}
	if len(rt.kept) != 1 || rt.kept[0].Path != b {
		t.Fatalf("kept failures = %+v, want the vanished remote file", rt.kept)
	}
	if rt.kept[0].StashFile == "" {
		t.Fatal("the vanished file's stash name was dropped")
	}
	if _, err := session.ReadStash(dir, rt.kept[0].StashFile); err != nil {
		t.Fatalf("the vanished file's stash is gone: %v", err)
	}
}

// TestDaemonRemoteDisconnectedKeepsSavedSession checks a remote workspace whose authority is
// down at daemon start restores nothing and, critically, does not overwrite the saved
// session/stash on close: the next start can restore it.
func TestDaemonRemoteDisconnectedKeepsSavedSession(t *testing.T) {
	isolateConfig(t)
	fs := remoteSessionFS()
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/main.go", Raw: "u@h:/srv/proj/main.go"}
	a, b := seedRemoteSession(t, "/srv/proj")
	down := authoritytest.New()
	down.ConnectedState = false
	down.ReconnectErr = errors.New("no route to host")
	down.FileSystem = fs
	down.LabelText = "u@h"
	m, dir := remoteSessionManager(t, down, spec, func(dir string) {
		writeRemoteSession(t, dir, a, b, "package main // keep me\n")
	})
	rt := foregroundRuntime(t, m)
	if rt.keepSavedSession != true {
		t.Fatal("a disconnected remote workspace did not mark its session kept")
	}
	if rt.sessionDir != "" {
		t.Fatalf("disconnected remote workspace session dir = %q, want no save", rt.sessionDir)
	}
	if len(rt.ed.Tabs()) != 0 {
		t.Fatalf("disconnected remote workspace restored tabs: %+v", rt.ed.Tabs())
	}
	// Close the manager (the daemon exit path): the disconnected workspace must not
	// overwrite the seeded session or its stash.
	must(t, m.closeAllChecked(5*time.Second))
	loaded, ok, err := session.Load(dir)
	must(t, err)
	if !ok || len(loaded.Files) != 2 {
		t.Fatalf("saved session after a disconnected start = %+v, %v", loaded, ok)
	}
	if loaded.Files[0].StashFile == "" {
		t.Fatal("the saved stash name was cleared by the disconnected runtime")
	}
	if text, err := session.ReadStash(dir, loaded.Files[0].StashFile); err != nil || !strings.Contains(text, "keep me") {
		t.Fatalf("saved stash after a disconnected start = %q, %v", text, err)
	}
	// Files on the remote fake are untouched too.
	if data, err := fs.ReadFile(context.Background(), filepath.Clean("/srv/proj/src/a.go"), 1<<20); err != nil || len(data) == 0 {
		t.Fatalf("remote file after a disconnected start = %q, %v", data, err)
	}
}

// TestDaemonRemoteCoTenantSessionsAreIsolated checks two remote workspaces over the same
// remote root keep separate sessions/stashes, each restoring its own text.
func TestDaemonRemoteCoTenantSessionsAreIsolated(t *testing.T) {
	isolateConfig(t)
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	reg, err := daemon.OpenRegistry(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	one, _, err := reg.CreateWithAuthority("/srv/proj", "one", "u@h:/srv/proj/")
	if err != nil {
		t.Fatal(err)
	}
	two, _, err := reg.CreateWithAuthority("/srv/proj", "two", "u2@h:/srv/proj/")
	if err != nil {
		t.Fatal(err)
	}
	oneDir, err := reg.SessionDir(one.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	twoDir, err := reg.SessionDir(two.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	if oneDir == twoDir {
		t.Fatal("co-tenant remote workspaces share one session directory")
	}
	a, _ := seedRemoteSession(t, "/srv/proj")
	if err := session.Save(oneDir, session.Session{Root: "/srv/proj", Files: []session.FileState{{Path: a}}}); err != nil {
		t.Fatal(err)
	}
	stash, err := session.StashContent(oneDir, "package one // edited\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Save(oneDir, session.Session{Root: "/srv/proj", Files: []session.FileState{{Path: a, StashFile: stash}}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Save(twoDir, session.Session{Root: "/srv/proj"}); err != nil {
		t.Fatal(err)
	}
	stubMountAuthority(t, jumpBoxSlot(remoteSessionFS()))
	// The foreground is the first remote workspace; build each by id and check the texts.
	m, err := newWorkspaceManager(context.Background(), Options{Remote: &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/", Raw: "u@h:/srv/proj/"}, SessionDir: t.TempDir(), NoPlugins: true, Daemon: &DaemonOptions{Mode: DaemonServe, Name: "app-test-remote-cotenant"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.closeAllChecked(5 * time.Second) })
	oneRT := foregroundRuntime(t, m)
	if len(oneRT.ed.Tabs()) != 1 {
		t.Fatalf("one tabs = %+v", oneRT.ed.Tabs())
	}
	oneRT.ed.ActivateTab(0)
	if text := oneRT.ed.Active().Buffer.Text(); !strings.Contains(text, "package one // edited") {
		t.Fatalf("one restored text = %q", text)
	}
	twoSess, err := m.buildWorkspace(context.Background(), two.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { twoSess.rt.exit() })
	if len(twoSess.rt.ed.Tabs()) != 0 {
		t.Fatalf("two restored the other workspace's tab: %+v", twoSess.rt.ed.Tabs())
	}
}
