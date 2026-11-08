package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// TestSharedBuffersForDaemonOnly: only a daemon-hosted local launch gets a registry; a
// direct launch and a remote workspace keep independent buffers.
func TestSharedBuffersForDaemonOnly(t *testing.T) {
	t.Parallel()
	if sharedBuffersFor(Options{}) != nil {
		t.Fatal("a direct launch got a shared-buffer registry")
	}
	if sharedBuffersFor(Options{Daemon: &DaemonOptions{}}) != nil {
		t.Fatal("a daemon launch without a registry got one")
	}
	reg := editor.NewSharedBuffers()
	if got := sharedBuffersFor(Options{Daemon: &DaemonOptions{}, sharedBuffers: reg}); got != reg {
		t.Fatalf("daemon registry = %p, want %p", got, reg)
	}
	if sharedBuffersFor(Options{Daemon: &DaemonOptions{}, Remote: &authority.RemoteSpec{}, sharedBuffers: reg}) != nil {
		t.Fatal("a remote workspace used the shared-buffer registry")
	}
}

// TestWorkspaceSharedBuffersAcrossWorkspaces drives the daemon wiring end to end: a file
// opened in the foreground workspace, then in a second materialized workspace, becomes a
// read-only mirror there until the workspace switch takes ownership; edits propagate; a
// denied request leaves a conflict notice; closing the owner keeps the buffer alive in
// the other workspace.
func TestWorkspaceSharedBuffersAcrossWorkspaces(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	rootA := t.TempDir()
	rootB := t.TempDir()
	path := filepath.Join(rootA, "shared.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := newWorkspaceManager(t.Context(), Options{
		Root:       rootA,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	if m.shared == nil {
		t.Fatal("the daemon manager has no shared-buffer registry")
	}
	m.mu.Lock()
	aSession := m.sessions[m.active]
	m.mu.Unlock()
	if aSession == nil || aSession.rt == nil {
		t.Fatal("no foreground workspace runtime")
	}
	if err := aSession.rt.ed.OpenFile(path); err != nil {
		t.Fatalf("open in first workspace: %v", err)
	}
	if aSession.rt.ed.Active().SharedMirror() {
		t.Fatal("the first opener is a mirror")
	}
	created, createdErr := m.Create(rootB, "second")
	runWorkspaceCmd(t, created, createdErr)
	m.mu.Lock()
	bSession := m.sessions[m.active]
	m.mu.Unlock()
	if bSession == nil || bSession.rt == nil || bSession == aSession {
		t.Fatal("the second workspace did not materialize")
	}
	if err := bSession.rt.ed.OpenFile(path); err != nil {
		t.Fatalf("open in the second workspace: %v", err)
	}
	// The second workspace is focused, so it took ownership; the first is its mirror.
	if bSession.rt.ed.Active().SharedMirror() || bSession.rt.ed.Active().ReadOnly() {
		t.Fatal("the focused second workspace did not take ownership")
	}
	if !aSession.rt.ed.Active().SharedMirror() || !aSession.rt.ed.Active().ReadOnly() {
		t.Fatal("the first workspace is not a read-only mirror")
	}
	bSession.rt.ed.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "X")
	if got := aSession.rt.ed.Active().Buffer.Text(); got != "Xone\ntwo\n" {
		t.Fatalf("mirror text after edit = %q", got)
	}
	// A denied ownership request on switching back leaves the conflict in the mirror.
	m.shared.Deny = func(owner, requester *editor.Editor) error { return os.ErrPermission }
	t.Cleanup(func() { m.shared.Deny = nil })
	selectCmd, selectErr := m.Select(aSession.ws.File.StableID)
	runWorkspaceCmd(t, selectCmd, selectErr)
	if !aSession.rt.ed.Active().SharedMirror() {
		t.Fatal("the denied switch changed ownership")
	}
	if !strings.Contains(aSession.rt.ed.Active().Status, "conflict") {
		t.Fatalf("conflict status = %q", aSession.rt.ed.Active().Status)
	}
	// Clearing the refusal and switching away and back takes ownership normally (a
	// select of the already-active workspace is a no-op by design).
	m.shared.Deny = nil
	bCmd, bErr := m.Select(bSession.ws.File.StableID)
	runWorkspaceCmd(t, bCmd, bErr)
	aCmd, aErr := m.Select(aSession.ws.File.StableID)
	runWorkspaceCmd(t, aCmd, aErr)
	if aSession.rt.ed.Active().SharedMirror() {
		t.Fatal("the cleared switch did not take ownership")
	}
	// Closing the owning workspace promotes the mirror in the other workspace.
	m.shared.Deny = nil
	if aSession.ws.File.StableID == m.active {
		bCmd, bErr := m.Select(bSession.ws.File.StableID)
		runWorkspaceCmd(t, bCmd, bErr)
	}
	closeCmd, closeErr := m.Close(aSession.ws.File.StableID)
	runWorkspaceCmd(t, closeCmd, closeErr)
	m.mu.Lock()
	bSession = m.sessions[m.active]
	m.mu.Unlock()
	if bSession == nil {
		t.Fatal("the surviving workspace disappeared")
	}
	if bSession.rt.ed.Active().SharedMirror() || bSession.rt.ed.Active().ReadOnly() {
		t.Fatal("closing the owner lost the buffer instead of promoting the mirror")
	}
	if got := bSession.rt.ed.Active().Buffer.Text(); got == "" {
		t.Fatal("the promoted buffer is empty")
	}
}
