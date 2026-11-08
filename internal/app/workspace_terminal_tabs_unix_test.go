//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// The per-workspace terminal tab behavior (#19a/#19): each workspace runtime owns
// its own dock.Tabs; a switch captures the outgoing workspace's tabs into its
// session directory (terminal.json, terminal.SavedState v1) and materializes the
// target (read-only dead restore, no auto-launch), warm runtimes keep their live
// shells across switches, and closing a workspace kills its processes.

// tabTestConfig writes a config whose terminal shell is a script that records its
// pid (next to the workspace root it starts in) and then sleeps: a per-workspace
// marker that says "a shell was started here", without control characters in the
// transcript.
func tabTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "shell.sh")
	body := "#!/bin/sh\necho $$ > \"$PWD/.gvim-shell.pid\"\necho READY\nexec sleep 300\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.json")
	body = "{\"terminal\":{\"shell\":{\"command\":\"" + script + "\"}},\"lsp_enabled\":false}"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// tabTestManager starts a workspace manager whose workspace registry lives in dir.
func tabTestManager(t *testing.T, root, cfg string) *workspaceManager {
	t.Helper()
	m, err := newWorkspaceManager(t.Context(), Options{
		Root:       root,
		Config:     cfg,
		NoPlugins:  true,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// tabTestRuntime returns the materialized runtime of a workspace.
func tabTestRuntime(t *testing.T, m *workspaceManager, id string) *runtime {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		t.Fatalf("workspace %s is not materialized", id)
	}
	return s.rt
}

// pumpDock runs first and every command the resulting messages start, until want
// holds. It mirrors how the Bubble Tea program closes the loop; commands that
// block (a wait for terminal output) run in their own goroutine and cannot wedge
// the pump.
func pumpDock(t *testing.T, model *tui.Model, first tea.Cmd, want func() bool, what string) {
	t.Helper()
	msgs := make(chan tea.Msg, 256)
	run := func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			if msg := c(); msg != nil {
				select {
				case msgs <- msg:
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}
	run(first)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if want() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		select {
		case msg := <-msgs:
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					run(c)
				}
				continue
			}
			_, next := model.Update(msg)
			run(next)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// tabTestPidPath is the pid marker a shell started in root writes.
func tabTestPidPath(root string) string { return filepath.Join(root, ".gvim-shell.pid") }

// tabTestPid reads the pid a started shell wrote, or 0 when no shell started.
func tabTestPid(pidPath string) int {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// tabTestAlive reports whether pid names a live process.
func tabTestAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// tabTestWaitDead polls until pid is gone.
func tabTestWaitDead(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !tabTestAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: pid %d is still alive", what, pid)
}

// tabTestSessionFile is the terminal record of a workspace's session directory.
func tabTestSessionFile(t *testing.T, reg *daemon.Registry, id string) (terminal.SavedState, bool) {
	t.Helper()
	dir, err := reg.SessionDir(id)
	if err != nil {
		t.Fatal(err)
	}
	st, ok, err := restore.LoadTerminal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st, ok
}

// TestWorkspaceTerminalTabsWarmShellSurvivesSwitchAndKill drives a real dock shell
// in workspace A: switching keeps the warm runtime and its process alive, switching
// back shows the same tabs, closing the workspace kills the process, the session
// directory keeps the terminal record, and a new manager restores that record as
// read-only dead tabs without auto-launching the shell.
func TestWorkspaceTerminalTabsWarmShellSurvivesSwitchAndKill(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA, rootB := t.TempDir(), t.TempDir()
	cfg := tabTestConfig(t)
	m := tabTestManager(t, rootA, cfg)
	var closeOnce sync.Once
	closeAll := func() { closeOnce.Do(m.closeAll) }
	t.Cleanup(closeAll)

	entries := m.Entries()
	if len(entries) != 1 {
		t.Fatalf("foreground entries = %+v", entries)
	}
	idA := entries[0].ID
	rtA := tabTestRuntime(t, m, idA)

	// Open a dock shell in workspace A and wait for its transcript.
	_, openA := rtA.model.Update(palette.RunCommandMsg{Action: "open_terminal"})
	pumpDock(t, rtA.model, openA, func() bool {
		return tabTestPid(tabTestPidPath(rootA)) != 0 && strings.Contains(rowsOf(rtA.model.TerminalState()), "READY")
	}, "the dock shell in workspace A")
	pidA := tabTestPid(tabTestPidPath(rootA))
	if !tabTestAlive(pidA) {
		t.Fatalf("the shell pid %d is not alive", pidA)
	}
	if got := len(rtA.model.TerminalState().Tabs); got != 1 {
		t.Fatalf("live tabs = %d, want 1", got)
	}

	// Create + switch to workspace B: A is captured and stays warm.
	created, createdErr := m.Create(rootB, "second")
	runWorkspaceCmd(t, created, createdErr)
	if err := m.waitWrites(10 * time.Second); err != nil {
		t.Fatalf("capture write: %v", err)
	}
	if !tabTestAlive(pidA) {
		t.Fatalf("switching away killed the warm tab's shell pid %d", pidA)
	}
	var idB string
	for _, e := range m.Entries() {
		if e.ID != idA {
			idB = e.ID
		}
	}
	if idB == "" {
		t.Fatalf("second workspace not listed: %+v", m.Entries())
	}
	if st, ok := tabTestSessionFile(t, m.reg, idA); !ok || len(st.Tabs) != 1 {
		t.Fatalf("captured terminal record = %+v, ok %v", st, ok)
	}

	// Open B's own shell, then switch back to A: both warm tabs keep running.
	rtB := tabTestRuntime(t, m, idB)
	_, openB := rtB.model.Update(palette.RunCommandMsg{Action: "open_terminal"})
	pumpDock(t, rtB.model, openB, func() bool {
		return tabTestPid(tabTestPidPath(rootB)) != 0 && strings.Contains(rowsOf(rtB.model.TerminalState()), "READY")
	}, "the dock shell in workspace B")
	pidB := tabTestPid(tabTestPidPath(rootB))
	selected, selectedErr := m.Select(idA)
	runWorkspaceCmd(t, selected, selectedErr)
	if got := tabTestRuntime(t, m, idA); got != rtA {
		t.Fatal("switching back rebuilt the runtime instead of reusing the warm one")
	}
	if !tabTestAlive(pidA) || !tabTestAlive(pidB) {
		t.Fatalf("a warm shell died during the switch: A %v B %v", tabTestAlive(pidA), tabTestAlive(pidB))
	}
	if got := len(rtA.model.TerminalState().Tabs); got != 1 {
		t.Fatalf("warm tabs after switching back = %d, want 1", got)
	}

	// Close the inactive workspace B: its shell is killed and its session data kept.
	closed, closedErr := m.Close(idB)
	runWorkspaceCmd(t, closed, closedErr)
	if err := m.waitWrites(10 * time.Second); err != nil {
		t.Fatalf("capture write: %v", err)
	}
	tabTestWaitDead(t, pidB, "closing workspace B")
	if _, ok := tabTestSessionFile(t, m.reg, idB); !ok {
		t.Fatal("closing the workspace removed its session directory terminal record")
	}
	if !tabTestAlive(pidA) {
		t.Fatalf("closing B killed the warm shell of A")
	}

	// Daemon exit saves and closes A, which kills its shell too.
	closeAll()
	tabTestWaitDead(t, pidA, "the daemon exit")
	if st, ok := tabTestSessionFile(t, m.reg, idA); !ok || len(st.Tabs) != 1 {
		t.Fatalf("daemon exit saved no terminal record for A: %+v, ok %v", st, ok)
	}

	// A new manager over the same registry restores A's tab read-only/dead, without
	// auto-launching the saved shell (its pid marker stays exactly as it was).
	before, err := os.ReadFile(tabTestPidPath(rootA))
	if err != nil {
		t.Fatal(err)
	}
	m2 := tabTestManager(t, rootA, cfg)
	t.Cleanup(m2.closeAll)
	rtA2 := tabTestRuntime(t, m2, idA)
	state := rtA2.model.TerminalState()
	if len(state.Tabs) != 1 || !strings.Contains(rowsOf(state), "READY") {
		t.Fatalf("restored workspace tabs = %+v", state)
	}
	time.Sleep(200 * time.Millisecond)
	after, err := os.ReadFile(tabTestPidPath(rootA))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("restoring the workspace auto-launched the shell again: pid %q -> %q", before, after)
	}
	if pid := tabTestPid(tabTestPidPath(rootA)); tabTestAlive(pid) {
		t.Fatalf("a restored workspace left a running shell (pid %d); restored tabs must be dead", pid)
	}
}

// TestWorkspaceTerminalTabsSavedStateRoundTrip injects a multi-tab terminal record
// into workspace A, switches away and back twice, and checks the per-workspace
// terminal.json carries every tab (names, active index, transcripts, launch
// records) without duplicating them; a fresh manager restores the same tabs
// read-only/dead and starts nothing.
func TestWorkspaceTerminalTabsSavedStateRoundTrip(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA, rootB := t.TempDir(), t.TempDir()
	cfg := tabTestConfig(t)
	m := tabTestManager(t, rootA, cfg)
	var closeOnce sync.Once
	closeAll := func() { closeOnce.Do(m.closeAll) }
	t.Cleanup(closeAll)

	idA := m.Entries()[0].ID
	rtA := tabTestRuntime(t, m, idA)
	injected := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{
			{Name: "build", Open: true, Cwd: "/w", Rows: []string{"old build"}},
			{
				Name: "agent", Open: true, Rows: []string{"agent log"},
				LaunchArgv: []string{"claude", "--session-id", "s1"},
				ResumeArgv: []string{"claude", "--resume", "s1"},
				Agent:      "claude", Ephemeral: true,
			},
		},
		ActiveTab: 1,
	}
	rtA.model.RestoreTerminal(injected)

	// Switch away twice with a switch back in between.
	switchTo := func(root string) {
		created, err := m.Create(root, "")
		runWorkspaceCmd(t, created, err)
		if err := m.waitWrites(10 * time.Second); err != nil {
			t.Fatalf("capture write: %v", err)
		}
	}
	selectA := func() {
		selected, err := m.Select(idA)
		runWorkspaceCmd(t, selected, err)
	}
	switchTo(rootB)
	var idB string
	for _, e := range m.Entries() {
		if e.ID != idA {
			idB = e.ID
		}
	}
	if idB == "" {
		t.Fatalf("second workspace not listed: %+v", m.Entries())
	}
	st, ok := tabTestSessionFile(t, m.reg, idA)
	if !ok || len(st.Tabs) != 2 || st.ActiveTab != 1 {
		t.Fatalf("captured record = %+v, ok %v", st, ok)
	}
	if st.Tabs[0].Name != "build" || st.Tabs[1].Name != "agent" || st.Tabs[1].Agent != "claude" || len(st.Tabs[1].ResumeArgv) != 3 {
		t.Fatalf("captured tabs lost their fields: %+v", st.Tabs)
	}
	selectA()
	if err := m.waitWrites(10 * time.Second); err != nil {
		t.Fatalf("capture write: %v", err)
	}
	// Leaving the tabless B workspace must not carry A's tabs into B's record.
	if stB, okB := tabTestSessionFile(t, m.reg, idB); okB && len(stB.Tabs) != 0 {
		t.Fatalf("workspace B's record has tabs from another workspace: %+v", stB.Tabs)
	}
	if rtA.model.TerminalState().ActiveTab != 1 || len(rtA.model.TerminalState().Tabs) != 2 {
		t.Fatalf("warm runtime lost the injected tabs: %+v", rtA.model.TerminalState())
	}
	switchTo(rootB)
	st, _ = tabTestSessionFile(t, m.reg, idA)
	if len(st.Tabs) != 2 {
		t.Fatalf("a repeated capture duplicated tabs: %d", len(st.Tabs))
	}

	// A fresh manager restores both tabs read-only and starts no shell.
	closeAll()
	m2 := tabTestManager(t, rootA, cfg)
	t.Cleanup(m2.closeAll)
	state := tabTestRuntime(t, m2, idA).model.TerminalState()
	if len(state.Tabs) != 2 || state.ActiveTab != 1 {
		t.Fatalf("restored state = %+v", state)
	}
	if state.Tabs[0].Name != "build" || state.Tabs[1].Agent != "claude" || state.Tabs[1].Ephemeral == false {
		t.Fatalf("restored tabs lost the saved fields: %+v", state.Tabs)
	}
	if pid := tabTestPid(tabTestPidPath(rootA)); tabTestAlive(pid) {
		t.Fatalf("restoring a workspace started a shell (pid %d); restored tabs must be dead", pid)
	}
}

// rowsOf joins the transcript rows of a saved terminal state, for the READY marker.
func rowsOf(st terminal.SavedState) string {
	var b strings.Builder
	for _, tab := range st.Tabs {
		for _, row := range tab.Rows {
			b.WriteString(row)
			b.WriteByte('\n')
		}
	}
	for _, row := range st.Rows {
		b.WriteString(row)
		b.WriteByte('\n')
	}
	return b.String()
}
