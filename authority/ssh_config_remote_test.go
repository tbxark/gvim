//go:build unix

package authority_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// TestSSHProxyJumpThroughConfig connects to a target server through a second
// in-process server named by ProxyJump in the user config, with the target
// reached over the jump's direct-tcpip tunnel.
func TestSSHProxyJumpThroughConfig(t *testing.T) {
	target := newFakeSSH(t)
	jump := newFakeSSH(t)
	cfgPath := filepath.Join(t.TempDir(), "config")
	cfg := fmt.Sprintf("Host target\n  HostName 127.0.0.1\n  Port %d\n  User u\n  ProxyJump bastion\n\nHost bastion\n  HostName 127.0.0.1\n  Port %d\n  User u\n", target.port(), jump.port())
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	acceptBoth := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if pk := key.Marshal(); bytes.Equal(pk, target.signer.PublicKey().Marshal()) || bytes.Equal(pk, jump.signer.PublicKey().Marshal()) {
			return nil
		}
		return fmt.Errorf("unknown host key from %s", hostname)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := authority.ConnectSSH(ctx, authority.SSHOptions{
		Spec:            authority.RemoteSpec{Host: "target", Path: "/x"},
		ConfigPath:      cfgPath,
		HostKeyCallback: acceptBoth,
		Password:        func(context.Context) (string, error) { return "pw", nil },
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ConnectSSH through jump: %v", err)
	}
	defer func() { _ = a.Close() }()
	if _, err := a.OneShot().Run(ctx, []string{"true"}, hostos.ProcOptions{}); err != nil {
		t.Fatalf("one-shot through jump: %v", err)
	}
	if got := target.lastExec(t); got == "" || !stringsContains(got, "true") {
		t.Fatalf("target server exec = %q, want the composed command", got)
	}
	jump.mu.Lock()
	directs := append([]string(nil), jump.directTCPIP...)
	jump.mu.Unlock()
	wantAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(target.port()))
	proxied := false
	for _, d := range directs {
		if d == wantAddr {
			proxied = true
		}
	}
	if !proxied {
		t.Fatalf("jump proxied %v, want %s", directs, wantAddr)
	}
}

func stringsContains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

// TestSSHAgentForwarding checks ForwardAgent: the session request reaches the
// server, the server opens the standard agent channel, and it reads the fake
// local agent's identities through it.
func TestSSHAgentForwarding(t *testing.T) {
	srv := newFakeSSH(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	wantSigners, err := keyring.Signers()
	if err != nil || len(wantSigners) != 1 {
		t.Fatalf("keyring signers: %v, %v", wantSigners, err)
	}
	a := srv.connect(t, authority.SSHOptions{Agent: keyring, ForwardAgent: true})
	defer func() { _ = a.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.OneShot().Run(ctx, []string{"true"}, hostos.ProcOptions{}); err != nil {
		t.Fatalf("one-shot: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		srv.mu.Lock()
		requests, signers, agentErr := srv.agentRequests, append([]ssh.PublicKey(nil), srv.agentSigners...), srv.agentErr
		srv.mu.Unlock()
		if agentErr != nil {
			t.Fatalf("server reading the forwarded agent: %v", agentErr)
		}
		if requests > 0 && len(signers) == 1 {
			if !bytes.Equal(signers[0].Marshal(), wantSigners[0].PublicKey().Marshal()) {
				t.Fatalf("forwarded identity differs from the fake agent's")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent forwarding not observed: requests=%d signers=%d", requests, len(signers))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSSHProxyJumpStripsTargetCredentials ensures target UI passwords and 2FA
// keyboardAnswers are never sent to or consumed by the ProxyJump bastion.
func TestSSHProxyJumpStripsTargetCredentials(t *testing.T) {
	target := newFakeSSH(t)
	jump := newFakeSSH(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	var jumpGotPassword, jumpGotKbd atomic.Bool
	jump.mu.Lock()
	jump.passwordCallback = func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		jumpGotPassword.Store(true)
		return nil, errors.New("bastion does not accept passwords")
	}
	jump.keyboardCallback = func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		jumpGotKbd.Store(true)
		return nil, errors.New("bastion does not accept keyboard-interactive")
	}
	jump.mu.Unlock()

	target.mu.Lock()
	target.publicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		return nil, errors.New("target rejects pubkey")
	}
	target.passwordCallback = func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		if string(pass) == "target-secret" {
			return nil, nil
		}
		return nil, errors.New("bad password")
	}
	target.keyboardCallback = func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		answers, cerr := challenge("2FA", "", []string{"Code:"}, []bool{false})
		if cerr != nil {
			return nil, cerr
		}
		if len(answers) == 1 && answers[0] == "654321" {
			return nil, nil
		}
		return nil, errors.New("bad 2fa code")
	}
	target.mu.Unlock()

	cfgPath := filepath.Join(dir, "config")
	cfg := fmt.Sprintf("Host target\n  HostName 127.0.0.1\n  Port %d\n  User u\n  ProxyJump bastion\n\nHost bastion\n  HostName 127.0.0.1\n  Port %d\n  User u\n", target.port(), jump.port())
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	acceptBoth := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if pk := key.Marshal(); bytes.Equal(pk, target.signer.PublicKey().Marshal()) || bytes.Equal(pk, jump.signer.PublicKey().Marshal()) {
			return nil
		}
		return fmt.Errorf("unknown host key from %s", hostname)
	}

	t.Setenv("SSH_AUTH_SOCK", "")
	a := authority.NewSSH(authority.SSHOptions{
		Spec:            authority.RemoteSpec{Host: "target", Path: "/x"},
		ConfigPath:      cfgPath,
		IdentityFiles:   []string{keyPath},
		DisableAgent:    true,
		HostKeyCallback: acceptBoth,
		Timeout:         5 * time.Second,
	})
	defer func() { _ = a.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Supply target password; it must never reach the bastion.
	if err := a.ReconnectWithPassword(ctx, "target-secret"); err != nil {
		t.Fatalf("ReconnectWithPassword through jump: %v", err)
	}
	if jumpGotPassword.Load() || jumpGotKbd.Load() {
		t.Fatalf("bastion saw target password=%v keyboard=%v", jumpGotPassword.Load(), jumpGotKbd.Load())
	}
	_ = a.Close()

	// Also verify 2FA keyboardAnswers are not sent to or consumed by the bastion.
	target.mu.Lock()
	target.passwordCallback = func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		return nil, errors.New("target rejects password")
	}
	target.mu.Unlock()
	a2 := authority.NewSSH(authority.SSHOptions{
		Spec:            authority.RemoteSpec{Host: "target", Path: "/x"},
		ConfigPath:      cfgPath,
		IdentityFiles:   []string{keyPath},
		DisableAgent:    true,
		HostKeyCallback: acceptBoth,
		KeyboardInteractive: func(context.Context, string, string, []string, []bool) ([]string, error) {
			return nil, authority.ErrNeedsKeyboardInteractive
		},
		Timeout: 5 * time.Second,
	})
	defer func() { _ = a2.Close() }()
	_ = a2.Reconnect(ctx)
	if !a2.NeedsKeyboardInteractive() {
		t.Fatal("target did not record 2FA challenge")
	}
	if err := a2.ReconnectWithAnswers(ctx, []string{"654321"}); err != nil {
		t.Fatalf("ReconnectWithAnswers through jump: %v", err)
	}
	if jumpGotPassword.Load() || jumpGotKbd.Load() {
		t.Fatalf("bastion saw target credentials on 2FA reconnect: password=%v keyboard=%v", jumpGotPassword.Load(), jumpGotKbd.Load())
	}
}
