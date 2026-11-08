//go:build unix

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// TestWorkspaceCloseReportsRunningShells checks the close polish on a real dock shell: the
// workspace layer can name the running terminal before the close (the TUI confirmation
// uses it), and closing reports exactly what was stopped - both for a direct manager close
// and for the socket protocol's close answer. The close kills the shell as before.
func TestWorkspaceCloseReportsRunningShells(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA := t.TempDir()
	cfg := tabTestConfig(t)
	m := tabTestManager(t, rootA, cfg)
	t.Cleanup(m.closeAll)

	entries := m.Entries()
	if len(entries) != 1 {
		t.Fatalf("foreground entries = %+v", entries)
	}
	idA := entries[0].ID
	rtA := tabTestRuntime(t, m, idA)
	_, openA := rtA.model.Update(palette.RunCommandMsg{Action: "open_terminal"})
	pumpDock(t, rtA.model, openA, func() bool {
		return tabTestPid(tabTestPidPath(rootA)) != 0 && strings.Contains(rowsOf(rtA.model.TerminalState()), "READY")
	}, "the dock shell in workspace A")
	pidA := tabTestPid(tabTestPidPath(rootA))
	if !tabTestAlive(pidA) {
		t.Fatalf("the shell pid %d is not alive", pidA)
	}
	// The close confirmation's source: exactly one running terminal, named.
	running := m.RunningTerminals(idA)
	if len(running) != 1 || running[0].Name == "" {
		t.Fatalf("RunningTerminals = %+v", running)
	}
	if got := runningTerminalSummary(running); !strings.Contains(got, "1 running terminal:") {
		t.Fatalf("summary = %q", got)
	}

	// A second workspace takes over as active, leaving A warm with its shell.
	rootB := t.TempDir()
	created, createdErr := m.Create(rootB, "second")
	runWorkspaceCmd(t, created, createdErr)
	var idB string
	for _, e := range m.Entries() {
		if e.ID != idA {
			idB = e.ID
		}
	}
	if idB == "" {
		t.Fatalf("second workspace not listed: %+v", m.Entries())
	}
	if got := m.RunningTerminals(idA); len(got) != 1 {
		t.Fatalf("warm workspace's running terminals = %+v", got)
	}

	// Closing A stops the shell and reports exactly what it stopped.
	if err := m.closeWorkspace(idA); err != nil {
		t.Fatalf("closeWorkspace = %v", err)
	}
	if !strings.Contains(m.lastNotice, "Closed workspace") || !strings.Contains(m.lastNotice, "1 running terminal:") {
		t.Fatalf("close report = %q", m.lastNotice)
	}
	deadline := time.Now().Add(10 * time.Second)
	for tabTestAlive(pidA) {
		if time.Now().After(deadline) {
			t.Fatalf("the closed workspace's shell pid %d is still alive", pidA)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.Entries(); len(got) != 1 || got[0].ID != idB {
		t.Fatalf("entries after close = %+v", got)
	}

	// The socket path answers the same report: open a shell in B, switch to a third
	// workspace, then close B through the workspace protocol.
	rtB := tabTestRuntime(t, m, idB)
	_, openB := rtB.model.Update(palette.RunCommandMsg{Action: "open_terminal"})
	pumpDock(t, rtB.model, openB, func() bool {
		return tabTestPid(tabTestPidPath(rootB)) != 0 && strings.Contains(rowsOf(rtB.model.TerminalState()), "READY")
	}, "the dock shell in workspace B")
	rootC := t.TempDir()
	createdC, createdCErr := m.Create(rootC, "third")
	runWorkspaceCmd(t, createdC, createdCErr)
	res := m.handleRequest(daemon.WorkspaceRequest{Close: &daemon.WorkspaceClose{ID: idB}})
	if !strings.Contains(res.Notice, "stopped 1 running terminal:") {
		t.Fatalf("protocol close notice = %q", res.Notice)
	}
}
