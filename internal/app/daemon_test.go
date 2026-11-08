package app

import (
	"context"
	"net"
	"os"
	"path/filepath"
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
	// The daemon exit saved the session; detaching alone did not remove it.
	if _, err := os.Stat(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("the daemon exit saved no session: %v", err)
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
