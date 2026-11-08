//go:build unix

package authority_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/tbxark/gvim/authority"
)

// TestSSHNeedsEncryptedKeyPassphrase checks an encrypted identity does not get skipped
// silently: the dial reports ErrNeedsPassphrase, the backend records the request, and
// ReconnectWithPassphrase redials with the secret.
func TestSSHNeedsEncryptedKeyPassphrase(t *testing.T) {
	srv := newFakeSSH(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte("sekret"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	a := authority.NewSSH(authority.SSHOptions{
		Spec:            authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/srv/proj"},
		Timeout:         5 * time.Second,
		IdentityFiles:   []string{keyPath},
		DisableAgent:    true,
		HostKeyCallback: ssh.FixedHostKey(srv.signer.PublicKey()),
		Passphrase: func(context.Context, string) (string, error) {
			return "", authority.ErrNeedsPassphrase
		},
	})
	defer func() { _ = a.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = a.Reconnect(ctx)
	if !errors.Is(err, authority.ErrNeedsPassphrase) {
		t.Fatalf("Reconnect error = %v, want ErrNeedsPassphrase", err)
	}
	if a.Connected() {
		t.Fatal("connected without the passphrase")
	}
	if !a.NeedsPassphrase() {
		t.Fatal("the backend did not record the passphrase request")
	}
	if err := a.ReconnectWithPassphrase(ctx, "sekret"); err != nil {
		t.Fatalf("ReconnectWithPassphrase: %v", err)
	}
	if !a.Connected() {
		t.Fatal("not connected after the passphrase")
	}
	if a.NeedsPassphrase() {
		t.Fatal("the passphrase request survived a successful dial")
	}
}

// TestSSHKeyboardInteractiveChallenge checks a 2FA challenge is recorded for the UI and
// ReconnectWithAnswers completes the dial.
func TestSSHKeyboardInteractiveChallenge(t *testing.T) {
	srv := newFakeSSH(t)
	srv.keyboardCallback = func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		answers, err := challenge("2FA", "Enter the code", []string{"Code:"}, []bool{false})
		if err != nil {
			return nil, err
		}
		if len(answers) == 1 && answers[0] == "123456" {
			return nil, nil
		}
		return nil, errors.New("bad code")
	}
	// No agent, no identity files (an empty HOME), so only the password and
	// keyboard-interactive methods are offered. The password callback refuses, and the
	// keyboard-interactive callback reports that the UI must answer.
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", t.TempDir())
	a := authority.NewSSH(authority.SSHOptions{
		Spec:            authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/srv/proj"},
		Timeout:         5 * time.Second,
		DisableAgent:    true,
		HostKeyCallback: ssh.FixedHostKey(srv.signer.PublicKey()),
		Password: func(context.Context) (string, error) {
			return "", errors.New("no password auth")
		},
		KeyboardInteractive: func(context.Context, string, string, []string, []bool) ([]string, error) {
			return nil, authority.ErrNeedsKeyboardInteractive
		},
	})
	defer func() { _ = a.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = a.Reconnect(ctx)
	if a.Connected() {
		t.Fatal("connected without the 2FA code")
	}
	if !a.NeedsKeyboardInteractive() {
		t.Fatal("the backend did not record the keyboard-interactive challenge")
	}
	challenge, ok := a.KeyboardInteractiveChallenge()
	if !ok || len(challenge.Questions) != 1 || challenge.Questions[0] != "Code:" || len(challenge.Echos) != 1 || challenge.Echos[0] {
		t.Fatalf("challenge = %+v", challenge)
	}
	if err := a.ReconnectWithAnswers(ctx, []string{"123456"}); err != nil {
		t.Fatalf("ReconnectWithAnswers: %v", err)
	}
	if !a.Connected() {
		t.Fatal("not connected after the 2FA code")
	}
}
