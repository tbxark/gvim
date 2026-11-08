package app

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
)

// daemonTestHome sets a short HOME so the daemon socket path fits the Unix socket
// address limit, and returns the socket name the test uses.
func daemonTestHome(t *testing.T, name string) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "gvimapp")
	if err != nil {
		t.Skipf("no short temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	return name
}

// startDaemonTest runs the daemon launch in the background and waits for its socket.
func startDaemonTest(t *testing.T, opts Options, name string) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), opts) }()
	path, err := daemon.SocketPath(name)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !daemon.IsLive(path) {
		if time.Now().After(deadline) {
			t.Fatal("the daemon did not start listening")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return done
}

// attachDaemonTest attaches a raw protocol client and completes the hello exchange.
func attachDaemonTest(t *testing.T, name string) net.Conn {
	t.Helper()
	conn, err := daemon.Attach(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.WriteJSON(conn, daemon.TypeHello, daemon.Hello{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.WriteFrame(conn, daemon.TypePing, nil); err != nil {
		t.Fatal(err)
	}
	// The hosted program may write repaint frames while the ping travels: read
	// until the pong instead of assuming the very next frame is it.
	readDaemonUntil(t, conn, daemon.TypePong)
	return conn
}

// readDaemonUntil reads frames until one of typ arrives, dropping output frames.
func readDaemonUntil(t *testing.T, conn net.Conn, typ byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("no frame of type %d within 5s", typ)
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		f, err := daemon.ReadFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read frame type %d: %v", typ, err)
		}
		if f.Type == typ {
			return
		}
	}
}

// drainDaemonOutput reads frames until one output payload carries text or the
// deadline passes, and returns the collected output.
func drainDaemonOutput(t *testing.T, conn net.Conn, want bool) string {
	t.Helper()
	var out []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		f, err := daemon.ReadFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if want && len(out) > 0 {
					return string(out)
				}
				continue
			}
			return string(out)
		}
		switch f.Type {
		case daemon.TypeOutput:
			out = append(out, f.Payload...)
			if want && len(out) > 0 {
				// The first frames carry the welcome screen; give the renderer a
				// couple of frames more so the attached test sees a full paint.
				_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				for {
					more, err := daemon.ReadFrame(conn)
					if err != nil {
						return string(out)
					}
					if more.Type == daemon.TypeOutput {
						out = append(out, more.Payload...)
					}
				}
			}
		case daemon.TypeExit, daemon.TypeDetached:
			return string(out)
		}
	}
	return string(out)
}

// TestDaemonHostsAttachDetachAndQuit drives the daemon end to end: it starts the
// hosted editor, attaches a client, detaches it (the editor keeps running), reattaches
// and quits the session, and the daemon saves the session on exit.
func TestDaemonHostsAttachDetachAndQuit(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-daemon")
	root := t.TempDir()
	sessionDir := t.TempDir()
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{
		Root:       root,
		SessionDir: sessionDir,
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)

	conn := attachDaemonTest(t, name)
	if got := drainDaemonOutput(t, conn, true); len(got) == 0 {
		t.Fatal("the attached client received no frame")
	}
	// Detach: the client is dropped, the editor keeps running.
	if err := daemon.WriteFrame(conn, daemon.TypeDetach, nil); err != nil {
		t.Fatal(err)
	}
	readDaemonUntil(t, conn, daemon.TypeDetached)
	_ = conn.Close()

	// Reattach: a fresh client gets a repaint.
	conn2 := attachDaemonTest(t, name)
	if got := drainDaemonOutput(t, conn2, true); len(got) == 0 {
		t.Fatal("the reattached client received no frame")
	}
	// Quit the hosted session with ctrl+q (the default keymap's quit).
	if err := daemon.WriteFrame(conn2, daemon.TypeInput, []byte{0x11}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not quit after ctrl+q")
	}
	// The daemon exit saved the workspace session in its per-workspace session
	// directory; detaching alone did not remove it.
	sessions, err := filepath.Glob(filepath.Join(workspacesDir, "sessions", "*", "session.json"))
	if err != nil || len(sessions) != 1 {
		t.Fatalf("the daemon exit saved no workspace session: %v, %v", sessions, err)
	}
	workspaceFiles, err := filepath.Glob(filepath.Join(workspacesDir, "*.json"))
	if err != nil || len(workspaceFiles) != 1 {
		t.Fatalf("the daemon exit left no workspace registry file: %v, %v", workspaceFiles, err)
	}
}

// TestDaemonSingleWriterRefusal checks the direct launch policy: while the daemon
// hosts the workspace under the per-root default name, a bare direct launch refuses
// instead of opening a second editor of the same session.
func TestDaemonSingleWriterRefusal(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	resolved, err := resolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	name := daemon.DefaultName(resolved)
	_ = daemonTestHome(t, name)
	opts := Options{
		Root:       root,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)
	t.Cleanup(func() {
		conn := attachDaemonTest(t, name)
		_ = daemon.WriteFrame(conn, daemon.TypeInput, []byte{0x11})
		_ = conn.Close()
		select {
		case err := <-done:
			_ = err
		case <-time.After(10 * time.Second):
		}
	})
	bare := Options{Root: root, SessionDir: t.TempDir()}
	if err := refuseIfDaemonRunning(bare); err == nil {
		t.Fatal("a bare launch did not refuse while the daemon hosts the workspace")
	}
	// A file launch and --simple stay direct launches.
	if err := refuseIfDaemonRunning(Options{Root: root, Files: []FileTarget{{Path: filepath.Join(root, "a.txt")}}}); err != nil {
		t.Fatalf("a file launch must stay direct: %v", err)
	}
	if err := refuseIfDaemonRunning(Options{Root: root, Simple: true}); err != nil {
		t.Fatalf("--simple must stay direct: %v", err)
	}
}

// daemonWorkspaceCall sends one workspace frame and returns the answered list.
func daemonWorkspaceCall(t *testing.T, conn net.Conn, typ byte, payload any) daemon.WorkspaceList {
	t.Helper()
	var err error
	if payload == nil {
		err = daemon.WriteFrame(conn, typ, nil)
	} else {
		err = daemon.WriteJSON(conn, typ, payload)
	}
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("no workspace answer within 15s")
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		f, err := daemon.ReadFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read workspace answer: %v", err)
		}
		switch f.Type {
		case daemon.TypeWorkspaces:
			var list daemon.WorkspaceList
			if err := daemon.DecodeJSON(f, &list); err != nil {
				t.Fatal(err)
			}
			return list
		case daemon.TypeNotice:
			t.Fatalf("daemon notice: %s", f.Payload)
		}
	}
}

// TestDaemonWorkspaceProtocol drives the #19a workspace lifecycle over the real daemon
// protocol: list, new, select, rename and close, with per-workspace materialization and
// session directories.
func TestDaemonWorkspaceProtocol(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-workspaces")
	root1 := t.TempDir()
	root2 := t.TempDir()
	resolved1, err := resolveRoot(root1)
	if err != nil {
		t.Fatal(err)
	}
	resolved2, err := resolveRoot(root2)
	if err != nil {
		t.Fatal(err)
	}
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{
		Root:       root1,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })

	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if len(list.Entries) != 1 || list.Active != list.Entries[0].ID {
		t.Fatalf("initial list = %+v", list)
	}
	first := list.Entries[0]
	if first.Root != resolved1 || !first.Materialized || !first.Active {
		t.Fatalf("foreground workspace = %+v (want root %q)", first, resolved1)
	}
	// Create (and switch to) a second workspace.
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceNew, daemon.WorkspaceNew{Root: root2, Label: "second"})
	if len(list.Entries) != 2 {
		t.Fatalf("after new, list = %+v", list)
	}
	var second daemon.WorkspaceInfo
	for _, e := range list.Entries {
		if e.Root == resolved2 {
			second = e
		}
	}
	if second.ID == "" || second.Label != "second" || !second.Active || !second.Materialized {
		t.Fatalf("new workspace = %+v", second)
	}
	if list.Active != second.ID {
		t.Fatalf("active = %q, want the new workspace", list.Active)
	}
	// Switch back: the first workspace keeps its warm runtime.
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceSelect, daemon.WorkspaceSelect{ID: first.ID})
	if list.Active != first.ID {
		t.Fatalf("after select, active = %q, want %q", list.Active, first.ID)
	}
	for _, e := range list.Entries {
		if e.ID == second.ID && !e.Materialized {
			t.Fatalf("the warm workspace was dropped: %+v", list)
		}
	}
	// Rename the active workspace.
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceRename, daemon.WorkspaceRename{ID: first.ID, Label: "renamed"})
	found := false
	for _, e := range list.Entries {
		if e.ID == first.ID && e.Label == "renamed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rename not reflected: %+v", list)
	}
	// Close the inactive workspace: its runtime closes, its session data stays.
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceClose, daemon.WorkspaceClose{ID: second.ID})
	if len(list.Entries) != 1 || list.Entries[0].ID != first.ID {
		t.Fatalf("after close, list = %+v", list)
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, "sessions", second.ID, "session.json")); err != nil {
		t.Fatalf("close removed the closed workspace's session data: %v", err)
	}
	// The closed workspace's registry file is gone.
	if _, err := os.Stat(filepath.Join(workspacesDir, "sessions")); err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	// Quit: the active workspace's session is saved.
	if err := daemon.WriteFrame(conn, daemon.TypeInput, []byte{0x11}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the daemon did not quit")
	}
	if _, err := os.Stat(filepath.Join(workspacesDir, "sessions", first.ID, "session.json")); err != nil {
		t.Fatalf("the active workspace session was not saved on exit: %v", err)
	}
}

// drainDaemonForText reads frames until the collected output contains want.
func drainDaemonForText(t *testing.T, conn net.Conn, want string) string {
	t.Helper()
	var out []byte
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		f, err := daemon.ReadFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read while waiting for %q: %v", want, err)
		}
		if f.Type == daemon.TypeOutput {
			out = append(out, f.Payload...)
			if strings.Contains(string(out), want) {
				return string(out)
			}
		}
	}
	t.Fatalf("output did not contain %q:\n%s", want, out)
	return ""
}

// TestDaemonWorkspaceSwitchPreservesUnsavedText checks a daemon switch captures the
// workspace (layout, unsaved stash, terminal record) and switching back restores it: the
// typed text and the open file come back.
func TestDaemonWorkspaceSwitchPreservesUnsavedText(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-ws-switch")
	root1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(root1, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root2 := t.TempDir()
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{
		Root:       root1,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })
	// Give the hosted program its size explicitly (the daemon may have finished
	// building after this client's hello).
	if err := daemon.WriteJSON(conn, daemon.TypeResize, daemon.Resize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}

	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if len(list.Entries) != 1 {
		t.Fatalf("initial list = %+v", list)
	}
	first := list.Entries[0]
	// Open a file in the foreground workspace and wait for its text.
	if err := daemon.WriteJSON(conn, daemon.TypeOpenFile, daemon.OpenFile{Paths: []string{filepath.Join(root1, "a.txt")}}); err != nil {
		t.Fatal(err)
	}
	drainDaemonForText(t, conn, "hello")
	// Type an unsaved change.
	if err := daemon.WriteFrame(conn, daemon.TypeInput, []byte("X")); err != nil {
		t.Fatal(err)
	}
	drainDaemonForText(t, conn, "Xhello")

	// Switch to a second workspace, then back: the typed text comes back through the
	// session capture/restore of the first workspace.
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceNew, daemon.WorkspaceNew{Root: root2, Label: "other"})
	second := list.Entries[0]
	for _, e := range list.Entries {
		if e.ID != first.ID {
			second = e
		}
	}
	if list.Active != second.ID {
		t.Fatalf("not switched to the new workspace: %+v", list)
	}
	// The captured session of the first workspace has the file and its unsaved stash.
	sessionPath := filepath.Join(workspacesDir, "sessions", first.ID, "session.json")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(sessionPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the switched-away workspace saved no session at %s", sessionPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "a.txt") {
		t.Fatalf("the captured session has no open file: %s", raw)
	}
	if !strings.Contains(string(raw), "stashFile") {
		t.Fatalf("the captured session has no stash for the unsaved text: %s", raw)
	}
	// Single writer: opening a file that the switched-away workspace has open brings
	// that workspace back instead of opening a second copy in the active one.
	if err := daemon.WriteJSON(conn, daemon.TypeOpenFile, daemon.OpenFile{Paths: []string{filepath.Join(root1, "a.txt")}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
		if list.Active == first.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("open-file did not switch back to the file's workspace: %+v", list)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := func() (string, bool) {
		for _, e := range list.Entries {
			if e.ID == second.ID {
				return e.ID, e.Materialized
			}
		}
		return "", false
	}(); !ok {
		t.Fatalf("the warm second workspace was dropped by the switch: %+v", list)
	}
	// Force a fresh paint (the repaint after the switch was consumed while polling the
	// workspace list) and check the unsaved text is visible again.
	if err := daemon.WriteJSON(conn, daemon.TypeResize, daemon.Resize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	drainDaemonForText(t, conn, "Xhello")
	// Quit and make sure the daemon exits cleanly.
	if err := daemon.WriteFrame(conn, daemon.TypeInput, []byte{0x11}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the daemon did not quit")
	}
}
