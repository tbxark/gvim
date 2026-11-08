package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// daemonTestName returns a short unique socket name for a test and isolates the cache
// directory the sockets live in. The base directory is /tmp rather than t.TempDir
// because a Unix socket address is limited to about 104 bytes, and the test-name
// temp directories are too long on macOS.
func daemonTestName(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "gvimd")
	if err != nil {
		t.Skipf("no short temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	name := fmt.Sprintf("t%d", testSocketSeq.Add(1))
	return name
}

// testSocketSeq numbers the test sockets, keeping the socket paths short.
var testSocketSeq atomic.Int32

func startTestServer(t *testing.T) (*Server, context.CancelFunc) {
	t.Helper()
	ln, path, err := Listen(daemonTestName(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(ln, path)
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})
	return srv, cancel
}

func dialTestDaemon(t *testing.T, srv *Server, cols, rows int) *client {
	t.Helper()
	conn, err := Attach(srv.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &client{t: t, conn: conn}
	if err := WriteJSON(conn, TypeHello, Hello{Cols: cols, Rows: rows}); err != nil {
		t.Fatal(err)
	}
	waitAttached(t, conn)
	return c
}

// waitAttached blocks until the server has processed the hello: a ping is answered
// only by a connection the server has attached (and stored), so a test that closes
// the server next does not race the accept handler.
func waitAttached(t *testing.T, conn io.ReadWriter) {
	t.Helper()
	if err := WriteFrame(conn, TypePing, nil); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypePong {
		t.Fatalf("attach ping answered with %#v, want pong", f)
	}
}

// client is one attached socket in a test.
type client struct {
	t    *testing.T
	conn interface {
		io.ReadWriteCloser
	}
}

func TestServerForwardsInputAndOutput(t *testing.T) {
	srv, _ := startTestServer(t)
	c := dialTestDaemon(t, srv, 100, 30)

	if err := WriteFrame(c.conn, TypeInput, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	done := make(chan int, 1)
	go func() {
		n, err := srv.Read(buf)
		if err != nil {
			done <- -1
			return
		}
		done <- n
	}()
	select {
	case n := <-done:
		if n != 3 || string(buf[:n]) != "abc" {
			t.Fatalf("read %q, want abc", buf[:n])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the server did not deliver the client's input")
	}
	// Output goes to the attached client as an output frame.
	go func() { _, _ = srv.Write([]byte("frame")) }()
	f, err := ReadFrame(c.conn)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeOutput || string(f.Payload) != "frame" {
		t.Fatalf("frame = %#v payload %q, want output/frame", f.Type, f.Payload)
	}
	// Without a client the output is discarded, not an error.
	srv.Detach()
	if _, err := srv.Write([]byte("dropped")); err != nil {
		t.Fatalf("write without a client: %v", err)
	}
}

func TestServerRefusesASecondClient(t *testing.T) {
	srv, _ := startTestServer(t)
	first := dialTestDaemon(t, srv, 80, 24)
	_ = first
	second, err := Attach(srv.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if err := WriteJSON(second, TypeHello, Hello{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(second)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeNotice || len(f.Payload) == 0 {
		t.Fatalf("second attach got %#v, want a notice", f)
	}
}

func TestServerClientDetachKeepsHosting(t *testing.T) {
	srv, _ := startTestServer(t)
	c := dialTestDaemon(t, srv, 80, 24)
	if err := WriteFrame(c.conn, TypeDetach, nil); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(c.conn)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeDetached {
		t.Fatalf("detach ack type = %d, want detached", f.Type)
	}
	// A new client can attach after the detach: the listener and program live on.
	c2 := dialTestDaemon(t, srv, 80, 24)
	if err := WriteFrame(c2.conn, TypeInput, []byte("x")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	done := make(chan int, 1)
	go func() {
		n, _ := srv.Read(buf)
		done <- n
	}()
	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("read %d bytes, want 1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reattached client's input did not arrive")
	}
}

func TestServerCloseSendsExit(t *testing.T) {
	ln, path, err := Listen(daemonTestName(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(ln, path)
	go srv.Serve(context.Background())
	c, err := Attach(srv.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := WriteJSON(c, TypeHello, Hello{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	waitAttached(t, c)
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeExit {
		t.Fatalf("close frame type = %d, want exit", f.Type)
	}
}
