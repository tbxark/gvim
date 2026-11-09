//go:build unix

package authority

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// silentSSH ignores the global-request channel, so the client's keepalive ping never gets
// its reply and must time out.
type silentSSH struct {
	ln  net.Listener
	cfg *ssh.ServerConfig
}

func newSilentSSH(t *testing.T) *silentSSH {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &silentSSH{ln: ln, cfg: cfg}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				// The connection is accepted and then deliberately left without
				// reading its channels or global requests: a keepalive with want-reply
				// never comes back.
				_, _, _, _ = ssh.NewServerConn(conn, s.cfg)
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *silentSSH) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// TestSSHKeepaliveMarksTheCarrierDown checks a failing info ping flips the slot to
// disconnected with a keepalive reason before the next user operation, and that a
// reconnect restarts the carrier.
func TestSSHKeepaliveMarksTheCarrierDown(t *testing.T) {
	srv := newSilentSSH(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &sshBackend{opts: SSHOptions{
		Spec:            RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/srv/proj"},
		Timeout:         5 * time.Second,
		DisableAgent:    true,
		IdentityFiles:   []string{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // test server
		Password:        func(context.Context) (string, error) { return "pw", nil },
	}, spec: RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/srv/proj"},
		keepaliveInterval: 20 * time.Millisecond, keepaliveTimeout: 40 * time.Millisecond}
	if err := b.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for b.Connected() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if b.Connected() {
		t.Fatal("the carrier stayed connected although the keepalive never answered")
	}
	if got := b.Reason(); got == "" {
		t.Fatal("the keepalive failure left no reason")
	}
	if err := b.Reconnect(ctx); err != nil {
		t.Fatalf("reconnect after the keepalive drop: %v", err)
	}
	if !b.Connected() {
		t.Fatal("not connected after the reconnect")
	}
	_ = b.Close()
}
