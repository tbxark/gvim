package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
)

// waitForLiveHosts waits until the daemon host mark for root is readable (the daemon
// writes it right after it starts listening).
func waitForLiveHosts(t *testing.T, root string) []hostMark {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if hosts := liveHostsFor(root); len(hosts) > 0 {
			return hosts
		}
		if time.Now().After(deadline) {
			t.Fatalf("no live daemon host mark for %s", root)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForNoLiveHosts waits until the daemon removed its mark (after its checked exit save).
func waitForNoLiveHosts(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if len(liveHostsFor(root)) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon host mark for %s was not cleared", root)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// quitDaemonTest closes a test daemon if it is still running.
func quitDaemonTest(t *testing.T, name string, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		_ = err
		return
	default:
	}
	conn, err := daemon.Attach(name)
	if err != nil {
		return
	}
	// A raw client must announce itself with the hello frame before the server reads its
	// input frames.
	_ = daemon.WriteJSON(conn, daemon.TypeHello, daemon.Hello{Cols: 100, Rows: 30})
	_ = daemon.WriteFrame(conn, daemon.TypeInput, []byte{0x11})
	_ = conn.Close()
	select {
	case err := <-done:
		_ = err
	case <-time.After(10 * time.Second):
		t.Error("the daemon did not quit")
	}
}

// TestDaemonNamedSingleWriterRefusal covers an explicitly named daemon: the per-root
// default socket is not running, so only the daemon host mark discovers it. A direct
// file launch must refuse and name the daemon with its attach instruction.
func TestDaemonNamedSingleWriterRefusal(t *testing.T) {
	isolateConfig(t)
	name := "app-test-named-host"
	_ = daemonTestHome(t, name)
	root := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	opts := Options{
		Root:       root,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	done := startDaemonTest(t, opts, name)
	t.Cleanup(func() { quitDaemonTest(t, name, done) })
	resolved, err := resolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := daemon.CanonicalRoot(resolved)
	if err != nil {
		t.Fatal(err)
	}
	hosts := waitForLiveHosts(t, canonical)
	if len(hosts) != 1 || hosts[0].Name != name {
		t.Fatalf("host marks = %+v", hosts)
	}
	err = refuseIfDaemonRunning(Options{Root: root, Files: []FileTarget{{Path: filepath.Join(root, "a.txt")}}})
	if err == nil {
		t.Fatal("a direct file launch was allowed while a named daemon hosts the workspace")
	}
	for _, want := range []string{name, "--attach=" + name, "second writer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal lacks %q: %v", want, err)
		}
	}
	// Quitting clears the mark only after the daemon's exit save.
	quitDaemonTest(t, name, done)
	waitForNoLiveHosts(t, canonical)
}

// TestDaemonRefusalKeepsUnsavedWork is the no-lost-work case: the daemon holds an unsaved
// edit to a file; a direct file launch is refused (so it cannot write the file under the
// daemon's buffer); after the direct launch the file on disk is untouched, and the
// daemon's unsaved text survives in the workspace session its exit save writes.
func TestDaemonRefusalKeepsUnsavedWork(t *testing.T) {
	isolateConfig(t)
	home := daemonTestHome(t, "app-test-no-lost")
	_ = home
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	resolved, err := resolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	name := daemon.DefaultName(resolved)
	opts := Options{
		Root:       root,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	}
	done := startDaemonTest(t, opts, name)
	t.Cleanup(func() { quitDaemonTest(t, name, done) })
	conn := attachDaemonTest(t, name)
	t.Cleanup(func() { _ = conn.Close() })
	if err := daemon.WriteJSON(conn, daemon.TypeResize, daemon.Resize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	// Open the file in the daemon's workspace (a daemon launch itself opens no files),
	// then type an unsaved change.
	if err := daemon.WriteJSON(conn, daemon.TypeOpenFile, daemon.OpenFile{Paths: []string{path}}); err != nil {
		t.Fatal(err)
	}
	drainDaemonForText(t, conn, "hello")
	if err := daemon.WriteFrame(conn, daemon.TypeInput, []byte("X")); err != nil {
		t.Fatal(err)
	}
	drainDaemonForText(t, conn, "Xhello")

	// A direct file launch is refused, and it does not touch the file the daemon holds.
	if err := refuseIfDaemonRunning(Options{Root: root, Files: []FileTarget{{Path: path}}}); err == nil {
		t.Fatal("a direct file launch was allowed while the daemon holds the file unsaved")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "hello\n" {
		t.Fatalf("the refused direct launch touched the daemon's file: %q, %v", raw, err)
	}

	// Quitting a modified buffer asks Save/Discard/Cancel, so keep the daemon's work the
	// way the daemon itself does: switch to a second workspace. The capture of the first
	// workspace must carry its unsaved text, and the file on disk must stay untouched.
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceNew, daemon.WorkspaceNew{Root: t.TempDir(), Label: "other"})
	firstID := ""
	for _, entry := range list.Entries {
		if entry.ID != list.Active {
			firstID = entry.ID
		}
	}
	if firstID == "" {
		t.Fatalf("no first workspace in %+v", list)
	}
	sessionPath := filepath.Join(workspaces, "sessions", firstID, "session.json")
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
	var found bool
	_ = filepath.WalkDir(filepath.Join(workspaces, "sessions", firstID), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr == nil && strings.Contains(string(raw), "Xhello") {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("the daemon's unsaved text was not kept in the workspace session")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "hello\n" {
		t.Fatalf("the daemon's unsaved text was written past the refused direct launch: %q, %v", raw, err)
	}
	if err := daemon.WriteFrame(conn, daemon.TypeInput, []byte{0x11}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon did not quit after the switch")
	}
}

// TestDaemonHostMarkFailureRefusesToStart: the host mark is what lets a direct launch see
// a NAMED daemon, so a failed registration must refuse the start instead of leaving a
// daemon that a direct launch cannot detect (and would race). The listener is closed and
// its socket removed on the way out.
func TestDaemonHostMarkFailureRefusesToStart(t *testing.T) {
	isolateConfig(t)
	_ = daemonTestHome(t, "app-test-hostmark-fail")
	root := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	name := "app-test-hostmark-fail"
	opts := Options{
		Root:       root,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
	path, err := daemon.SocketPath(name)
	if err != nil {
		t.Fatal(err)
	}
	// A regular file where the hosts directory belongs makes the mark write fail.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(filepath.Dir(path), "hosts")
	if err := os.WriteFile(hosts, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "host mark") {
		t.Fatalf("Run = %v, want a host mark startup error", err)
	}
	if daemon.IsLive(path) {
		t.Error("the daemon kept listening after the host mark failure")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("the daemon left its socket file behind after the host mark failure")
	}
}
