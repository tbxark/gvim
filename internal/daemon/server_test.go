package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	if err := WriteJSON(conn, TypeHello, Hello{Version: ProtocolVersion, Cols: cols, Rows: rows}); err != nil {
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

// TestServerMultipleClientsShareTheSession: two clients attach to one session; both
// receive every output frame, input from either reaches the program, and the negotiated
// size is the smallest attached client's size.
func TestServerMultipleClientsShareTheSession(t *testing.T) {
	srv, _ := startTestServer(t)
	first := dialTestDaemon(t, srv, 100, 30)
	second := dialTestDaemon(t, srv, 80, 24)
	if cols, rows := srv.negotiatedSize(); cols != 80 || rows != 24 {
		t.Fatalf("negotiated size = %dx%d, want the smallest 80x24", cols, rows)
	}
	// Both see the same frame.
	go func() { _, _ = srv.Write([]byte("shared")) }()
	for i, c := range []*client{first, second} {
		f, err := ReadFrame(c.conn)
		if err != nil || f.Type != TypeOutput || string(f.Payload) != "shared" {
			t.Fatalf("client %d frame = %#v, %v", i, f, err)
		}
	}
	// Input from the second client reaches the program.
	if err := WriteFrame(second.conn, TypeInput, []byte("from-second")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	done := make(chan int, 1)
	go func() {
		n, _ := srv.Read(buf)
		done <- n
	}()
	select {
	case n := <-done:
		if got := string(buf[:n]); got != "from-second" {
			t.Fatalf("input = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second client's input did not arrive")
	}
	// A resize of either client renegotiates; the smallest still wins.
	if err := WriteJSON(first.conn, TypeResize, Resize{Cols: 60, Rows: 20}); err != nil {
		t.Fatal(err)
	}
	waitNegotiated(t, srv, 60, 20)
	if err := WriteJSON(first.conn, TypeResize, Resize{Cols: 140, Rows: 50}); err != nil {
		t.Fatal(err)
	}
	waitNegotiated(t, srv, 80, 24)
}

// TestServerDetachOneClientKeepsTheSession: detaching one client acks it, keeps the
// others and the hosted program; the remaining client keeps receiving frames and the
// size is renegotiated without the detached client.
func TestServerDetachOneClientKeepsTheSession(t *testing.T) {
	srv, _ := startTestServer(t)
	first := dialTestDaemon(t, srv, 100, 30)
	second := dialTestDaemon(t, srv, 80, 24)
	if err := WriteFrame(second.conn, TypeDetach, nil); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(second.conn)
	if err != nil || f.Type != TypeDetached {
		t.Fatalf("detach ack = %#v, %v", f, err)
	}
	waitNegotiated(t, srv, 100, 30)
	go func() { _, _ = srv.Write([]byte("still-here")) }()
	f, err = ReadFrame(first.conn)
	if err != nil || f.Type != TypeOutput || string(f.Payload) != "still-here" {
		t.Fatalf("remaining client frame = %#v, %v", f, err)
	}
	// A third client can attach to the same living session.
	third := dialTestDaemon(t, srv, 70, 20)
	waitNegotiated(t, srv, 70, 20)
	_ = third
}

// TestServerRefusesAnIncompatibleProtocolVersion: a client speaking another protocol
// version gets a clear notice and is dropped before it can change the shared session;
// an existing client's size and stream are untouched, and a correct client still attaches.
func TestServerRefusesAnIncompatibleProtocolVersion(t *testing.T) {
	srv, _ := startTestServer(t)
	first := dialTestDaemon(t, srv, 100, 30)
	conn, err := Attach(srv.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := WriteJSON(conn, TypeHello, Hello{Version: ProtocolVersion + 1, Cols: 40, Rows: 10}); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(conn)
	if err != nil || f.Type != TypeVersionRejected {
		t.Fatalf("incompatible hello answer = %#v, %v", f, err)
	}
	var mismatch VersionMismatch
	if err := DecodeJSON(f, &mismatch); err != nil || mismatch.Client != ProtocolVersion+1 || mismatch.Daemon != ProtocolVersion {
		t.Fatalf("version-rejected payload = %+v, %v", mismatch, err)
	}
	// No state change: the old client's size is still the negotiated one.
	if cols, rows := srv.negotiatedSize(); cols != 100 || rows != 30 {
		t.Fatalf("size after incompatible hello = %dx%d", cols, rows)
	}
	// The existing client still gets frames; a compatible client can attach.
	go func() { _, _ = srv.Write([]byte("ok")) }()
	if f, err := ReadFrame(first.conn); err != nil || f.Type != TypeOutput || string(f.Payload) != "ok" {
		t.Fatalf("first client frame = %#v, %v", f, err)
	}
	_ = dialTestDaemon(t, srv, 90, 25)
}

func waitNegotiated(t *testing.T, srv *Server, cols, rows int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, r := srv.negotiatedSize(); c == cols && r == rows {
			return
		}
		if time.Now().After(deadline) {
			c, r := srv.negotiatedSize()
			t.Fatalf("negotiated size = %dx%d, want %dx%d", c, r, cols, rows)
		}
		time.Sleep(time.Millisecond)
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
	if err := WriteJSON(c, TypeHello, Hello{Version: ProtocolVersion, Cols: 80, Rows: 24}); err != nil {
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

// TestServerConcurrentOutputFramesIntact checks frames written concurrently through
// Server.Write arrive whole: every payload is one of the expected blocks, never a mix.
func TestServerConcurrentOutputFramesIntact(t *testing.T) {
	srv, _ := startTestServer(t)
	c := dialTestDaemon(t, srv, 100, 30)
	const n = 16
	want := make([][]byte, n)
	for i := range want {
		want[i] = bytes.Repeat([]byte{byte('a' + i)}, 8<<10)
	}
	go func() {
		for _, p := range want {
			_, _ = srv.Write(p)
		}
	}()
	got := make([][]byte, 0, n)
	deadline := time.After(5 * time.Second)
	for len(got) < n {
		type res struct {
			f   Frame
			err error
		}
		ch := make(chan res, 1)
		go func() {
			f, err := ReadFrame(c.conn)
			ch <- res{f, err}
		}()
		select {
		case r := <-ch:
			if r.err != nil {
				t.Fatalf("read frame %d: %v", len(got), r.err)
			}
			got = append(got, r.f.Payload)
		case <-deadline:
			t.Fatalf("read %d of %d frames", len(got), n)
		}
	}
	seen := make(map[string]bool, n)
	for _, p := range got {
		seen[string(p)] = true
	}
	for i, p := range want {
		if !seen[string(p)] {
			t.Fatalf("output frame %d never arrived whole", i)
		}
	}
}

// TestWorkspaceProtocolServer checks the workspace frames reach the hook and answer with a
// workspaces frame, that a malformed request is a notice (not a disconnect), and that a
// daemon without a hook refuses with a notice.
func TestWorkspaceProtocolServer(t *testing.T) {
	srv, _ := startTestServer(t)
	c := dialTestDaemon(t, srv, 100, 30)
	// No hook yet: a workspace_list gets a notice and the connection stays usable.
	if err := WriteFrame(c.conn, TypeWorkspaceList, nil); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(c.conn)
	if err != nil || f.Type != TypeNotice || !strings.Contains(string(f.Payload), "no workspace layer") {
		t.Fatalf("frame without a hook = %#v, %v", f, err)
	}
	var got WorkspaceRequest
	srv.OnWorkspaceRequest(func(req WorkspaceRequest) WorkspaceResult {
		got = req
		return WorkspaceResult{List: &WorkspaceList{Active: "id-1", Entries: []WorkspaceInfo{{ID: "id-1", Label: "one", Root: "/tmp/one", Materialized: true, Active: true}}}}
	})
	if err := WriteFrame(c.conn, TypeWorkspaceList, nil); err != nil {
		t.Fatal(err)
	}
	f, err = ReadFrame(c.conn)
	if err != nil || f.Type != TypeWorkspaces {
		t.Fatalf("workspace answer = %#v, %v", f, err)
	}
	var list WorkspaceList
	if err := DecodeJSON(f, &list); err != nil {
		t.Fatal(err)
	}
	if list.Active != "id-1" || len(list.Entries) != 1 || list.Entries[0].Label != "one" || !list.Entries[0].Materialized {
		t.Fatalf("list = %+v", list)
	}
	if got.List == nil {
		t.Fatalf("hook saw %+v, want a list request", got)
	}
	// A rename request reaches the hook with its fields.
	if err := WriteJSON(c.conn, TypeWorkspaceRename, WorkspaceRename{ID: "id-1", Label: "renamed"}); err != nil {
		t.Fatal(err)
	}
	f, err = ReadFrame(c.conn)
	if err != nil || f.Type != TypeWorkspaces {
		t.Fatalf("rename answer = %#v, %v", f, err)
	}
	if got.Rename == nil || got.Rename.Label != "renamed" {
		t.Fatalf("hook saw %+v", got)
	}
	// A malformed request is a notice, and the connection survives (ping answers).
	if err := WriteJSON(c.conn, TypeWorkspaceSelect, WorkspaceSelect{}); err != nil {
		t.Fatal(err)
	}
	f, err = ReadFrame(c.conn)
	if err != nil || f.Type != TypeNotice || !strings.Contains(string(f.Payload), "empty id") {
		t.Fatalf("malformed request answer = %#v, %v", f, err)
	}
	waitAttached(t, c.conn)
}

// blockedUpdateModel blocks the program's event loop in Update until release is closed.
type blockedUpdateModel struct{ release chan struct{} }

func (blockedUpdateModel) Init() tea.Cmd { return nil }
func (m blockedUpdateModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	<-m.release
	return m, nil
}
func (blockedUpdateModel) View() tea.View { return tea.NewView("") }

// TestServerHelloSendsSizeOutsideLock is the regression for a daemon deadlock: the hello
// and resize handlers used to call Program.Send while holding s.mu. When the program's
// event loop was busy (for example rendering), Send blocked while s.mu stayed held; the
// renderer's flush then blocked in Server.Write on that same mutex, and the event loop
// waited for the renderer - a three-way deadlock that stalled the daemon. The handler must
// release s.mu before Send, so Server.Write (the renderer's output path) keeps working.
func TestServerHelloSendsSizeOutsideLock(t *testing.T) {
	srv, _ := startTestServer(t)
	release := make(chan struct{})
	prog := tea.NewProgram(blockedUpdateModel{release: release}, tea.WithoutRenderer(), tea.WithInput(nil), tea.WithOutput(io.Discard))
	runDone := make(chan struct{})
	go func() {
		_, _ = prog.Run()
		close(runDone)
	}()
	t.Cleanup(func() {
		close(release)
		prog.Quit()
		select {
		case <-runDone:
		case <-time.After(2 * time.Second):
		}
	})
	srv.mu.Lock()
	srv.prog = prog
	srv.mu.Unlock()

	conn, err := Attach(srv.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := WriteJSON(conn, TypeHello, Hello{Version: ProtocolVersion, Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	// The hello handler added the client and is now blocked in Program.Send (the event
	// loop is inside the blocked Update). The server lock must be free meanwhile.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if srv.mu.TryLock() {
			attached := len(srv.clients) > 0
			srv.mu.Unlock()
			if attached {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the hello handler held s.mu while Program.Send blocked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The renderer's output path must not be blocked by the pending Send.
	done := make(chan struct{})
	go func() { _, _ = srv.Write([]byte("frame")); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Server.Write blocked while Program.Send was pending")
	}
}
