//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/session"
)

// liveRestoreConfig writes a config with the given terminal.restore_live /
// resume_agents flags and a plain shell script that records its pid in its cwd and
// prints READY before sleeping, so "did a shell start here" is observable.
func liveRestoreConfig(t *testing.T, restoreLive, resumeAgents bool) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "shell.sh")
	body := "#!/bin/sh\necho $$ > \"$PWD/.gvim-shell.pid\"\necho READY\nexec sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	flags := "\"restore_live\":" + strconv.FormatBool(restoreLive) + ",\"resume_agents\":" + strconv.FormatBool(resumeAgents)
	cfg := filepath.Join(dir, "config.json")
	body = "{\"editor\":{\"save_session\":true},\"terminal\":{\"shell\":{\"command\":\"" + script + "\"}, " + flags + "},\"lsp_enabled\":false}"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// liveAgentScripts writes executable scripts that record which argv ran; both then
// sleep so the revived terminal stays running.
func liveAgentScripts(t *testing.T) (launch, resume, launchMarker, resumeMarker string) {
	t.Helper()
	dir := t.TempDir()
	launch = filepath.Join(dir, "launch.sh")
	resume = filepath.Join(dir, "resume.sh")
	launchMarker = filepath.Join(dir, "launch.marker")
	resumeMarker = filepath.Join(dir, "resume.marker")
	must(t, os.WriteFile(launch, []byte("#!/bin/sh\necho launched > "+launchMarker+"\nexec sleep 300\n"), 0o755))
	must(t, os.WriteFile(resume, []byte("#!/bin/sh\necho resumed > "+resumeMarker+"\nexec sleep 300\n"), 0o755))
	return launch, resume, launchMarker, resumeMarker
}

// liveSavedState builds the terminal record of one live-at-capture agent tab.
func liveSavedState(root string, launchArgv, resumeArgv []string) terminal.SavedState {
	tab := terminal.SavedTab{
		Name: "agent", Open: true, Cwd: root, LaunchDir: root,
		Rows: []string{"old transcript"}, Live: true,
		LaunchArgv: launchArgv, ResumeArgv: resumeArgv, Agent: "agent",
	}
	return terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: true,
		Cwd: root, LaunchDir: root, Rows: []string{"old transcript"},
		LaunchArgv: launchArgv, ResumeArgv: resumeArgv, Agent: "agent",
		Tabs: []terminal.SavedTab{tab}, ActiveTab: 0,
	}
}

// buildLiveRestoreRuntime starts a local runtime from sessDir with cfg, with no
// file arguments, and restores the terminal record from sessDir.
func buildLiveRestoreRuntime(t *testing.T, root, cfg, sessDir string) *runtime {
	t.Helper()
	rt, err := buildRuntime(t.Context(), Options{Root: root, Config: cfg, NoPlugins: true, SessionDir: sessDir}, false)
	must(t, err)
	t.Cleanup(rt.close)
	return rt
}

// diveTerminal focuses the dock (a user dive) and runs the resulting commands until
// want holds or the timeout expires. It acts through the same palette message a
// menu row or focus action takes, so the dock's focus/SetActive path is real.
func diveTerminal(t *testing.T, model *tui.Model, want func() bool, what string) {
	t.Helper()
	_, cmd := model.Update(palette.RunCommandMsg{Action: "focus_terminal"})
	pumpDock(t, model, cmd, want, what)
}

// TestLocalLiveRestoreRevivesOnDiveAndResumesAgent checks the Fresh-parity default:
// a tab that was live at save comes back read-only/dead, and the user's first dive
// starts a fresh PTY with the agent-resume argv (resume deferred to the dive),
// keeping the saved transcript above it. Nothing starts before the dive.
func TestLocalLiveRestoreRevivesOnDiveAndResumesAgent(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	cfg := liveRestoreConfig(t, true, true)
	launch, resume, launchMarker, resumeMarker := liveAgentScripts(t)
	sessDir := filepath.Join(t.TempDir(), "session")
	must(t, session.Save(sessDir, session.Session{Root: root}))
	must(t, restore.SaveTerminal(sessDir, liveSavedState(root, []string{launch}, []string{resume})))
	rt := buildLiveRestoreRuntime(t, root, cfg, sessDir)

	// Restore itself runs nothing.
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(resumeMarker); !os.IsNotExist(err) {
		t.Fatalf("restore ran the agent resume: %v", err)
	}
	if _, err := os.Stat(launchMarker); !os.IsNotExist(err) {
		t.Fatalf("restore ran the agent launch: %v", err)
	}
	if st := rt.model.TerminalState(); len(st.Tabs) != 1 || !st.Tabs[0].Open {
		t.Fatalf("restored terminal state = %+v", st)
	}

	diveTerminal(t, rt.model, func() bool {
		_, err := os.Stat(resumeMarker)
		return err == nil
	}, "the agent resume on dive")
	if _, err := os.Stat(launchMarker); !os.IsNotExist(err) {
		t.Fatalf("the dive ran the launch argv instead of the resume: %v", err)
	}
	state := rt.model.TerminalState()
	if len(state.Tabs) != 1 || !state.Tabs[0].Live {
		t.Fatalf("revived terminal is not live: %+v", state.Tabs)
	}
	if !strings.Contains(rowsText(state.Tabs[0].Rows), "old transcript") {
		t.Fatalf("revived tab lost its transcript: %q", state.Tabs[0].Rows)
	}
}

// TestLocalLiveRestoreOptions covers the gates: an exited-at-save tab and an old
// record without the live flag never revive, restoring everything read-only never
// revives, and resume_agents=false dives into the launch argv instead of the resume.
func TestLocalLiveRestoreOptions(t *testing.T) {
	tests := []struct {
		name       string
		live       bool
		restoreOn  bool
		resumeOn   bool
		launchWant bool
		resumeWant bool
	}{
		{name: "exited tab stays dead", live: false, restoreOn: true, resumeOn: true},
		{name: "all read-only", live: true, restoreOn: false, resumeOn: true},
		{name: "resume_agents off uses launch", live: true, restoreOn: true, resumeOn: false, launchWant: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			cfg := liveRestoreConfig(t, tt.restoreOn, tt.resumeOn)
			launch, resume, launchMarker, resumeMarker := liveAgentScripts(t)
			sessDir := filepath.Join(t.TempDir(), "session")
			must(t, session.Save(sessDir, session.Session{Root: root}))
			st := liveSavedState(root, []string{launch}, []string{resume})
			st.Live = tt.live
			st.Tabs[0].Live = tt.live
			must(t, restore.SaveTerminal(sessDir, st))
			rt := buildLiveRestoreRuntime(t, root, cfg, sessDir)

			deadline := time.Now().Add(500 * time.Millisecond)
			diveTerminal(t, rt.model, func() bool { return time.Now().After(deadline) }, "the dive settle")
			_, resumeErr := os.Stat(resumeMarker)
			_, launchErr := os.Stat(launchMarker)
			if tt.resumeWant != (resumeErr == nil) || tt.launchWant != (launchErr == nil) {
				t.Fatalf("resume ran %v, launch ran %v; want resume %v launch %v", resumeErr == nil, launchErr == nil, tt.resumeWant, tt.launchWant)
			}
			if !tt.launchWant && !tt.resumeWant {
				if st := rt.model.TerminalState(); !st.Tabs[0].Open {
					t.Fatalf("read-only tab was lost: %+v", st)
				}
			}
		})
	}
}

// TestLocalLiveRestoreOldRecordStaysDead checks migration: a terminal record
// written before the live flag restores read-only and no dive can spawn it.
func TestLocalLiveRestoreOldRecordStaysDead(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	cfg := liveRestoreConfig(t, true, true)
	sessDir := filepath.Join(t.TempDir(), "session")
	must(t, session.Save(sessDir, session.Session{Root: root}))
	must(t, restore.SaveTerminal(sessDir, terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Cwd: root, Rows: []string{"old output"},
	}))
	rt := buildLiveRestoreRuntime(t, root, cfg, sessDir)
	deadline := time.Now().Add(400 * time.Millisecond)
	diveTerminal(t, rt.model, func() bool { return time.Now().After(deadline) }, "the dive settle")
	if _, err := os.Stat(tabTestPidPath(root)); !os.IsNotExist(err) {
		t.Fatalf("an old dead record spawned a shell")
	}
	if st := rt.model.TerminalState(); !st.Tabs[0].Open || st.Tabs[0].Live {
		t.Fatalf("old record after the dive = %+v", st.Tabs)
	}
}

// seedLiveWorkspace creates the registry workspace for root and writes a session
// directory with a live-at-capture plain-shell tab in its terminal record.
func seedLiveWorkspace(t *testing.T, root string) (string, string) {
	t.Helper()
	reg, err := daemon.OpenRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.Create(root, "live")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := reg.SessionDir(ws.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	must(t, session.Save(dir, session.Session{Root: root}))
	must(t, restore.SaveTerminal(dir, terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: true,
		Cwd: root, LaunchDir: root, Rows: []string{"old workspace output"},
		Tabs: []terminal.SavedTab{{
			Name: "shell", Open: true, Cwd: root, LaunchDir: root,
			Rows: []string{"old workspace output"}, Live: true,
		}},
		ActiveTab: 0,
	}))
	return ws.File.StableID, dir
}

// TestWorkspaceDaemonTerminalLiveRestore checks per-workspace daemon tabs behave
// like local ones: a live-at-save tab in a materialized workspace stays dead until
// the user dives into it, then starts a fresh shell in its workspace root; switching
// keeps the warm shell alive and the other workspace's tab revives independently;
// a daemon restart restores it dead again until the next dive.
func TestWorkspaceDaemonTerminalLiveRestore(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	rootA, rootB := t.TempDir(), t.TempDir()
	cfg := liveRestoreConfig(t, true, true)
	idA, _ := seedLiveWorkspace(t, rootA)
	idB, _ := seedLiveWorkspace(t, rootB)
	m := tabTestManager(t, rootA, cfg)
	var closeOnce sync.Once
	closeAll := func() { closeOnce.Do(m.closeAll) }
	t.Cleanup(closeAll)
	rtA := tabTestRuntime(t, m, idA)

	// Materializing A restores read-only; nothing starts.
	if pid := tabTestPid(tabTestPidPath(rootA)); pid != 0 {
		t.Fatalf("workspace materialization started a shell (pid %d)", pid)
	}
	diveTerminal(t, rtA.model, func() bool { return tabTestPid(tabTestPidPath(rootA)) != 0 }, "the workspace A dive")
	pidA := tabTestPid(tabTestPidPath(rootA))
	if !tabTestAlive(pidA) {
		t.Fatalf("the revived workspace shell pid %d is not alive", pidA)
	}

	// Switch to the second seeded workspace: its tab stays dead until its own dive.
	created, createdErr := m.Create(rootB, "")
	runWorkspaceCmd(t, created, createdErr)
	rtB := tabTestRuntime(t, m, idB)
	if pid := tabTestPid(tabTestPidPath(rootB)); pid != 0 {
		t.Fatalf("workspace B materialization started a shell (pid %d)", pid)
	}
	diveTerminal(t, rtB.model, func() bool { return tabTestPid(tabTestPidPath(rootB)) != 0 }, "the workspace B dive")

	// Switching back to A keeps its warm shell alive and does not restart it.
	selected, selectedErr := m.Select(idA)
	runWorkspaceCmd(t, selected, selectedErr)
	if !tabTestAlive(pidA) || tabTestPid(tabTestPidPath(rootA)) != pidA {
		t.Fatalf("the warm workspace shell changed across the switch")
	}
	if got := tabTestRuntime(t, m, idA); got != rtA {
		t.Fatal("switching back rebuilt the workspace A runtime")
	}

	// Daemon exit captures A's tab as live (its shell runs); a new manager restores
	// it dead again and the next dive starts a NEW shell (the old pid is gone).
	closeAll()
	tabTestWaitDead(t, pidA, "the daemon exit")
	// The marker of the dead run is stale; remove it so a spawn before the dive is
	// observable.
	_ = os.Remove(tabTestPidPath(rootA))
	m2 := tabTestManager(t, rootA, cfg)
	t.Cleanup(m2.closeAll)
	if pid := tabTestPid(tabTestPidPath(rootA)); pid != 0 {
		t.Fatalf("daemon restart started a shell before the dive (pid %d)", pid)
	}
	rtA2 := tabTestRuntime(t, m2, idA)
	diveTerminal(t, rtA2.model, func() bool { return tabTestPid(tabTestPidPath(rootA)) != 0 }, "the daemon-restart dive")
	pidA2 := tabTestPid(tabTestPidPath(rootA))
	if pidA2 == pidA || !tabTestAlive(pidA2) {
		t.Fatalf("the daemon-restart dive started pid %d (old %d)", pidA2, pidA)
	}
}

// rowsText joins saved transcript rows.
func rowsText(rows []string) string { return strings.Join(rows, "\n") }
