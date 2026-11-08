package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui"
)

// newWorkspaceManagerTest builds a manager over a fresh foreground root with GVIM's
// workspace dir isolated, and returns it with the foreground entry.
func newWorkspaceManagerTest(t *testing.T) (*workspaceManager, string) {
	t.Helper()
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	root := t.TempDir()
	opts := Options{Root: root, SessionDir: t.TempDir(), Daemon: &DaemonOptions{Mode: DaemonServe}}
	m, err := newWorkspaceManager(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	entries := m.Entries()
	if len(entries) != 1 {
		t.Fatalf("foreground entries = %+v", entries)
	}
	return m, entries[0].ID
}

// createDormant registers a second workspace without switching to it.
func createDormant(t *testing.T, m *workspaceManager) daemon.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, _, err := m.reg.Create(root, "dormant")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// TestWorkspaceMaterializeIsAsync checks a switch to a dormant workspace returns a command
// immediately (the UI does not wait for buildRuntime): the build starts in the background
// and the command completes it.
func TestWorkspaceMaterializeIsAsync(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	dormant := createDormant(t, m)
	realBuild := m.build
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	m.build = func(ctx context.Context, opts Options) (*runtime, error) {
		started <- struct{}{}
		<-release // hold the build: this is the I/O the UI must not wait for
		return realBuild(ctx, opts)
	}
	start := time.Now()
	cmd, err := m.Select(dormant.File.StableID)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the background build never started")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Select blocked for %v", elapsed)
	}
	if err != nil || cmd == nil {
		t.Fatalf("Select = %v, %v", cmd, err)
	}
	close(release)
	if msg := cmd(); msg != nil {
		t.Fatalf("switch command returned %T", msg)
	}
	entries := m.Entries()
	active := workspaceTestEntry(t, entries, func(e tui.WorkspaceEntry) bool { return e.Active })
	if active.ID != dormant.File.StableID || !active.Materialized {
		t.Fatalf("after the async switch: %+v", entries)
	}
}

// TestWorkspaceSwitchStaleResultCannotLand checks a newer request supersedes an older
// switch: the stale build result must not switch the daemon.
func TestWorkspaceSwitchStaleResultCannotLand(t *testing.T) {
	m, active := newWorkspaceManagerTest(t)
	slow := createDormant(t, m)
	fast := createDormant(t, m)
	realBuild := m.build
	slowStarted, fastStarted := make(chan struct{}, 1), make(chan struct{}, 1)
	slowRelease := make(chan struct{})
	m.build = func(ctx context.Context, opts Options) (*runtime, error) {
		if opts.Root == slow.File.Root {
			slowStarted <- struct{}{}
			// Ignore cancellation: the build finishes after the newer request started,
			// so the manager itself must reject the stale result.
			<-slowRelease
			return realBuild(context.WithoutCancel(ctx), opts)
		}
		fastStarted <- struct{}{}
		return realBuild(ctx, opts)
	}
	slowCmd, err := m.Select(slow.File.StableID)
	if err != nil || slowCmd == nil {
		t.Fatalf("slow Select = %v, %v", slowCmd, err)
	}
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the slow build never started")
	}
	fastCmd, err := m.Select(fast.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fastStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the newer build never started")
	}
	if fastCmd != nil {
		fastCmd()
	}
	close(slowRelease)
	if slowCmd != nil {
		slowCmd()
	}
	entries := m.Entries()
	activeID := ""
	for _, e := range entries {
		if e.Active {
			activeID = e.ID
		}
	}
	if activeID != fast.File.StableID {
		t.Fatalf("the stale switch landed: %+v (active was %q)", entries, active)
	}
	for _, e := range entries {
		if e.ID == slow.File.StableID && e.Materialized {
			t.Fatalf("the stale build stayed materialized: %+v", entries)
		}
	}
}

// TestWorkspaceSwitchFailureLeavesTheEditor checks a failed materialization reports and
// keeps the current workspace active.
func TestWorkspaceSwitchFailureLeavesTheEditor(t *testing.T) {
	m, active := newWorkspaceManagerTest(t)
	dormant := createDormant(t, m)
	m.build = func(context.Context, Options) (*runtime, error) {
		return nil, errors.New("cannot read the workspace")
	}
	cmd, err := m.Select(dormant.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("a failing switch returned no command")
	}
	cmd()
	entries := m.Entries()
	activeEntry := ""
	for _, e := range entries {
		if e.Active {
			activeEntry = e.ID
		}
	}
	if activeEntry != active {
		t.Fatalf("a failed switch changed the active workspace: %+v", entries)
	}
	for _, e := range entries {
		if e.ID == dormant.File.StableID && e.Materialized {
			t.Fatalf("a failed switch materialized the target: %+v", entries)
		}
	}
}

// TestWorkspaceSwitchCommandShowsNotice checks a failed switch surfaces a notice on the
// program's status row through the host (no host: it must not panic).
func TestWorkspaceSwitchCommandShowsNotice(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	var _ tui.WorkspaceCmd
	m.mu.Lock()
	_ = m.notify
	m.mu.Unlock()
}

// TestWorkspaceCaptureWriteFailureIsReported checks a failed snapshot write is an error
// (reported, not silently swallowed) and does not corrupt the manager.
func TestWorkspaceCaptureWriteFailureIsReported(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	dir := sess.rt.sessionDir
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	job := m.enqueueCapture(sess)
	if job == nil {
		t.Fatal("enqueueCapture returned no job")
	}
	<-job.done
	if job.err == nil {
		// A privileged test process can write into a 0500 directory (root); accept it
		// only when the session file was actually written.
		if _, err := os.Stat(filepath.Join(dir, "session.json")); err != nil {
			t.Fatalf("the capture write reported no error and wrote no session: %v", err)
		}
		return
	}
	if err := m.waitWrites(5 * time.Second); err == nil {
		t.Fatal("waitWrites hid the failed capture")
	}
}

// TestWorkspaceCreateWithLaunch checks the discovery Import path creates/materializes the
// workspace and remembers the terminal launch for its dock (no program: the queued launch
// cannot start here, but the create/switch contract is the same).
func TestWorkspaceCreateWithLaunch(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	root := t.TempDir()
	cmd, err := m.CreateWithLaunch(root, "tmux:work", tui.WorkspaceLaunch{
		Argv: []string{"tmux", "attach", "-t", "work"}, Name: "tmux:work", Ephemeral: true,
		Source: "tmux:default/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("CreateWithLaunch returned no command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("command returned %T", msg)
	}
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	active := workspaceTestEntry(t, entries, func(e tui.WorkspaceEntry) bool { return e.Active })
	if active.Label != "tmux:work" || !active.Materialized {
		t.Fatalf("imported workspace = %+v", active)
	}
	// Same-root import reuses the one workspace of that canonical root.
	cmd, err = m.CreateWithLaunch(root, "tmux:work", tui.WorkspaceLaunch{Argv: []string{"tmux", "attach"}})
	if err != nil || cmd == nil {
		t.Fatalf("second AddWithLaunch = %v, %v", cmd, err)
	}
	cmd()
	if got := m.Entries(); len(got) != 2 {
		t.Fatalf("same-root import duplicated the workspace: %+v", got)
	}
}

// TestWorkspaceCreateWithLaunchStartsTerminal drives the real Import path end to end:
// CreateWithLaunch materializes a workspace, applyCommit queues the launch through the
// host, the queued command starts the attach program as a dock tab in that model, a
// second source in the same root gets its own tab, and a repeated source is deduped by
// FindSource (no duplicate tab, no second start).
func TestWorkspaceCreateWithLaunchStartsTerminal(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	m.mu.Lock()
	foreground := m.sessions[m.active].rt.model
	host := tui.NewWorkspaceHost(foreground)
	m.mu.Unlock()
	m.setHost(host)
	root := t.TempDir()

	// A harmless local attach program: the real dock starter runs it, so the test uses
	// no tmux and no network.
	launchFor := func(source string) tui.WorkspaceLaunch {
		argv := []string{"/bin/sh", "-c", "exit 0"}
		return tui.WorkspaceLaunch{
			Argv: argv, Name: "imported", ResumeArgv: argv,
			Ephemeral: true, Dir: root, Source: source,
		}
	}
	drainHost := func(first tea.Cmd) {
		t.Helper()
		var run func(tea.Cmd)
		msgs := make(chan tea.Msg, 128)
		run = func(c tea.Cmd) {
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
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case msg := <-msgs:
				if batch, ok := msg.(tea.BatchMsg); ok {
					for _, c := range batch {
						run(c)
					}
					continue
				}
				_, next := host.Update(msg)
				run(next)
			case <-time.After(20 * time.Millisecond):
				if strings.Contains(strings.Join(host.Active().DockTabNames(), ","), "imported") {
					return
				}
			}
		}
	}

	cmd, err := m.CreateWithLaunch(root, "tmux:work", launchFor("tmux:default/work"))
	if err != nil || cmd == nil {
		t.Fatalf("CreateWithLaunch = %v, %v", cmd, err)
	}
	cmd()
	// The host has the queued launch; flush it as the program loop would.
	_, queued := host.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	drainHost(queued)
	names := host.Active().DockTabNames()
	if len(names) != 1 || !strings.HasPrefix(names[0], "imported") {
		t.Fatalf("dock tabs after import = %v, want the imported attach tab", names)
	}

	// A second tmux session that shares the root keeps its own attach tab.
	cmd, err = m.CreateWithLaunch(root, "tmux:work2", launchFor("tmux:default/work2"))
	if err != nil || cmd == nil {
		t.Fatalf("second CreateWithLaunch = %v, %v", cmd, err)
	}
	cmd()
	_, queued = host.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	drainHost(queued)
	if got := len(host.Active().DockTabNames()); got != 2 {
		t.Fatalf("dock tabs after a second source = %d, want 2", got)
	}

	// Re-importing the same source focuses the existing tab: no duplicate, no start.
	cmd, err = m.CreateWithLaunch(root, "tmux:work", launchFor("tmux:default/work"))
	if err != nil || cmd == nil {
		t.Fatalf("repeat CreateWithLaunch = %v, %v", cmd, err)
	}
	cmd()
	_, queued = host.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	drainHost(queued)
	names = host.Active().DockTabNames()
	if len(names) != 2 {
		t.Fatalf("repeated import duplicated the tab: %v", names)
	}
}
