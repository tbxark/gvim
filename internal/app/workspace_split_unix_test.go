//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/session"
)

// seedSplitWorkspace creates the registry workspace for root and writes a session
// with a two-pane layout over a.txt and b.txt into its per-workspace session
// directory. It returns the workspace stable id and the session directory.
func seedSplitWorkspace(t *testing.T, root, a, b string) (string, string) {
	t.Helper()
	reg, err := daemon.OpenRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.Create(root, "ws")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := reg.SessionDir(ws.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Save(dir, session.Session{
		Root: root, Active: 1,
		Files:  []session.FileState{{Path: a}, {Path: b}},
		Layout: twoSplitLayout(a, b),
	}); err != nil {
		t.Fatal(err)
	}
	return ws.File.StableID, dir
}

// TestWorkspaceDaemonRestartRestoresSplits checks the daemon workspace path: a
// workspace with a saved two-pane layout materializes with both panes; the daemon
// restart (closeAll captures every workspace, then a new manager discovers the
// registry) writes the layout into the workspace session and restores it again with
// the same direction, ratio and focused pane.
func TestWorkspaceDaemonRestartRestoresSplits(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	apptest.WriteTestFile(t, a, "a1\na2\na3\n")
	apptest.WriteTestFile(t, b, "b1\nb2\nb3\n")
	id, sessDir := seedSplitWorkspace(t, root, a, b)

	m := tabTestManager(t, root, "")
	t.Cleanup(m.closeAll)
	rt := tabTestRuntime(t, m, id)
	got := rt.model.SplitState()
	if got == nil || len(got.Root.Leaves()) != 2 || got.Root.Ratio != 35 || got.Focus != 1 {
		t.Fatalf("materialized workspace layout = %+v", got)
	}

	// The daemon exit captures the layout into the workspace's session directory.
	m.closeAll()
	saved, ok, err := session.Load(sessDir)
	if err != nil || !ok {
		t.Fatalf("workspace session after exit = %v, %v", ok, err)
	}
	if saved.Layout == nil || saved.Layout.Root.Ratio != 35 || len(saved.Layout.Root.Leaves()) != 2 {
		t.Fatalf("captured workspace layout = %+v", saved.Layout)
	}

	// A new manager (daemon restart) materializes it again with both panes.
	m2 := tabTestManager(t, root, "")
	t.Cleanup(m2.closeAll)
	rt2 := tabTestRuntime(t, m2, id)
	got = rt2.model.SplitState()
	if got == nil || len(got.Root.Leaves()) != 2 || got.Root.Ratio != 35 || got.Focus != 1 {
		t.Fatalf("restarted workspace layout = %+v", got)
	}
	if names, active := apptest.TabNames(rt2.ed); len(names) != 2 || active != 1 {
		t.Fatalf("restarted tabs = %v active %d", names, active)
	}
}

// TestWorkspaceDaemonRestartDropsStaleSplitLayout checks the stale-file fallback on
// the daemon path: a pane file deleted after the layout was captured makes the
// restore reject the whole layout (one pane), never showing a wrong file in a pane
// and never failing the workspace start.
func TestWorkspaceDaemonRestartDropsStaleSplitLayout(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	apptest.WriteTestFile(t, a, "a1\na2\n")
	apptest.WriteTestFile(t, b, "b1\nb2\n")
	id, sessDir := seedSplitWorkspace(t, root, a, b)

	m := tabTestManager(t, root, "")
	t.Cleanup(m.closeAll)
	if got := tabTestRuntime(t, m, id).model.SplitState(); got == nil {
		t.Fatal("the two-pane workspace did not materialize")
	}
	// The file a pane showed is deleted; the daemon capture keeps its record.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	m.closeAll()
	saved, _, err := session.Load(sessDir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Layout == nil {
		t.Fatal("the exit capture dropped the layout instead of keeping the stale record")
	}

	m2 := tabTestManager(t, root, "")
	t.Cleanup(m2.closeAll)
	rt2 := tabTestRuntime(t, m2, id)
	if got := rt2.model.SplitState(); got != nil {
		t.Fatalf("a stale layout was applied: %+v", got)
	}
	if names, _ := apptest.TabNames(rt2.ed); len(names) != 1 {
		t.Fatalf("restarted tabs = %v, want only a.txt", names)
	}
}
