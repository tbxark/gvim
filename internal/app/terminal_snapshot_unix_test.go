//go:build unix

package app

import (
	"os"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/session"
)

// styledOldScreen is a styled snapshot with one history row ("HIST"), the visible
// row "OLD", a visible bar cursor and the alt flag off.
func styledOldScreen() *terminal.SavedScreen {
	return &terminal.SavedScreen{
		Cols: 4,
		History: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: 'H', Bold: true}, {Rune: 'I'}, {Rune: 'S'}, {Rune: 'T'},
		}}},
		Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: 'O', Fg: "#ff0000", Bold: true},
			{Rune: 'L'}, {Rune: 'D'},
		}}},
		Cursor: terminal.SavedCursor{Row: 0, Col: 1, Visible: true, Shape: "bar"},
	}
}

// seedStyledWorkspace creates the registry workspace for root and writes a session
// dir terminal record whose visible screen was "OLD" with styles and whose process
// was live at capture (so a dive revives it).
func seedStyledWorkspace(t *testing.T, root string) (string, string) {
	t.Helper()
	reg, err := daemon.OpenRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.Create(root, "styled")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := reg.SessionDir(ws.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Save(dir, session.Session{Root: root}); err != nil {
		t.Fatal(err)
	}
	st := terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{
		Name: "shell", Open: true, Cwd: root, LaunchDir: root, Width: 4,
		Rows: []string{"HIST", "OLD"}, Screen: styledOldScreen(), Live: true,
	}}, ActiveTab: 0}
	if err := restore.SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	return ws.File.StableID, dir
}

// screenText joins a saved screen's cells row by row.
func screenText(screen *terminal.SavedScreen) string {
	if screen == nil {
		return ""
	}
	var b strings.Builder
	for _, row := range screen.Rows {
		for _, c := range row.Cells {
			if c.Rune != 0 {
				b.WriteRune(c.Rune)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestWorkspaceDaemonScreenSnapshot checks the daemon workspace path of the styled
// screen snapshot: a materialized workspace restores the styled prior screen, a
// dive revives it and keeps that transcript above the new shell output (the screen
// snapshot is then the live screen), and a daemon restart keeps the transcript.
func TestWorkspaceDaemonScreenSnapshot(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	root := t.TempDir()
	cfg := liveRestoreConfig(t, true, true)
	id, dir := seedStyledWorkspace(t, root)
	m := tabTestManager(t, root, cfg)
	t.Cleanup(m.closeAll)
	rt := tabTestRuntime(t, m, id)

	// The restored tab has the styled prior screen (no live shell yet).
	st := rt.model.TerminalState()
	if len(st.Tabs) != 1 || !strings.Contains(rowsText(st.Tabs[0].Rows), "OLD") {
		t.Fatalf("restored state = %+v", st)
	}
	if activeSaved(st).Screen == nil || activeSaved(st).Screen.Rows[0].Cells[0].Fg != "#ff0000" {
		t.Fatalf("restored styled snapshot = %+v", activeSaved(st).Screen)
	}
	// The daemon path carries the rest of the backing-file remainder too: the
	// styled history row and the saved cursor survive materialization.
	if len(activeSaved(st).Screen.History) != 1 || !activeSaved(st).Screen.History[0].Cells[0].Bold {
		t.Fatalf("restored history snapshot = %+v", activeSaved(st).Screen.History)
	}
	if activeSaved(st).Screen.Cursor != (terminal.SavedCursor{Row: 0, Col: 1, Visible: true, Shape: "bar"}) {
		t.Fatalf("restored cursor = %+v", activeSaved(st).Screen.Cursor)
	}

	// The dive starts the shell; the snapshot transcript stays above the new output.
	if pid := tabTestPid(tabTestPidPath(root)); pid != 0 {
		t.Fatalf("materialization started a shell before the dive (pid %d)", pid)
	}
	diveTerminal(t, rt.model, func() bool {
		cur := rt.model.TerminalState()
		return tabTestPid(tabTestPidPath(root)) != 0 && strings.Contains(rowsText(cur.Tabs[0].Rows), "READY")
	}, "the workspace dive and shell output")
	after := rt.model.TerminalState()
	if !strings.Contains(rowsText(after.Tabs[0].Rows), "OLD") {
		t.Fatalf("the revived tab lost its saved transcript: %q", after.Tabs[0].Rows)
	}
	if !strings.Contains(screenText(after.Tabs[0].Screen), "READY") {
		t.Fatalf("the live screen was not captured after the revive: %+v", after.Tabs[0].Screen)
	}

	// The daemon capture/restart keeps the transcript: close and materialize again
	// from the same registry, and the restored tab still has OLD above.
	m.closeAll()
	// The dead shell's marker is stale after the close; remove it so a spawn before
	// the dive would be observable.
	_ = os.Remove(tabTestPidPath(root))
	m2 := tabTestManager(t, root, cfg)
	t.Cleanup(m2.closeAll)
	restored := tabTestRuntime(t, m2, id).model.TerminalState()
	if len(restored.Tabs) != 1 || !strings.Contains(rowsText(restored.Tabs[0].Rows), "OLD") {
		t.Fatalf("daemon restart lost the transcript: %+v", restored)
	}
	// The record keeps the tab's liveness (dormant-live) so the first dive revives it,
	// but the daemon restart itself must not start a process.
	if pid := tabTestPid(tabTestPidPath(root)); pid != 0 {
		t.Fatalf("the daemon restart started a shell before the dive (pid %d)", pid)
	}
	if !strings.Contains(rowsText(restored.Tabs[0].Rows), "READY") {
		t.Fatalf("the restored transcript lost the live screen text: %+v", restored.Tabs[0].Rows)
	}
	_ = dir
}
