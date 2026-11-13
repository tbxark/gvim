package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/session"
)

// precreateDormantWorkspace registers a workspace with a saved layout (and optionally a
// stash) before the daemon starts, so the daemon discovers it as a dormant workspace.
func precreateDormantWorkspace(t *testing.T, workspacesDir, root, label string, files []session.FileState) (id string, dir string) {
	t.Helper()
	reg, err := daemon.OpenRegistry(workspacesDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.Create(root, label)
	if err != nil {
		t.Fatal(err)
	}
	dir, err = reg.SessionDir(ws.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	layout := session.Session{Version: 1, Root: root, Files: files}
	if err := session.Save(dir, layout); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.UpdateLayout(ws.File.StableID, &layout); err != nil {
		t.Fatal(err)
	}
	return ws.File.StableID, dir
}

// waitDaemonProcessed sends a ping and waits for the pong: the daemon handles one client's
// frames in order, so a pong after an open_file means the open was handled.
func waitDaemonProcessed(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := daemon.WriteFrame(conn, daemon.TypePing, nil); err != nil {
		t.Fatal(err)
	}
	readDaemonUntil(t, conn, daemon.TypePong)
}

// requestOpen sends one open_file frame and waits until the daemon handled it. The caller
// then forces a full repaint (sendResize) before checking the painted output: a daemon
// client receives ANSI diffs, so a reused screen updates only changed cells.
func requestOpen(t *testing.T, conn net.Conn, req daemon.OpenFile) {
	t.Helper()
	if err := daemon.WriteJSON(conn, daemon.TypeOpenFile, req); err != nil {
		t.Fatal(err)
	}
	waitDaemonProcessed(t, conn)
}

// sendResize forces the program to repaint the whole frame for the attached client.
func sendResize(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := daemon.WriteJSON(conn, daemon.TypeResize, daemon.Resize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
}

// quitDaemon sends ctrl+q and waits for the daemon to return.
func quitDaemon(t *testing.T, conn net.Conn, done <-chan error) {
	t.Helper()
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

// quitDaemonDiscard sends ctrl+q and answers the one unsaved-changes confirmation with
// Discard, then waits for the daemon to return. Use it when a modified tab (in the active
// or in a background workspace) keeps a plain ctrl+q waiting for an answer.
func quitDaemonDiscard(t *testing.T, conn net.Conn, done <-chan error) {
	t.Helper()
	if err := daemon.WriteFrame(conn, daemon.TypeInput, []byte{0x11, 'd'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the daemon did not quit after answering the quit confirmation")
	}
}

// TestOpenFileFromAnotherClientCwd checks a named daemon opens a client's relative
// file:line:col from the client's own working directory.
func TestOpenFileFromAnotherClientCwd(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-openfile-cwd")
	root := t.TempDir()
	clientCwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(clientCwd, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientCwd, "sub", "f.txt"), []byte("line1\nline2\nline3\nline4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientCwd, "plain.txt"), []byte("PLAIN-CONTENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{
		Root:       root,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })

	// Positional form: a path relative to the CLIENT cwd, at 3:2.
	requestOpen(t, conn, daemon.OpenFile{
		Cwd:   clientCwd,
		Files: []daemon.OpenFileTarget{{Path: "sub/f.txt", Line: 3, Col: 2}},
	})
	sendResize(t, conn)
	out := drainDaemonForText(t, conn, "Ln 3, Col 2")
	if !strings.Contains(out, "line1") && !strings.Contains(out, "f.txt") {
		t.Fatalf("the client's file was not painted:\n%s", out)
	}
	// A position-less target is also resolved against the client cwd.
	requestOpen(t, conn, daemon.OpenFile{Cwd: clientCwd, Files: []daemon.OpenFileTarget{{Path: "plain.txt"}}})
	sendResize(t, conn)
	drainDaemonForText(t, conn, "PLAIN-CONTENT")
	quitDaemon(t, conn, done)
}

// TestOpenFileDormantOwnerMaterializesAndSwitches checks a path saved in a dormant
// workspace's layout is opened there: the workspace materializes, becomes active, the
// saved position is preserved, and a later request honours a requested position.
func TestOpenFileDormantOwnerMaterializesAndSwitches(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-openfile-dormant")
	rootA := t.TempDir()
	rootB := t.TempDir()
	fileB := filepath.Join(rootB, "b.txt")
	if err := os.WriteFile(fileB, []byte("b1\nb2\nb3\nb4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspacesDir := t.TempDir()
	idB, _ := precreateDormantWorkspace(t, workspacesDir, rootB, "B", []session.FileState{{Path: fileB, Line: 3, Col: 2}})
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{
		Root:       rootA,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	var bInfo daemon.WorkspaceInfo
	for _, e := range list.Entries {
		if e.ID == idB {
			bInfo = e
		}
	}
	if bInfo.ID == "" || bInfo.Materialized {
		t.Fatalf("B is not dormant at start: %+v", list)
	}
	// No requested position: the saved 3:2 is preserved.
	requestOpen(t, conn, daemon.OpenFile{Files: []daemon.OpenFileTarget{{Path: fileB}}})
	sendResize(t, conn)
	out := drainDaemonForText(t, conn, "Ln 3, Col 2")
	if !strings.Contains(out, "b1") && !strings.Contains(out, "b.txt") {
		t.Fatalf("the dormant workspace's file was not painted:\n%s", out)
	}
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if list.Active != idB {
		t.Fatalf("the dormant owner was not activated: %+v", list)
	}
	for _, e := range list.Entries {
		if e.ID == idB && !e.Materialized {
			t.Fatalf("the owner stayed dormant: %+v", list)
		}
	}
	// The same request again focuses the existing tab (no duplicate) and honours the
	// requested position.
	requestOpen(t, conn, daemon.OpenFile{Files: []daemon.OpenFileTarget{{Path: fileB, Line: 2, Col: 1}}})
	sendResize(t, conn)
	drainDaemonForText(t, conn, "Ln 2, Col 1")
	quitDaemon(t, conn, done)
}

// TestOpenFileStaleDormantLayoutIgnored checks a saved layout entry whose file is gone and
// has no unsaved stash does not materialize the workspace: the request opens in the active
// workspace instead, and the dormant workspace stays dormant.
func TestOpenFileStaleDormantLayoutIgnored(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-openfile-stale")
	rootA := t.TempDir()
	rootB := t.TempDir()
	gone := filepath.Join(rootB, "gone.txt") // never created: stale layout entry
	workspacesDir := t.TempDir()
	idB, _ := precreateDormantWorkspace(t, workspacesDir, rootB, "B", []session.FileState{{Path: gone, Line: 5}})
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{Root: rootA, SessionDir: t.TempDir(), Daemon: &DaemonOptions{Mode: DaemonServe, Name: name}}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })
	requestOpen(t, conn, daemon.OpenFile{Files: []daemon.OpenFileTarget{{Path: gone}}})
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	for _, e := range list.Entries {
		if e.ID == idB && (e.Materialized || e.Active) {
			t.Fatalf("a stale layout materialized its workspace: %+v", list)
		}
	}
	quitDaemon(t, conn, done)
}

// TestOpenFileDormantStashIsRestored checks a dormant layout entry whose file is gone but
// has unsaved stash text is materialized and shows the unsaved text.
func TestOpenFileDormantStashIsRestored(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-openfile-stash")
	rootA := t.TempDir()
	rootB := t.TempDir()
	gone := filepath.Join(rootB, "gone.txt")
	workspacesDir := t.TempDir()
	idB, dirB := precreateDormantWorkspace(t, workspacesDir, rootB, "B", nil)
	stashName, err := session.StashContent(dirB, "UNSAVED-STASH-TEXT\n")
	if err != nil {
		t.Fatal(err)
	}
	layout := session.Session{Version: 1, Root: rootB, Files: []session.FileState{{
		Path: gone, StashFile: stashName, Line: 1,
	}}}
	if err := session.Save(dirB, layout); err != nil {
		t.Fatal(err)
	}
	reg, err := daemon.OpenRegistry(workspacesDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.UpdateLayout(idB, &layout); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{Root: rootA, SessionDir: t.TempDir(), Daemon: &DaemonOptions{Mode: DaemonServe, Name: name}}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })
	requestOpen(t, conn, daemon.OpenFile{Files: []daemon.OpenFileTarget{{Path: gone}}})
	sendResize(t, conn)
	drainDaemonForText(t, conn, "UNSAVED-STASH-TEXT")
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if list.Active != idB {
		t.Fatalf("the stashed dormant owner was not activated: %+v", list)
	}
	quitDaemonDiscard(t, conn, done)
}

// TestOpenTargetsFromRequestResolvesCwd checks the pure request resolution: client-relative
// paths against Cwd, absolute paths unchanged.
func TestOpenTargetsFromRequestResolvesCwd(t *testing.T) {
	cwd := t.TempDir()
	got := openTargetsFromRequest(daemon.OpenFile{
		Cwd: cwd,
		Files: []daemon.OpenFileTarget{
			{Path: "rel/a.txt", Line: 3, Col: 2},
			{Path: filepath.Join(cwd, "abs.txt")},
		},
	})
	if len(got) != 2 {
		t.Fatalf("targets = %+v, want 2", got)
	}
	if got[0].path != filepath.Join(cwd, "rel", "a.txt") || got[0].line != 3 || got[0].col != 2 {
		t.Fatalf("relative target = %+v", got[0])
	}
	if got[1].path != filepath.Join(cwd, "abs.txt") {
		t.Fatalf("absolute target = %+v", got[1])
	}
}

// TestOpenFileDormantOwnerMaterializeFailureKeepsCurrentState checks a detected dormant
// owner that cannot be materialized is an error: the current workspace/state is kept and
// the file is NOT re-opened in another workspace (single-writer).
func TestOpenFileDormantOwnerMaterializeFailureKeepsCurrentState(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-openfile-fail")
	rootA := t.TempDir()
	rootB := t.TempDir()
	gone := filepath.Join(rootB, "gone.txt")
	workspacesDir := t.TempDir()
	idB, dirB := precreateDormantWorkspace(t, workspacesDir, rootB, "B", nil)
	stashName, err := session.StashContent(dirB, "STASHED\n")
	if err != nil {
		t.Fatal(err)
	}
	layout := session.Session{Version: 1, Root: rootB, Files: []session.FileState{{Path: gone, StashFile: stashName, Line: 1}}}
	if err := session.Save(dirB, layout); err != nil {
		t.Fatal(err)
	}
	reg, err := daemon.OpenRegistry(workspacesDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.UpdateLayout(idB, &layout); err != nil {
		t.Fatal(err)
	}
	// The workspace root disappears: its materialization must fail.
	if err := os.RemoveAll(rootB); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	opts := Options{Root: rootA, SessionDir: t.TempDir(), Daemon: &DaemonOptions{Mode: DaemonServe, Name: name}}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })
	requestOpen(t, conn, daemon.OpenFile{Files: []daemon.OpenFileTarget{{Path: gone}}})
	sendResize(t, conn)
	drainDaemonForText(t, conn, "the file was not opened")
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if list.Active == idB {
		t.Fatalf("a failed materialization still activated the owner: %+v", list)
	}
	for _, e := range list.Entries {
		if e.ID == idB && e.Materialized {
			t.Fatalf("a failed materialization was kept materialized: %+v", list)
		}
	}
	quitDaemon(t, conn, done)
}

// TestAttachClientTargetsSendsCwdAndPositions checks the attach client's forwarding fix:
// the hello and open_file frames carry the client's cwd, the raw relative paths (legacy
// field) and the positional files, so a daemon can resolve them against the client's
// directory and place the caret.
func TestAttachClientTargetsSendsCwdAndPositions(t *testing.T) {
	name := daemonTestHome(t, "app-attach-targets")
	ln, socket, err := daemon.Listen(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	cwd := t.TempDir()
	got := make(chan daemon.OpenFile, 1)
	errs := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errs <- err
			return
		}
		defer func() { _ = conn.Close() }()
		hello, err := daemon.ReadFrame(conn)
		if err != nil || hello.Type != daemon.TypeHello {
			errs <- fmt.Errorf("hello frame: %+v, %v", hello, err)
			return
		}
		f, err := daemon.ReadFrame(conn)
		if err != nil || f.Type != daemon.TypeOpenFile {
			errs <- fmt.Errorf("open frame: %+v, %v", f, err)
			return
		}
		var req daemon.OpenFile
		if err := daemon.DecodeJSON(f, &req); err != nil {
			errs <- err
			return
		}
		got <- req
	}()
	want := []daemon.OpenFileTarget{{Path: "rel/f.txt", Line: 3, Col: 2}, {Path: "/abs/g.txt"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = daemon.AttachClientTargets(ctx, name, cwd, want) // returns when the fake server closes
	select {
	case err := <-errs:
		t.Fatal(err)
	case req := <-got:
		if req.Cwd != cwd {
			t.Fatalf("cwd = %q, want %q", req.Cwd, cwd)
		}
		if len(req.Files) != 2 || req.Files[0].Line != 3 || req.Files[0].Col != 2 || req.Files[1].Path != "/abs/g.txt" {
			t.Fatalf("files = %+v", req.Files)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the attach client sent no open_file frame")
	}
	_ = socket
}
