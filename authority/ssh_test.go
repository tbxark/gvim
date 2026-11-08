//go:build unix

package authority_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// testExec is one exec request the fake server saw.
type testExec struct {
	Command string
}

// testSize is one PTY/window size the fake server saw.
type testSize struct {
	Cols, Rows int
}

// fakeSSH is an in-process SSH server: password policy "pw", any public key accepted. It
// records exec commands, PTY requests and window changes, and can drop live connections to
// exercise reconnect.
type fakeSSH struct {
	t        *testing.T
	signer   ssh.Signer
	cfg      *ssh.ServerConfig
	ln       net.Listener
	mu       sync.Mutex
	execs    []testExec
	ptys     []testSize
	changes  []testSize
	conns    []*ssh.ServerConn
	accepted int
}

// newFakeSSH starts the server on a loopback port.
func newFakeSSH(t *testing.T) *fakeSSH {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSSH{t: t, signer: signer}
	s.cfg = &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == "pw" {
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	s.cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	go s.acceptLoop()
	t.Cleanup(s.stop)
	return s
}

func (s *fakeSSH) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSSH) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeSSH) handleConn(conn net.Conn) {
	sc, chans, reqs, err := ssh.NewServerConn(conn, s.cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	s.mu.Lock()
	s.conns = append(s.conns, sc)
	s.accepted++
	s.mu.Unlock()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(channel, requests)
	}
}

func (s *fakeSSH) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer func() { _ = ch.Close() }()
	execMode, ptyMode := false, false
	for req := range reqs {
		switch req.Type {
		case "exec":
			var payload struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &payload)
			s.mu.Lock()
			s.execs = append(s.execs, testExec{Command: payload.Command})
			s.mu.Unlock()
			execMode = true
			reply(req, true)
			if ptyMode {
				// A PTY-backed exec is an interactive program: echo like a shell.
				go s.runPTY(ch, false)
			} else {
				go s.runExec(ch, payload.Command)
			}
		case "shell":
			reply(req, true)
			go s.runPTY(ch, true)
		case "pty-req":
			var payload struct {
				Term          string
				Cols, Rows    uint32
				Width, Height uint32
				Modes         string
			}
			_ = ssh.Unmarshal(req.Payload, &payload)
			s.mu.Lock()
			s.ptys = append(s.ptys, testSize{Cols: int(payload.Cols), Rows: int(payload.Rows)})
			s.mu.Unlock()
			ptyMode = true
			reply(req, true)
		case "window-change":
			var payload struct{ Cols, Rows, Width, Height uint32 }
			_ = ssh.Unmarshal(req.Payload, &payload)
			s.mu.Lock()
			s.changes = append(s.changes, testSize{Cols: int(payload.Cols), Rows: int(payload.Rows)})
			s.mu.Unlock()
			reply(req, false)
		case "signal":
			reply(req, true)
		default:
			reply(req, false)
		}
	}
	if !execMode && !ptyMode {
		return
	}
}
func reply(req *ssh.Request, ok bool) {
	if req.WantReply {
		_ = req.Reply(ok, nil)
	}
}

// runExec answers a one-shot command deterministically: "fail" exits 3, "sleeper" echoes
// stdin until EOF, everything else succeeds. The channel is closed after the exit status so
// the client's Run returns.
func (s *fakeSSH) runExec(ch ssh.Channel, cmd string) {
	switch {
	case strings.Contains(cmd, "sleeper"):
		buf := make([]byte, 1024)
		for {
			n, err := ch.Read(buf)
			if n > 0 {
				_, _ = ch.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		sendExit(ch, 0)
	case strings.Contains(cmd, "fail"):
		_, _ = io.WriteString(ch.Stderr(), "boom\n")
		sendExit(ch, 3)
	default:
		_, _ = io.WriteString(ch, "ran:"+cmd+"\n")
		sendExit(ch, 0)
	}
	_ = ch.Close()
}

// runPTY is the shell mode of the terminal: it announces itself and echoes input until EOF.
func (s *fakeSSH) runPTY(ch ssh.Channel, _ bool) {
	_, _ = io.WriteString(ch, "ready\r\n")
	buf := make([]byte, 1024)
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			_, _ = ch.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	sendExit(ch, 0)
	_ = ch.Close()
}

func sendExit(ch ssh.Channel, code int) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{uint32(code)}))
}

// dropConnections closes every accepted server-side connection, simulating a carrier drop.
func (s *fakeSSH) dropConnections() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *fakeSSH) lastExec(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) == 0 {
		return ""
	}
	return s.execs[len(s.execs)-1].Command
}

func (s *fakeSSH) sizes() (ptys, changes []testSize) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]testSize(nil), s.ptys...), append([]testSize(nil), s.changes...)
}

// stop closes the listener and the live connections.
func (s *fakeSSH) stop() {
	_ = s.ln.Close()
	s.dropConnections()
}

// connect dials the fake server with password auth and a fixed host key.
func (s *fakeSSH) connect(t *testing.T, opts authority.SSHOptions) *authority.Authority {
	t.Helper()
	t.Setenv("SSH_AUTH_SOCK", "")
	if opts.Spec.Host == "" {
		opts.Spec = authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: s.port(), Path: "/srv/proj"}
	}
	if opts.HostKeyCallback == nil {
		opts.HostKeyCallback = ssh.FixedHostKey(s.signer.PublicKey())
	}
	if opts.Password == nil {
		opts.Password = func(context.Context) (string, error) { return "pw", nil }
	}
	opts.Timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := authority.ConnectSSH(ctx, opts)
	if err != nil {
		t.Fatalf("ConnectSSH: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// TestSSHOneShotAndState checks one-shot exec composition, exit codes and the connection
// state.
func TestSSHOneShotAndState(t *testing.T) {
	srv := newFakeSSH(t)
	a := srv.connect(t, authority.SSHOptions{})
	if !a.Connected() {
		t.Fatal("not connected after ConnectSSH")
	}
	wantLabel := "SSH:u@127.0.0.1:" + strconv.Itoa(srv.port())
	if got := a.Label(); got != wantLabel {
		t.Fatalf("Label = %q, want %q", got, wantLabel)
	}
	st := a.State()
	if st.Kind != "remote" || st.Remote == nil || st.Remote.Host != "127.0.0.1" || !st.Connected {
		t.Fatalf("State = %+v", st)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := a.OneShot().Run(ctx, []string{"git", "status", "--porcelain"}, hostos.ProcOptions{
		Cwd: "/srv/proj", Env: map[string]string{"GIT_DIR": "/x/.git"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantCmd := authority.RemoteCommand([]string{"git", "status", "--porcelain"}, "/srv/proj", map[string]string{"GIT_DIR": "/x/.git"})
	if got := srv.lastExec(t); got != wantCmd {
		t.Fatalf("remote command = %q, want %q", got, wantCmd)
	}
	if res.Code != 0 || !strings.Contains(string(res.Stdout), "ran:") {
		t.Fatalf("result = %+v", res)
	}
	// A non-zero exit is a result, not an error.
	res, err = a.OneShot().Run(ctx, []string{"fail"}, hostos.ProcOptions{})
	if err != nil || res.Code != 3 || !strings.Contains(string(res.Stderr), "boom") {
		t.Fatalf("failing run = %+v, %v", res, err)
	}
}

// TestSSHReconnect checks a dropped carrier marks the slot disconnected and Reconnect heals
// it.
func TestSSHReconnect(t *testing.T) {
	srv := newFakeSSH(t)
	a := srv.connect(t, authority.SSHOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.OneShot().Run(ctx, []string{"x"}, hostos.ProcOptions{}); err != nil {
		t.Fatal(err)
	}
	srv.dropConnections()
	// The next use sees the dead carrier and marks the connection down.
	deadCtx, deadCancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, err := a.OneShot().Run(deadCtx, []string{"x"}, hostos.ProcOptions{})
	deadCancel()
	if err == nil {
		t.Fatal("a command ran over a dropped carrier")
	}
	if a.Connected() {
		t.Fatal("the slot still reports connected after the drop")
	}
	if reason := a.State().Reason; reason == "" {
		t.Fatal("no disconnected reason")
	}
	if err := a.Reconnect(ctx); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if !a.Connected() {
		t.Fatal("Reconnect did not mark the slot connected")
	}
	res, err := a.OneShot().Run(ctx, []string{"x"}, hostos.ProcOptions{})
	if err != nil || res.Code != 0 {
		t.Fatalf("run after reconnect = %+v, %v", res, err)
	}
}

// TestSSHTerminal checks the remote PTY terminal: pty request, output, input echo and
// resize.
func TestSSHTerminal(t *testing.T) {
	srv := newFakeSSH(t)
	a := srv.connect(t, authority.SSHOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	launch, err := a.Terminal().Launch(ctx, authority.TerminalOptions{Dir: "/srv/proj", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if launch.Backend == nil || !launch.ManagesCwd {
		t.Fatalf("launch = %+v", launch)
	}
	defer func() { _ = launch.Backend.Close() }()
	ptys, _ := srv.sizes()
	if len(ptys) != 1 || ptys[0] != (testSize{Cols: 80, Rows: 24}) {
		t.Fatalf("pty request = %+v", ptys)
	}
	if got := srv.lastExec(t); !strings.Contains(got, "cd ") || !strings.Contains(got, "exec ") {
		t.Fatalf("terminal command = %q", got)
	}
	readUntil := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		var got []byte
		for time.Now().Before(deadline) {
			buf := make([]byte, 256)
			_ = launch.Backend.Resize(80, 24)
			n, err := launch.Backend.Read(buf)
			got = append(got, buf[:n]...)
			if strings.Contains(string(got), want) {
				return
			}
			if err != nil {
				break
			}
		}
		t.Fatalf("terminal output %q does not contain %q", got, want)
	}
	readUntil("ready")
	if _, err := launch.Backend.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	readUntil("x")
	if err := launch.Backend.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, changes := srv.sizes()
		if len(changes) > 0 && changes[len(changes)-1] == (testSize{Cols: 100, Rows: 30}) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, changes := srv.sizes()
	t.Fatalf("window changes = %+v, want 100x30", changes)
}

// TestSSHLongRunning checks the stdio spawn surface: output chunks and a clean wait.
func TestSSHLongRunning(t *testing.T) {
	srv := newFakeSSH(t)
	a := srv.connect(t, authority.SSHOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := a.LongRunning().Start(ctx, []string{"sleeper"}, hostos.ProcOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Terminate(50 * time.Millisecond)
	if err := p.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for len(got) < 5 {
		chunk, ok := p.Output(ctx)
		if !ok {
			t.Fatal("no output")
		}
		got = append(got, chunk.Data...)
	}
	if string(got) != "hello" {
		t.Fatalf("output = %q", got)
	}
	p.CloseStdin()
	res, err := p.Wait(ctx)
	if err != nil || res.Code != 0 {
		t.Fatalf("wait = %+v, %v", res, err)
	}
}

// TestSSHIdentityFile checks a private-key identity file authenticates (no password).
func TestSSHIdentityFile(t *testing.T) {
	srv := newFakeSSH(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = pub
	a := srv.connect(t, authority.SSHOptions{IdentityFiles: []string{path}, DisableAgent: true, Password: func(context.Context) (string, error) {
		t.Fatal("password callback used despite the identity file")
		return "", nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.OneShot().Run(ctx, []string{"x"}, hostos.ProcOptions{}); err != nil {
		t.Fatalf("identity auth run: %v", err)
	}
}

// TestSSHUnknownHost checks a missing known_hosts entry is a clear error, never silent
// trust.
func TestSSHUnknownHost(t *testing.T) {
	srv := newFakeSSH(t)
	empty := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := authority.ConnectSSH(ctx, authority.SSHOptions{
		Spec:           authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/x"},
		KnownHostsPath: empty,
		Password:       func(context.Context) (string, error) { return "pw", nil },
		DisableAgent:   true,
		Timeout:        3 * time.Second,
	})
	if err == nil {
		t.Fatal("an unknown host key was accepted")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to name known_hosts", err)
	}
}

// TestSSHLazyMountAndPassword checks the app-facing lazy path: NewSSH mounts
// disconnected, an auth failure reports NeedsPassword, and ReconnectWithPassword heals it.
func TestSSHLazyMountAndPassword(t *testing.T) {
	srv := newFakeSSH(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	a := authority.NewSSH(authority.SSHOptions{
		Spec:            authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/srv"},
		HostKeyCallback: ssh.FixedHostKey(srv.signer.PublicKey()),
		DisableAgent:    true,
		Timeout:         3 * time.Second,
	})
	if a.Connected() {
		t.Fatal("a lazy SSH slot starts connected")
	}
	if st := a.State(); st.Connected || st.Kind != "remote" || st.Remote == nil {
		t.Fatalf("lazy state = %+v", st)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Reconnect(ctx); err == nil {
		t.Fatal("a reconnect without credentials succeeded")
	}
	if !a.NeedsPassword() {
		t.Fatal("the auth failure did not ask for a password")
	}
	if err := a.ReconnectWithPassword(ctx, "pw"); err != nil {
		t.Fatalf("ReconnectWithPassword: %v", err)
	}
	if !a.Connected() || a.NeedsPassword() {
		t.Fatalf("connected = %v, needs password = %v", a.Connected(), a.NeedsPassword())
	}
	if _, err := a.OneShot().Run(ctx, []string{"x"}, hostos.ProcOptions{}); err != nil {
		t.Fatalf("run after password reconnect: %v", err)
	}
}
